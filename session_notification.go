// SPDX-AI-Disclosure: ai-generated
// SPDX-AI-Model: GPT-6.0 Astra
// SPDX-AI-Provider: OpenAI
// SPDX-AI-Scope: Library code generated and revised with Codex under maintainer direction.
// SPDX-AI-Date: 2026-10-28

package smpp

import (
	"context"
	"errors"

	"github.com/skill215/smppkit/pdu"
)

// Notify sends alert_notification from a bound MC to an RX/TRX ESME. It uses
// the sole Session writer and sequence space, but no request window or pending
// response. For pre-bind outbind, use Server.Outbind instead. Other bodies are
// rejected. Success means Written, never peer acceptance or final delivery.
// Admission is bounded separately by NotificationLimit and RequestTimeout.
// Cancelling a queued write sends nothing; cancelling an active write closes
// the transport to expose its final byte count. No notification is retried.
func (s *Session) Notify(ctx context.Context, body pdu.Body) (Result, error) {
	result := Result{Outcome: NotWritten}
	ctx, cancel := context.WithTimeout(ctx, s.cfg.RequestTimeout)
	defer cancel()
	select {
	case s.notifySlots <- struct{}{}:
	case <-ctx.Done():
		return result, ctx.Err()
	case <-s.done:
		return result, s.Err()
	}
	defer func() { <-s.notifySlots }()
	if err := ctx.Err(); err != nil {
		return result, err
	}
	data, err := s.codec.Append(nil, pdu.PDU{Sequence: 1, Body: body})
	if err != nil {
		return result, err
	}
	if body.CommandID() != pdu.AlertNotificationID {
		return result, ErrState
	}
	s.mu.Lock()
	if err := ctx.Err(); err != nil {
		s.mu.Unlock()
		return result, err
	}
	if !s.allowed(body.CommandID(), true) || !s.tlvAllowed && hasOptional(body) {
		s.mu.Unlock()
		return result, ErrState
	}
	// A cancelled queued job can still occupy the channel until the writer
	// drains it. Reject before allocating a sequence if that queue is full.
	if len(s.notifications) == cap(s.notifications) {
		s.mu.Unlock()
		return result, ErrOverloaded
	}
	if s.seq == 0x7fffffff {
		s.mu.Unlock()
		s.stop(errors.New("smpp: sequence space exhausted"))
		return result, s.Err()
	}
	s.seq++
	result.Sequence = s.seq
	data[12], data[13], data[14], data[15] = byte(s.seq>>24), byte(s.seq>>16), byte(s.seq>>8), byte(s.seq)
	j := job(data)
	j.ctx = ctx
	s.notifications <- j // all producers hold mu; capacity was checked above
	s.mu.Unlock()
	select {
	case <-j.done:
	case <-ctx.Done():
		s.cancelWrite(j)
	case <-s.done:
		s.cancelWrite(j)
	}
	<-j.done // join an active write before observing its byte count
	result.BytesWritten = j.n
	if j.n > 0 {
		result.Outcome = PartlyWritten
	}
	if j.n == len(data) {
		result.Outcome = Written
	}
	// Completed successful writing wins a concurrent cancellation. No peer
	// response exists to provide a second completion point for notifications.
	if j.err == nil {
		return result, nil
	}
	if ctx.Err() != nil {
		return result, ctx.Err()
	}
	return result, j.err
}

// ReceiveNotification returns an owned alert_notification or pre-bind outbind.
// Use one notification consumer independently of Receive/RunReceiver. Never ACK
// these packets: there is no response handle, inbound reservation or ACK timer.
// Cancelling this wait leaves the session and queued notifications intact.
// An outbind contains credentials: validate them before Bind with BindReceiver;
// on refusal Close the session. No automatic bind or credential logging occurs.
func (s *Session) ReceiveNotification(ctx context.Context) (pdu.PDU, error) {
	select {
	case <-s.done:
		return pdu.PDU{}, s.Err()
	default:
	}
	if err := ctx.Err(); err != nil {
		return pdu.PDU{}, err
	}
	select {
	case packet := <-s.notices:
		s.mu.Lock()
		closed, err := s.state == Closed, s.err
		s.mu.Unlock()
		if closed {
			return pdu.PDU{}, err
		}
		return packet, nil
	case <-ctx.Done():
		return pdu.PDU{}, ctx.Err()
	case <-s.done:
		return pdu.PDU{}, s.Err()
	}
}

func oneWay(id pdu.CommandID) bool {
	return id == pdu.AlertNotificationID || id == pdu.OutbindID
}

// dispatchNotification never waits for an application or occupies the request
// map. Overflow closes the connection rather than blocking response dispatch or
// silently dropping a notification. Duplicate sequence values are not deduped:
// there is no response/retry identity contract for these notifications.
func (s *Session) dispatchNotification(packet pdu.PDU) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := packet.Body.CommandID()
	if id == pdu.OutbindID {
		if s.server || s.state != Connected || s.outbindReceived {
			return ErrState
		}
		s.outbindReceived = true
	} else if !s.allowed(id, false) || !s.tlvAllowed && hasOptional(packet.Body) {
		return ErrState
	}
	select {
	case s.notices <- packet:
		return nil
	default:
		return ErrOverloaded
	}
}
