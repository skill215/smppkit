// SPDX-AI-Disclosure: ai-generated
// SPDX-AI-Model: GPT-6.0 Astra
// SPDX-AI-Provider: OpenAI
// SPDX-AI-Scope: Library code generated and revised with Codex under maintainer direction.
// SPDX-AI-Date: 2026-10-28

package smpp

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/skill215/smppkit/message"
	"github.com/skill215/smppkit/pdu"
)

// TextMessage is the transport-independent logical text input.
type TextMessage = message.Text

// SegmentResult preserves one planned part, including parts never attempted.
// Number is one-based; MessageID is the peer's opaque acceptance identifier.
// Err is set only for an attempted Request that failed. NotAttempted parts have
// zero sequence/byte counts and nil Err; the enclosing SendText error explains
// why scheduling stopped. If preparation fails, there is no segment plan.
type SegmentResult struct {
	Number    int
	Result    Result
	MessageID string
	Err       error
}

// SendResult identifies one local send attempt. Segments stay in planned order
// even on partial failure; success is submission acceptance, not final delivery.
type SendResult struct {
	ID, Encoding string
	Mode         message.Mode
	Segments     []SegmentResult
}

func localID() string {
	var b [16]byte
	if _, e := rand.Read(b[:]); e != nil {
		panic("smpp: system entropy unavailable")
	}
	return hex.EncodeToString(b[:])
}
func (s *Session) defaultReferences(bits int) *message.References {
	s.referenceMu.Lock()
	defer s.referenceMu.Unlock()
	ref := &s.references16
	max := 4096
	if bits == 8 {
		ref = &s.references8
		max = 256
	}
	if *ref == nil {
		*ref, _ = message.NewReferences(bits, max, 5*time.Minute)
	}
	return *ref
}

// SendText validates the entire message and every segment before the first
// write. policy must describe the peer's encoding/concatenation capabilities.
// Optional references overrides the session-scoped allocator, e.g. to share a
// sending domain across binds. At most one may be supplied; it is used only for
// multipart plans and must then be non-nil with the plan's reference width.
// Segments execute sequentially through Request's
// existing window; failure stops scheduling and retains every segment result.
// Neither a positive response nor this method's success implies final delivery.
func (s *Session) SendText(ctx context.Context, msg TextMessage, policy message.Policy, references ...*message.References) (SendResult, error) {
	result := SendResult{ID: localID()}
	if len(references) > 1 {
		return result, message.ErrMetadata
	}
	plan, e := message.Prepare(s.codec.Version(), msg, policy)
	if e != nil {
		return result, e
	}
	result.Encoding = plan.Encoding()
	result.Mode = plan.Mode()
	result.Segments = make([]SegmentResult, plan.Count())
	for i := range result.Segments {
		result.Segments[i].Number = i + 1
		result.Segments[i].Result.Outcome = NotAttempted
	}

	if e = ctx.Err(); e != nil {
		return result, e
	}
	// Placeholder reference preflight catches invalid local fields before
	// consuming a scarce lease. Changing the reference does not change sizes.
	parts, e := plan.Materialize(0)
	if e != nil {
		return result, e
	}
	bodies := make([]pdu.Body, len(parts))
	makeBody := func(part pdu.SMFields) pdu.Body {
		if s.server {
			return &pdu.DeliverSM{SMFields: part}
		}
		return &pdu.SubmitSM{SMFields: part}
	}
	for i, part := range parts {
		bodies[i] = makeBody(part)
		if _, e = s.codec.Append(nil, pdu.PDU{Sequence: 1, Body: bodies[i]}); e != nil {
			return result, e
		}
	}
	s.mu.Lock()
	allowed := s.allowed(bodies[0].CommandID(), true) && (s.tlvAllowed || !hasOptional(bodies[0]))
	s.mu.Unlock()
	if !allowed {
		return result, ErrState
	}
	if e = ctx.Err(); e != nil {
		return result, e
	}
	if plan.Multipart() {
		refs := s.defaultReferences(plan.ReferenceBits())
		if len(references) == 1 {
			refs = references[0]
		}
		if refs == nil || refs.Bits() != plan.ReferenceBits() {
			return result, message.ErrMetadata
		}
		// Once reserved, retain the lease even if cancellation races scheduling:
		// absence of a correlated response cannot establish safe reference reuse.
		ref, err := refs.Reserve(time.Now())
		if err != nil {
			return result, err
		}
		parts, e = plan.Materialize(ref)
		if e != nil {
			return result, e
		}
		for i, part := range parts {
			bodies[i] = makeBody(part)
		}
	}
	for i, body := range bodies {
		if e = ctx.Err(); e != nil {
			return result, e
		}
		sr := &result.Segments[i]
		sr.Result, e = s.Request(ctx, body)
		sr.Err = e
		switch b := sr.Result.Response.Body.(type) {
		case *pdu.SubmitSMResp:
			sr.MessageID = b.MessageID
		case *pdu.DeliverSMResp:
			sr.MessageID = b.MessageID
		}
		if e != nil {
			return result, e
		}
	}
	return result, nil
}

// AdmitMessage admits and responds to one messaging request. ACK success means
// bounded memory admission only. The complete-message consumer runs separately
// through Receiver.Receive; its result cannot retract these ACKs.
// Explicit transaction submissions (mode 2) return ErrState before admission or
// response, leaving the handle available for application Respond. Memory alone
// cannot establish their delivery outcome. Use Receive/Respond for that mode.
// For mode 0, the caller must configure a non-transaction peer default. Delivery
// requests from the MC are not subject to this submission-mode restriction.
func AdmitMessage(ctx context.Context, receiver *message.Receiver, in *Inbound, namespace string) error {
	if receiver == nil || in == nil {
		return message.ErrMetadata
	}
	// Check immutable request identity/mode, not the application-editable PDU.
	// Both supported transaction forms (3.4 data_sm; 5.0 data_sm/submit_sm)
	// require an actual delivery result. Also refuse mode 2 on 3.4 submit_sm
	// here rather than misrepresenting it as ordinary memory acceptance.
	if in.s.server && in.s.codec.Version() != pdu.V33 &&
		(in.expected == pdu.DataSMRespID || in.expected == pdu.SubmitSMRespID) && in.esmClass&3 == 2 {
		return fmt.Errorf("%w: buffered ACK cannot report transaction delivery", ErrState)
	}
	if namespace == "" {
		namespace = in.s.namespace
	}
	var fields pdu.SMFields
	var response pdu.Body
	// Generated message IDs identify individual admitted requests, not delivery.
	id := localID()
	if in.s.codec.Version() == pdu.V33 {
		id = id[:8]
	}
	switch b := in.PDU.Body.(type) {
	case *pdu.SubmitSM:
		fields = b.SMFields
		response = &pdu.SubmitSMResp{MessageResponseFields: pdu.MessageResponseFields{MessageID: id}}
	case *pdu.DeliverSM:
		fields = b.SMFields
		response = &pdu.DeliverSMResp{}
	case *pdu.DataSM:
		fields = pdu.SMFields{ServiceType: b.ServiceType, Source: b.Source, Destination: b.Destination, ESMClass: b.ESMClass, RegisteredDelivery: b.RegisteredDelivery, DataCoding: b.DataCoding, Optional: b.Optional}
		response = &pdu.DataSMResp{MessageResponseFields: pdu.MessageResponseFields{MessageID: id}}
	default:
		return ErrState
	}
	// Commit memory admission before replying OK. A later response-write error
	// cannot roll back content already admitted or consumed by the application.
	_, admissionErr := receiver.Admit(namespace, fields, time.Now())
	status := pdu.OK
	if admissionErr != nil {
		status = pdu.InvalidOptionalParameter
		if errors.Is(admissionErr, message.ErrCapacity) {
			status = pdu.Throttled
		}
		if in.s.codec.Version() != pdu.V33 {
			response = &pdu.Omitted{ID: response.CommandID()}
		}
	}
	if e := in.Respond(ctx, status, response); e != nil {
		return e
	}
	return admissionErr
}

// RunReceiver is the sole inbound consumer until it returns. It admits single
// parts and multipart messages, replies per segment, and sweeps expired groups
// every second. Consume completed messages with Receiver.Receive and events with
// Receiver.Events. On exit it closes the session; errors never discard already
// queued messages. Use AdmitMessage directly for a mixed-command dispatcher.
// The one-second expiry ticker is checked between admissions; a blocked ACK
// write can delay the sweep until that write finishes or its deadline expires.
// An unsupported transaction submission returns ErrState and closes the Session
// without admission or ACK; use Receive/Respond to implement transaction mode.
func (s *Session) RunReceiver(ctx context.Context, receiver *message.Receiver, namespace string) error {
	if receiver == nil {
		return message.ErrMetadata
	}
	defer s.Close()
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-s.done:
			return s.Err()
		case now := <-tick.C:
			if e := receiver.Expire(now); e != nil {
				return e
			}
		case in := <-s.incoming:
			if e := AdmitMessage(ctx, receiver, in, namespace); e != nil {
				return e
			}
		}
	}
}
