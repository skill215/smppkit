package smpp

import (
	"context"
	"sync/atomic"
	"time"

	"github.com/skill215/smppkit/pdu"
)

// Inbound is bound to its originating session. Respond succeeds at most once.
// PDU is owned by this handle; applications must not mutate it concurrently.
// Response correlation always uses the original request identity, even if the
// application edits its owned PDU.
type Inbound struct {
	PDU       pdu.PDU
	sequence  uint32
	expected  pdu.CommandID
	esmClass  byte // original message mode: response validation and buffered ACK eligibility
	s         *Session
	deadline  time.Time
	responded atomic.Bool
}

// Respond validates a response before claiming the handle. Local validation or
// an already-cancelled context leaves it available for correction. After the
// claim, even a transport failure consumes the handle: blindly retrying an ACK
// could duplicate a response whose final peer-visible outcome is unknown.
// Cancellation after queue admission closes the originating Session.
// data_sm_resp fields are checked against the original request's direction/mode,
// independently of edits to PDU. Contextual validation does not claim the handle.
func (in *Inbound) Respond(ctx context.Context, status pdu.Status, body pdu.Body) error {
	if body == nil {
		return ErrResponse
	}
	if in.expected == 0 || body.CommandID() != in.expected && body.CommandID() != pdu.GenericNACKID {
		return ErrResponse
	}
	data, e := in.s.codec.Append(nil, pdu.PDU{Sequence: in.sequence, Status: status, Body: body})
	if e != nil {
		return e
	}
	if in.expected == pdu.DataSMRespID {
		packet := pdu.PDU{Sequence: in.sequence, Status: status, Body: body}
		if e = in.s.codec.ValidateDataResponse(packet, in.esmClass, in.s.dataDirection(false)); e != nil {
			return e
		}
	}
	if e := ctx.Err(); e != nil {
		return e
	}
	in.s.mu.Lock()
	optionalAllowed := in.s.tlvAllowed
	in.s.mu.Unlock()
	if !optionalAllowed && hasOptional(body) {
		return ErrState
	}
	if !in.responded.CompareAndSwap(false, true) {
		return ErrResponded
	}
	j := job(data)
	// Keep the inbound slot until the ACK is fully written, not just queued.
	j.after = func() { in.s.mu.Lock(); delete(in.s.inbound, in.sequence); in.s.mu.Unlock() }
	if e := in.s.enqueueResponse(j); e != nil {
		return e
	}
	select {
	case <-j.done:
		return j.err
	case <-in.s.done:
		return in.s.Err()
	case <-ctx.Done():
		in.s.stop(ctx.Err())
		return ctx.Err()
	}
}
