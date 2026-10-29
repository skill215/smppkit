// SPDX-AI-Disclosure: ai-generated
// SPDX-AI-Model: GPT-6.0 Astra
// SPDX-AI-Provider: OpenAI
// SPDX-AI-Scope: Library code generated and revised with Codex under maintainer direction.
// SPDX-AI-Date: 2026-10-29

package smpp

import (
	"time"

	"github.com/skill215/smppkit/codec"
	"github.com/skill215/smppkit/pdu"
	"github.com/skill215/smppkit/tlv"
)

// readLoop is the sole transport reader. Dispatch never waits for application
// handlers or a free queue slot: blocking here would also block responses to
// outbound requests, creating a deadlock when a handler makes its own Request.
func (s *Session) readLoop() {
	for {
		// One deadline covers a whole frame, including a stalled partial header/body.
		if err := s.conn.SetReadDeadline(time.Now().Add(s.cfg.ReadTimeout)); err != nil {
			if !s.finishUnbindOnWriter(err) {
				s.stop(err)
			}
			return
		}
		packet, err := s.codec.Read(s.conn)
		if err == nil {
			if packet.Body.CommandID().IsResponse() {
				err = s.dispatchResponse(packet)
			} else {
				err = s.dispatchRequest(packet)
			}
		}
		if err != nil {
			if !s.finishUnbindOnWriter(err) {
				s.stop(err)
			}
			return
		}
	}
}

// dispatchResponse shares mu with Request's cancellation decision. A response
// either claims a still-pending request or is ignored; it cannot revive a bind
// after cancellation removed that request, or after Close made state terminal.
func (s *Session) dispatchResponse(packet pdu.PDU) error {
	id := packet.Body.CommandID()
	s.mu.Lock()
	closeAfterDispatch := false
	defer func() {
		s.mu.Unlock()
		if closeAfterDispatch {
			s.stop(ErrClosed)
		}
	}()
	if s.state == Closed {
		return s.err
	}
	pending := s.pending[packet.Sequence]
	if pending == nil {
		return nil // late, duplicate or unsolicited response
	}
	if id != pending.expected && id != pdu.GenericNACKID {
		return ErrResponse
	}
	// Codec validates the configured wire profile, not bind negotiation. Apply
	// the negotiated policy to correlated responses (including errors/NACKs)
	// before publishing them. Bind responses establish that policy themselves.
	if bindRole(pending.request) == 0 && !s.tlvAllowed && hasOptional(packet.Body) {
		return ErrState
	}
	if pending.request == pdu.DataSMID {
		if err := s.codec.ValidateDataResponse(packet, pending.dataESM, s.dataDirection(true)); err != nil {
			return err
		}
	}
	if bindRole(pending.request) != 0 {
		if err := s.applyBindResponseLocked(packet); err != nil {
			return err
		}
	}
	delete(s.pending, packet.Sequence)
	// Each pending entry owns a one-element channel. Removing it before sending
	// guarantees this send cannot block or complete the same request twice.
	pending.response <- packet
	if pending.request == pdu.UnbindID {
		s.localUnbindDone = true
		closeAfterDispatch = s.unbindCompleteLocked()
	}
	return nil
}

// applyBindResponseLocked requires mu and a live, correlated bind request.
// The chosen Codec never changes. Missing advertisements disable TLVs instead
// of inferring a version or silently enabling optional parameters.
func (s *Session) applyBindResponseLocked(packet pdu.PDU) error {
	if packet.Status != pdu.OK {
		s.state = Connected
		return nil
	}
	var optional tlv.Fields
	switch b := packet.Body.(type) {
	case *pdu.BindTransmitterResp:
		optional = b.Optional
	case *pdu.BindReceiverResp:
		optional = b.Optional
	case *pdu.BindTransceiverResp:
		optional = b.Optional
	}
	enabled := false
	if advertised, ok := optional.First(tlv.SCInterfaceVersion); ok {
		if len(advertised) != 1 || pdu.Version(advertised[0]) != s.codec.Version() {
			return codec.ErrUnsupported
		}
		enabled = true
	}
	s.tlvAllowed = enabled
	s.state = Bound
	if s.heartbeatReady != nil {
		close(s.heartbeatReady)
	}
	return nil
}

// dispatchRequest checks state and publishes the inbound handle in one critical
// section. Close or an outbound unbind cannot slip between eligibility and
// admission. Inbound and outbound maps deliberately have separate sequence spaces.
func (s *Session) dispatchRequest(packet pdu.PDU) error {
	if oneWay(packet.Body.CommandID()) {
		return s.dispatchNotification(packet)
	}
	if err := s.validateDataDirection(packet.Body, false); err != nil {
		return err
	}
	id := packet.Body.CommandID()
	s.mu.Lock()
	if !s.allowed(id, false) || !s.tlvAllowed && hasOptional(packet.Body) {
		s.mu.Unlock()
		return ErrState
	}
	if id == pdu.EnquireLinkID && s.state == Unbinding {
		// The peer can have started this frame before seeing our unbind. Drain
		// it without adding an ACK that could block behind or race the final
		// unbind response. Shutdown has its own deadlines; this enquiry neither
		// restarts scheduling nor changes admission or completion obligations.
		s.mu.Unlock()
		return nil
	}
	if id == pdu.EnquireLinkID || id == pdu.UnbindID {
		if id == pdu.UnbindID {
			if s.peerUnbind {
				s.mu.Unlock()
				return ErrState
			}
			s.peerUnbind = true
			s.state = Unbinding
			s.stopHeartbeatLocked()
		}
		s.mu.Unlock()
		return s.respondControl(packet)
	}
	expected, ok := s.codec.ResponseCommand(id)
	if !ok {
		s.mu.Unlock()
		return ErrResponse
	}
	if _, duplicate := s.inbound[packet.Sequence]; duplicate || len(s.inbound) >= s.cfg.InboundLimit {
		s.mu.Unlock()
		return ErrOverloaded
	}
	in := &Inbound{
		PDU: packet, sequence: packet.Sequence, expected: expected, s: s,
		deadline: time.Now().Add(s.cfg.InboundTimeout),
		esmClass: requestESMClass(packet.Body),
	}
	s.inbound[packet.Sequence] = in
	select {
	case s.incoming <- in:
		s.mu.Unlock()
		return nil
	default:
		delete(s.inbound, packet.Sequence)
		s.mu.Unlock()
		return ErrOverloaded
	}
}

// respondControl uses the same serialized writer as application responses. An
// unbind ACK must finish before closing; crossed unbind also waits for the
// response to our own request. The completion hook coordinates both obligations.
func (s *Session) respondControl(packet pdu.PDU) error {
	unbind := packet.Body.CommandID() == pdu.UnbindID
	var body pdu.Body = &pdu.EnquireLinkResp{}
	if unbind {
		body = &pdu.UnbindResp{}
	}
	data, err := s.codec.Append(nil, pdu.PDU{Sequence: packet.Sequence, Body: body})
	if err != nil {
		return err
	}
	j := job(data)
	if unbind {
		j.after = s.finishPeerUnbind
	}
	return s.enqueueResponse(j)
}
