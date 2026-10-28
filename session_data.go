// SPDX-AI-Disclosure: ai-generated
// SPDX-AI-Model: GPT-6.0 Astra
// SPDX-AI-Provider: OpenAI
// SPDX-AI-Scope: Library code generated and revised with Codex under maintainer direction.
// SPDX-AI-Date: 2026-10-28

package smpp

import (
	"github.com/skill215/smppkit/codec"
	"github.com/skill215/smppkit/pdu"
)

// validateDataDirection translates endpoint-relative traffic to the original
// request's direction. server is immutable, so this helper needs no lock. The
// Codec owns field rules; no callbacks or duplicate protocol tables run here.
// Call only after ordinary Codec validation has established a non-nil body.
func (s *Session) validateDataDirection(body pdu.Body, outgoing bool) error {
	id := body.CommandID()
	if id != pdu.DataSMID && id != pdu.DataSMRespID {
		return nil
	}
	requestOutgoing := outgoing
	if id == pdu.DataSMRespID {
		requestOutgoing = !outgoing
	}
	return s.codec.ValidateDataDirection(body, s.dataDirection(requestOutgoing))
}

func (s *Session) dataDirection(requestOutgoing bool) codec.DataDirection {
	direction := codec.DataDelivery
	if requestOutgoing != s.server {
		direction = codec.DataSubmission
	}
	return direction
}

// Snapshot the mode needed for data_sm response validation and automatic ACK
// eligibility. Applications may edit their owned Inbound.PDU later; those edits
// cannot change the original request's meaning. No payload pointer is retained.
func requestESMClass(body pdu.Body) byte {
	switch b := body.(type) {
	case *pdu.DataSM:
		return b.ESMClass
	case *pdu.SubmitSM:
		return b.ESMClass
	}
	return 0
}
