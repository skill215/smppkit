package smpp

import (
	"context"
	"errors"
	"net"
	"sync"
	"time"

	"github.com/skill215/smppkit/codec"
	"github.com/skill215/smppkit/message"
	"github.com/skill215/smppkit/pdu"
)

// Role describes the ESME bind role, even on the server side of a Session.
type Role uint8

const (
	Transmitter Role = iota + 1
	Receiver
	Transceiver
)

// State is a snapshot of one connection generation; Closed is terminal.
type State uint8

const (
	Connected State = iota // transport open, no successful bind yet
	Binding                // local bind queued or awaiting its response
	Bound                  // messaging allowed according to the negotiated role
	Unbinding              // reject new business requests while unbind finishes
	Closed                 // transport retired; create a new Session to reconnect
)

// pending is registered before queue admission so a fast peer response cannot
// arrive before its correlation entry exists. All map access requires Session.mu.
type pending struct {
	expected pdu.CommandID
	response chan pdu.PDU
	job      *writeJob
	request  pdu.CommandID
	dataESM  byte // immutable data_sm request context, not an application pointer
}

// Session owns exactly one reader and writer. mu protects state, sequence, and
// independent inbound/outbound maps. No callbacks or I/O execute under mu.
// Inputs may be reused when Request/Notify returns. Receive and
// ReceiveNotification each have an independent single consumer.
// Other exported methods may be called concurrently. Do not mutate an input
// while a call uses it. Construct with NewSession, Dial or Server admission;
// a zero Session is not usable. Close then Wait joins the runtime, not application
// goroutines which may still be using returned values.
type Session struct {
	referenceMu                  sync.Mutex
	references8, references16    *message.References
	namespace                    string
	conn                         net.Conn
	codec                        *codec.Codec
	cfg                          Config
	server                       bool
	tlvAllowed                   bool
	mu                           sync.Mutex
	state                        State
	role                         Role
	outbindReceived              bool   // unbound ESME: application must validate credentials, then bind RX
	localUnbind, localUnbindDone bool   // queued request and correlated response
	peerUnbind, peerUnbindDone   bool   // received request and fully written ACK
	seq                          uint32 // never reused; connection retires instead of wrapping
	pending                      map[uint32]*pending
	inbound                      map[uint32]*Inbound
	err                          error
	slots                        chan struct{}
	requests, responses          chan *writeJob
	notifications                chan *writeJob
	notifySlots                  chan struct{}
	notices                      chan pdu.PDU
	incoming                     chan *Inbound
	done                         chan struct{}
	wg                           sync.WaitGroup
}

// NewSession starts an unbound ESME on an existing connection (including TLS).
// Call Bind before messaging. For MC-initiated connections, receive/validate
// outbind through ReceiveNotification first, then bind as receiver. Failure to
// construct a Session leaves ownership of conn with the caller.
func NewSession(conn net.Conn, c *codec.Codec, cfg Config) (*Session, error) {
	return newSession(conn, c, cfg, false, Connected, 0)
}
func newSession(conn net.Conn, c *codec.Codec, cfg Config, server bool, state State, role Role) (*Session, error) {
	return newSessionAtSequence(conn, c, cfg, server, state, role, 0)
}

// Bootstrap can consume an MC sequence before starting the sole Session writer.
// Initialize it before any goroutine runs so late outbind errors cannot match a
// subsequent request that accidentally reuses the outbind's sequence.
func newSessionAtSequence(conn net.Conn, c *codec.Codec, cfg Config, server bool, state State, role Role, sequence uint32) (*Session, error) {
	cfg, e := cfg.normalized()
	if e != nil {
		return nil, e
	}
	if conn == nil || c == nil {
		return nil, errors.New("smpp: connection and codec required")
	}

	s := &Session{
		conn: conn, codec: c, cfg: cfg, server: server,
		state: state, role: role, seq: sequence, tlvAllowed: c.Version() != pdu.V33,
		namespace: localID(),
		pending:   make(map[uint32]*pending), inbound: make(map[uint32]*Inbound),
		slots:         make(chan struct{}, cfg.Window),
		requests:      make(chan *writeJob, cfg.Window),
		notifications: make(chan *writeJob, cfg.NotificationLimit),
		notifySlots:   make(chan struct{}, cfg.NotificationLimit),
		notices:       make(chan pdu.PDU, cfg.NotificationLimit),
		// Reserve two additional response positions for automatic control traffic.
		responses: make(chan *writeJob, cfg.InboundLimit+2),
		incoming:  make(chan *Inbound, cfg.InboundLimit), done: make(chan struct{}),
	}
	// Register all runtime tasks before publishing the Session to callers.
	s.wg.Go(s.readLoop)
	s.wg.Go(s.writeLoop)
	s.wg.Go(s.expiryLoop)
	return s, nil
}

// Codec returns the immutable profile selected for this connection.
func (s *Session) Codec() *codec.Codec { return s.codec }

// State returns a synchronized snapshot, not a reservation for a later call.
func (s *Session) State() State { s.mu.Lock(); defer s.mu.Unlock(); return s.state }

// Done closes when shutdown begins. Use Wait to join all runtime goroutines.
func (s *Session) Done() <-chan struct{} { return s.done }

// Err returns the first terminal cause, or nil before shutdown.
func (s *Session) Err() error { s.mu.Lock(); defer s.mu.Unlock(); return s.err }

// stop publishes the terminal cause before waking waiters. Close runs outside
// mu because a transport may block while interrupting outstanding I/O.
func (s *Session) stop(err error) {
	if err == nil {
		err = ErrClosed
	}
	s.mu.Lock()
	if s.state == Closed {
		s.mu.Unlock()
		return
	}
	s.state = Closed
	s.err = err
	// Callers retain their own pending/result/Inbound handles. The terminal
	// Session must not retain unconsumed application payloads or reservations.
	s.pending = nil
	s.inbound = nil
	for len(s.incoming) > 0 {
		select {
		case <-s.incoming:
		default:
		}
	}
	for len(s.notices) > 0 {
		select {
		case <-s.notices:
		default:
		}
	}
	close(s.done)
	s.mu.Unlock()
	_ = s.conn.Close()
}

// Close aborts immediately. Wait joins the three runtime goroutines.
func (s *Session) Close() error { s.stop(ErrClosed); return nil }

// Wait joins the reader, writer and expiry loop; it does not initiate shutdown.
func (s *Session) Wait() { s.wg.Wait() }
func bindRole(id pdu.CommandID) Role {
	switch id {
	case pdu.BindTransmitterID:
		return Transmitter
	case pdu.BindReceiverID:
		return Receiver
	case pdu.BindTransceiverID:
		return Transceiver
	}
	return 0
}

// allowed requires mu. Direction is expressed relative to this endpoint, while
// bind roles are always expressed relative to the ESME.
func (s *Session) allowed(id pdu.CommandID, out bool) bool {
	if bindRole(id) != 0 {
		return !s.server && out && s.state == Connected && (!s.outbindReceived || id == pdu.BindReceiverID)
	}
	if s.state == Unbinding && !out && id == pdu.UnbindID {
		return true
	}
	if s.state != Bound {
		return false
	}
	if id == pdu.EnquireLinkID || id == pdu.UnbindID {
		return true
	}
	sendingESME := out != s.server
	switch id {
	case pdu.SubmitSMID, pdu.SubmitMultiID, pdu.QuerySMID, pdu.CancelSMID, pdu.ReplaceSMID:
		return sendingESME && (s.role == Transmitter || s.role == Transceiver)
	case pdu.DeliverSMID:
		return !sendingESME && (s.role == Receiver || s.role == Transceiver)
	case pdu.AlertNotificationID:
		return !sendingESME && (s.role == Receiver || s.role == Transceiver)
	case pdu.DataSMID:
		if sendingESME {
			return s.role == Transmitter || s.role == Transceiver
		}
		return s.role == Receiver || s.role == Transceiver
	}
	_, ok := s.codec.ResponseCommand(id)
	return ok
}

// Bind establishes the ESME role. The codec fixes the effective version; neither
// missing advertisements nor unknown version bytes trigger a fallback.
func (s *Session) Bind(ctx context.Context, b pdu.Body) (Result, error) {
	if b == nil || bindRole(b.CommandID()) == 0 {
		return Result{}, ErrState
	}
	return s.Request(ctx, b)
}

// Request returns a result even on cancellation or transport failure. No request
// is retried. Cancellation during an active write closes the connection to make
// its final byte count observable; other pending requests may become uncertain.
// This also applies if a response arrives before Write returns. An already
// correlated response wins the request outcome (nil or PeerError), even when
// cancellation must retire the transport to join that write. Unbind additionally
// requires our ACK of a concurrent peer Unbind to finish.
// A successful submit_multi response can still contain Unsuccessful SMEs;
// inspect that list even when Request returns nil error. An ordinary submission
// ACK is not final delivery. For explicit transaction mode, interpret the peer's
// reported outcome using that request's semantics; no delivery tracking is added.
// An admitted Unbind retires the connection on completion or failure. If both
// peers initiate unbind, success waits for our response and the peer's ACK write.
func (s *Session) Request(ctx context.Context, body pdu.Body) (Result, error) {
	result := Result{Outcome: NotWritten}
	ctx, cancel := context.WithTimeout(ctx, s.cfg.RequestTimeout)
	defer cancel()
	select {
	case s.slots <- struct{}{}:
	case <-ctx.Done():
		return result, ctx.Err()
	case <-s.done:
		return result, s.Err()
	}
	defer func() { <-s.slots }()
	if err := ctx.Err(); err != nil {
		return result, err
	}
	// Encode before changing protocol state, with a placeholder sequence.
	data, e := s.codec.Append(nil, pdu.PDU{Sequence: 1, Body: body})
	if e != nil {
		return result, e
	}
	if e = s.validateDataDirection(body, true); e != nil {
		return result, e
	}
	id := body.CommandID()
	expected, ok := s.codec.ResponseCommand(id)
	if !ok {
		return result, ErrState
	}
	s.mu.Lock()
	if err := ctx.Err(); err != nil {
		s.mu.Unlock()
		return result, err
	}
	if !s.allowed(id, true) || !s.tlvAllowed && hasOptional(body) {
		s.mu.Unlock()
		return result, ErrState
	}
	if s.seq == 0x7fffffff {
		s.mu.Unlock()
		s.stop(errors.New("smpp: sequence space exhausted"))
		return result, s.Err()
	}
	s.seq++
	seq := s.seq
	data[12], data[13], data[14], data[15] = byte(seq>>24), byte(seq>>16), byte(seq>>8), byte(seq)
	j := job(data)
	j.ctx = ctx
	p := &pending{expected: expected, response: make(chan pdu.PDU, 1), job: j, request: id, dataESM: requestESMClass(body)}
	s.pending[seq] = p
	if r := bindRole(id); r != 0 {
		s.state = Binding
		s.role = r
	}
	if id == pdu.UnbindID {
		s.state = Unbinding
		s.localUnbind = true
	}
	// Enqueue under the same lock used by stop. No job can enter after the
	// writer drains closed-session queues.
	select {
	case s.requests <- j:
	default:
		delete(s.pending, seq)
		if bindRole(id) != 0 {
			s.state = Connected
		}
		if id == pdu.UnbindID {
			s.state = Bound
			s.localUnbind = false
		}
		s.mu.Unlock()
		result.Sequence = seq
		return result, ErrOverloaded
	}
	s.mu.Unlock()
	result.Sequence = seq
	var response pdu.PDU
	var received bool
	select {
	case response = <-p.response:
		received = true
	case <-ctx.Done():
	case <-s.done:
	}
	// Correlation and cancellation have one linearization point. Once removed,
	// a late response cannot change bind state or complete this request.
	s.mu.Lock()
	if !received {
		select {
		case response = <-p.response:
			received = true
		default:
		}
	}
	delete(s.pending, seq)
	s.mu.Unlock()
	if !received {
		s.cancelWrite(j)
	}
	// A response proves the peer parsed the request. Its bytes may have been read
	// before net.Conn.Write returned; join the writer before inspecting counters.
	// Keep observing the request context: receiving a response does not remove
	// its deadline or allow a stalled write to hold the window until WriteTimeout.
	select {
	case <-j.done:
	case <-ctx.Done():
		s.cancelWrite(j)
		<-j.done
	case <-s.done:
		s.cancelWrite(j)
		<-j.done
	}
	result.BytesWritten = j.n
	if j.n > 0 {
		result.Outcome = PartlyWritten
	}
	if j.n == len(data) {
		result.Outcome = Written
	}
	if received {
		result.Outcome = ResponseReceived
		result.Response = response
		if id == pdu.UnbindID {
			// Receiving our response is only half of a crossed unbind: the
			// peer's ACK must also finish on the sole writer before returning.
			select {
			case <-s.done:
			case <-ctx.Done():
				s.stop(ctx.Err())
			}
			s.mu.Lock()
			complete, cause := s.unbindCompleteLocked(), s.err
			s.mu.Unlock()
			if !complete {
				return result, cause
			}
		}
		if response.Status != pdu.OK {
			return result, &PeerError{id, response.Status}
		}
		return result, nil
	}
	if bindRole(id) != 0 || id == pdu.UnbindID {
		s.stop(ctx.Err())
	}
	if ctx.Err() != nil {
		return result, ctx.Err()
	}
	if j.err != nil {
		return result, j.err
	}
	return result, s.Err()
}

// cancelWrite synchronizes queue cancellation. Once writing starts, closing the
// transport joins that write; queued jobs are completed by writer shutdown.
func (s *Session) cancelWrite(j *writeJob) {
	if j.cancelQueued(context.Canceled) {
		return
	}
	select {
	case <-j.done:
		return
	default:
		s.stop(context.Canceled)
	}
}

// Receive waits for the next application request; control traffic is handled
// internally. Use one consumer per Session and respond before InboundTimeout.
// Cancelling this wait does not close the Session or discard queued requests.
// Close discards inputs that have not been received; an already returned handle
// remains owned by the caller but cannot respond on a retired connection.
func (s *Session) Receive(ctx context.Context) (*Inbound, error) {
	select {
	case <-s.done:
		return nil, s.Err()
	default:
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	select {
	case in := <-s.incoming:
		s.mu.Lock()
		closed, err := s.state == Closed, s.err
		s.mu.Unlock()
		if closed {
			return nil, err
		}
		return in, nil
	case <-s.done:
		return nil, s.Err()
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// Shutdown attempts unbind and then closes even if the exchange fails. Call
// Wait separately when all connection goroutines must have stopped. It does not
// drain application requests; finish that work before initiating shutdown.
func (s *Session) Shutdown(ctx context.Context) error {
	_, e := s.Request(ctx, &pdu.Unbind{})
	s.stop(ErrClosed)
	return e
}

// expiryLoop bounds the lifetime of inbound handles, including replies queued
// behind a slow peer. One ticker per connection avoids a timer per request.
func (s *Session) expiryLoop() {
	interval := max(time.Millisecond, min(s.cfg.InboundTimeout/4, time.Second))
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-s.done:
			return
		case now := <-t.C:
			s.mu.Lock()
			expired := false
			for _, in := range s.inbound {
				if !now.Before(in.deadline) {
					expired = true
					break
				}
			}
			s.mu.Unlock()
			if expired {
				s.stop(errors.New("smpp: inbound response deadline exceeded"))
				return
			}
		}
	}
}

// enqueueResponse shares the shutdown lock with request admission. Full ACK
// capacity retires the connection instead of blocking the sole frame reader.
func (s *Session) enqueueResponse(j *writeJob) error {
	s.mu.Lock()
	if s.state == Closed {
		e := s.err
		s.mu.Unlock()
		return e
	}
	select {
	case s.responses <- j:
		s.mu.Unlock()
		return nil
	default:
		s.mu.Unlock()
		s.stop(ErrOverloaded)
		return ErrOverloaded
	}
}

// TLVEnabled reports negotiated optional-parameter support. A missing bind
// advertisement never silently enables TLVs, even with a 3.4/5.0 codec.
func (s *Session) TLVEnabled() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state == Bound && s.tlvAllowed
}
func hasOptional(body pdu.Body) bool {
	switch b := body.(type) {
	case *pdu.QuerySMResp:
		return len(b.Optional) > 0
	case *pdu.UnbindResp:
		return len(b.Optional) > 0
	case *pdu.EnquireLinkResp:
		return len(b.Optional) > 0
	case *pdu.ReplaceSMResp:
		return len(b.Optional) > 0
	case *pdu.CancelSMResp:
		return len(b.Optional) > 0
	case *pdu.GenericNACK:
		return len(b.Optional) > 0

	case *pdu.SubmitMulti:
		return len(b.Optional) > 0
	case *pdu.SubmitMultiResp:
		return len(b.Optional) > 0
	case *pdu.SubmitSM:
		return len(b.Optional) > 0
	case *pdu.DeliverSM:
		return len(b.Optional) > 0
	case *pdu.DataSM:
		return len(b.Optional) > 0
	case *pdu.SubmitSMResp:
		return len(b.Optional) > 0
	case *pdu.DeliverSMResp:
		return len(b.Optional) > 0
	case *pdu.DataSMResp:
		return len(b.Optional) > 0
	case *pdu.AlertNotification:
		return len(b.Optional) > 0
	}
	return false
}
