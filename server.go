// SPDX-AI-Disclosure: ai-generated
// SPDX-AI-Model: GPT-6.0 Astra
// SPDX-AI-Provider: OpenAI
// SPDX-AI-Scope: Library code generated and revised with Codex under maintainer direction.
// SPDX-AI-Date: 2026-10-28

package smpp

import (
	"context"
	"errors"
	"io"
	"net"
	"slices"
	"sync"
	"time"

	"github.com/skill215/smppkit/codec"
	"github.com/skill215/smppkit/pdu"
	"github.com/skill215/smppkit/tlv"
)

// BindInfo is the authenticated peer candidate, not yet an admitted Session.
// Fields contains credentials; applications should avoid logging it wholesale.
type BindInfo struct {
	Version    pdu.Version
	Role       Role
	Fields     pdu.BindFields
	RemoteAddr net.Addr
}

// BindDecision selects the bind response. Status OK accepts; other statuses
// reject and close the transport after the response. SystemID identifies the SMSC.
type BindDecision struct {
	Status   pdu.Status
	SystemID string
}

// ServerConfig configures bounded bind admission. Profiles and Session defaults
// are frozen by NewServer; Authenticate can run concurrently for multiple peers.
type ServerConfig struct {
	Profiles       []*codec.Codec // one per allowed version; defensively copied
	Session        Config
	BindTimeout    time.Duration // default 10 seconds
	MaxConnections int           // default 128, includes unbound connections
	// Authenticate is required; it must honor ctx. Credentials are never logged.
	Authenticate func(context.Context, BindInfo) (BindDecision, error)
}

// Server authenticates connections and creates Sessions; it does not implement
// an SMSC message store, routing policy or final-delivery tracking.
type Server struct {
	cfg      ServerConfig
	profiles map[pdu.Version]*codec.Codec
}

// NewServer validates configuration without opening a listener or goroutines.
func NewServer(cfg ServerConfig) (*Server, error) {
	var e error
	cfg.Session, e = cfg.Session.normalized()
	if e != nil {
		return nil, e
	}
	if cfg.BindTimeout == 0 {
		cfg.BindTimeout = 10 * time.Second
	}
	if cfg.MaxConnections == 0 {
		cfg.MaxConnections = 128
	}
	if cfg.BindTimeout < 0 || cfg.MaxConnections < 1 || cfg.MaxConnections > 65536 || cfg.Authenticate == nil {
		return nil, errors.New("smpp: invalid server configuration")
	}
	profiles := make(map[pdu.Version]*codec.Codec)
	for _, c := range cfg.Profiles {
		if c == nil || profiles[c.Version()] != nil {
			return nil, ErrState
		}
		profiles[c.Version()] = c
	}
	if len(profiles) == 0 {
		return nil, errors.New("smpp: server requires at least one profile")
	}
	cfg.Profiles = slices.Clone(cfg.Profiles)
	return &Server{cfg, profiles}, nil
}

// Accept takes ownership of conn, authenticates under the bind deadline, sends
// the response, and returns a bound session. TLS can be supplied by tls.Listener.
// Authenticate must return on context cancellation; no detached callback runs.
// Direct Accept callers must bound concurrency themselves; MaxConnections is
// enforced by Serve. The returned Session outlives the bind context.
func (srv *Server) Accept(ctx context.Context, conn net.Conn) (*Session, error) {
	return srv.accept(ctx, conn, 0, nil)
}

// Outbind takes ownership of an already connected MC->ESME transport, sends
// outbind, then authenticates the peer's bind_receiver using Accept's admission
// path. Version must select a configured 3.4/5.0 profile; outbind has no version
// advertisement, so the peer must be configured consistently. RX is the supported
// outbind flow. The application owns dialing/listening and TLS role selection.
// One BindTimeout covers writing, reading and authentication. Failure closes the
// connection and is never retried; the outbind may already have reached the peer.
// Like direct Accept, callers must bound concurrent handshakes themselves.
func (srv *Server) Outbind(ctx context.Context, conn net.Conn, version pdu.Version, body *pdu.Outbind) (*Session, error) {
	if body == nil || version == pdu.V33 || srv.profiles[version] == nil {
		if conn != nil {
			conn.Close()
		}
		return nil, ErrState
	}
	return srv.accept(ctx, conn, version, body)
}

func (srv *Server) accept(ctx context.Context, conn net.Conn, outbindVersion pdu.Version, outbind *pdu.Outbind) (*Session, error) {
	if conn == nil {
		return nil, ErrState
	}
	ctx, cancel := context.WithTimeout(ctx, srv.cfg.BindTimeout)
	defer cancel()
	deadline, _ := ctx.Deadline()
	if e := conn.SetDeadline(deadline); e != nil {
		conn.Close()
		return nil, e
	}
	// Wake blocked transport operations when the parent context is cancelled.
	wakeDone := make(chan struct{})
	stopWake := context.AfterFunc(ctx, func() { _ = conn.SetDeadline(time.Now()); close(wakeDone) })
	wakeStopped := false
	defer func() {
		if !wakeStopped && !stopWake() {
			<-wakeDone
		}
	}()
	fail := func(e error) (*Session, error) { conn.Close(); return nil, e }
	initialSequence := uint32(0)
	if outbind != nil {
		data, err := srv.profiles[outbindVersion].Append(nil, pdu.PDU{Sequence: 1, Body: outbind})
		if err != nil {
			return fail(err)
		}
		if err = ctx.Err(); err != nil {
			return fail(err)
		}
		if err = writeHandshake(conn, data); err != nil {
			return fail(err)
		}
		initialSequence = 1
	}
	maxFrame := 16
	for _, c := range srv.profiles {
		if c.MaxFrame() > maxFrame {
			maxFrame = c.MaxFrame()
		}
	}
	if outbind != nil {
		maxFrame = srv.profiles[outbindVersion].MaxFrame()
	}
	packet, v, e := codec.ReadBind(conn, maxFrame)
	if e != nil {
		return fail(e)
	}
	if outbind != nil && (v != outbindVersion || packet.Body.CommandID() != pdu.BindReceiverID) {
		return fail(ErrState)
	}
	c, ok := srv.profiles[v]
	if !ok {
		return fail(codec.ErrUnsupported)
	}
	// The bootstrap used the largest limit; revalidate against the chosen scope.
	if _, e = c.Append(nil, packet); e != nil {
		return fail(e)
	}
	var fields pdu.BindFields
	switch b := packet.Body.(type) {
	case *pdu.BindTransmitter:
		fields = b.BindFields
	case *pdu.BindReceiver:
		fields = b.BindFields
	case *pdu.BindTransceiver:
		fields = b.BindFields
	}
	role := bindRole(packet.Body.CommandID())
	decision, e := srv.cfg.Authenticate(ctx, BindInfo{v, role, fields, conn.RemoteAddr()})
	if e != nil {
		return fail(e)
	}
	if e = ctx.Err(); e != nil {
		return fail(e)
	}
	id := packet.Body.CommandID() | pdu.ResponseBit
	var body pdu.Body
	f := pdu.BindResponseFields{SystemID: decision.SystemID}
	if v != pdu.V33 {
		f.Optional = tlv.Fields{{Tag: tlv.SCInterfaceVersion, Value: []byte{byte(v)}}}
	}
	switch id {
	case pdu.BindTransmitterRespID:
		body = &pdu.BindTransmitterResp{BindResponseFields: f}
	case pdu.BindReceiverRespID:
		body = &pdu.BindReceiverResp{BindResponseFields: f}
	case pdu.BindTransceiverRespID:
		body = &pdu.BindTransceiverResp{BindResponseFields: f}
	}
	if decision.Status != pdu.OK && v != pdu.V33 {
		body = &pdu.Omitted{ID: id}
	}
	b, e := c.Append(nil, pdu.PDU{Sequence: packet.Sequence, Status: decision.Status, Body: body})
	if e != nil {
		return fail(e)
	}
	if e = writeHandshake(conn, b); e != nil {
		return fail(e)
	}
	if decision.Status != pdu.OK {
		return fail(&PeerError{packet.Body.CommandID(), decision.Status})
	}
	// Join any deadline wakeup before clearing it: a late wakeup must not set
	// an expired deadline on the newly established Session.
	if !stopWake() {
		<-wakeDone
		return fail(ctx.Err())
	}
	wakeStopped = true
	if e = conn.SetDeadline(time.Time{}); e != nil {
		return fail(e)
	}
	return newSessionAtSequence(conn, c, srv.cfg.Session, true, Bound, role, initialSequence)
}

// The bootstrap owns the transport exclusively until Session starts. Outbind
// and bind responses use this same bounded, deadline-controlled write path.
func writeHandshake(conn net.Conn, data []byte) error {
	for len(data) > 0 {
		n, err := conn.Write(data)
		if err != nil {
			return err
		}
		if n <= 0 || n > len(data) {
			return io.ErrShortWrite
		}
		data = data[n:]
	}
	return nil
}

// Serve accepts bounded concurrent connections. handle owns each bound session
// for its duration and must honor ctx. Cancelling ctx closes listener and all
// accepted connections; Serve joins handlers before returning.
func (srv *Server) Serve(ctx context.Context, ln net.Listener, handle func(context.Context, *Session)) error {
	if handle == nil {
		return errors.New("smpp: server handler required")
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	slots := make(chan struct{}, srv.cfg.MaxConnections)
	var wg sync.WaitGroup
	stop := context.AfterFunc(ctx, func() { _ = ln.Close() })
	defer stop()
	defer wg.Wait()
	for {
		select {
		case slots <- struct{}{}:
		case <-ctx.Done():
			return ctx.Err()
		}
		conn, e := ln.Accept()
		if e != nil {
			<-slots
			cause := ctx.Err()
			cancel()
			if cause != nil {
				return cause
			}
			return e
		}
		wg.Go(func() {
			defer func() { <-slots }()
			defer conn.Close()
			wake := context.AfterFunc(ctx, func() { conn.Close() })
			defer wake()
			s, e := srv.Accept(ctx, conn)
			if e != nil {
				return
			}
			defer func() { s.Close(); s.Wait() }()
			handle(ctx, s)
		})
	}
}
