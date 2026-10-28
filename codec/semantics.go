// SPDX-AI-Disclosure: ai-generated
// SPDX-AI-Model: GPT-6.0 Astra
// SPDX-AI-Provider: OpenAI
// SPDX-AI-Scope: Library code generated and revised with Codex under maintainer direction.
// SPDX-AI-Date: 2026-10-28

package codec

import (
	"fmt"

	"github.com/skill215/smppkit/pdu"
)

// These checks are independent of transport, routing and carrier provisioning.
// A field error names the field, never its value (which may contain credentials
// or subscriber information). Unknown response status/error codes stay opaque.
func invalidField(name string) error {
	return fmt.Errorf("%w: %s", ErrValidation, name)
}

func (c *Codec) validAddressType(ton, npi byte) bool {
	if ton > 6 {
		return false
	}
	if c.version == pdu.V33 {
		// 3.3 delegates NPI to GSM rather than enumerating the later SMPP IP/WAP
		// assignments. Check the four-bit representation without imposing 3.4's
		// assignment table on an older network profile.
		return npi <= 15
	}
	return validNPI(npi)
}

func (c *Codec) validRegisteredDelivery(v byte) bool {
	if c.version == pdu.V33 {
		return v <= 1
	}
	// 3.4's diagram places intermediate notification at bit 4, despite its
	// heading saying "bit 5". 5.0 confirms bit 4. Bits 5..7 are reserved.
	return v&0xe0 == 0 && (c.version == pdu.V50 || v&3 != 3)
}

func (c *Codec) validDefaultMessageID(v byte) bool {
	switch c.version {
	case pdu.V33:
		return v <= 100
	case pdu.V34:
		return v != 255 // zero means unused in the command layouts
	default:
		return true
	}
}

func validDataCoding(v byte, version pdu.Version) bool {
	if version == pdu.V33 {
		// The 3.3 field is a GSM DCS, not the 3.4/5.0 SMPP coding table. Payload
		// decoding and the selected GSM coding group's semantics belong to policy.
		return true
	}
	// Retain defined GSM control groups without interpreting their payload.
	return v <= 10 || v == 13 || v == 14 || v >= 0xc0 && v <= 0xdf || v >= 0xf0
}

func (c *Codec) validESMClass(v byte, id pdu.CommandID) bool {
	if c.version == pdu.V33 {
		// 3.3 defines submission as unused; its delivery marker's numeric
		// assignment is delegated to the historical applications guide.
		return id == pdu.DeliverSMID || v == 0
	}
	typ := v & 0x3c
	if c.version == pdu.V34 && (id == pdu.SubmitSMID || id == pdu.SubmitMultiID) {
		return typ == 0 || typ == 8 || typ == 16
	}
	// data_sm is bidirectional; standalone Codec accepts the union. The low
	// two mode bits on 3.4 deliver_sm are explicitly ignored, not reserved.
	switch typ {
	case 0, 4, 8, 16, 24, 32:
		return true
	}
	return false
}

func (c *Codec) validateMessageFlags(id pdu.CommandID, esm, registered, coding byte) error {
	if !c.validESMClass(esm, id) {
		return invalidField("esm_class")
	}
	if !c.validRegisteredDelivery(registered) {
		return invalidField("registered_delivery")
	}
	if !validDataCoding(coding, c.version) {
		return invalidField("data_coding")
	}
	return nil
}

func (c *Codec) validateReplacement(b *pdu.ReplaceSM) error {
	if !c.validTime(b.ScheduleDeliveryTime, false) {
		return invalidField("schedule_delivery_time")
	}
	if !c.validTime(b.ValidityPeriod, false) {
		return invalidField("validity_period")
	}
	if !c.validRegisteredDelivery(b.RegisteredDelivery) {
		return invalidField("registered_delivery")
	}
	if !c.validDefaultMessageID(b.DefaultMessageID) {
		return invalidField("sm_default_msg_id")
	}
	return nil
}

// validateResponseSemantics needs header status as well as the typed body.
// Error responses may carry zero placeholders, which do not report message
// state. An omitted negative body is handled separately by the frame codec.
func (c *Codec) validateResponseSemantics(p pdu.PDU) error {
	switch b := p.Body.(type) {
	case *pdu.DeliverSMResp:
		if c.version != pdu.V33 && b.MessageID != "" {
			return invalidField("deliver_sm_resp.message_id is unused")
		}
	case *pdu.QuerySMResp:
		if !c.validTime(b.FinalDate, true) {
			return invalidField("final_date")
		}
		if p.Status != pdu.OK || c.version == pdu.V33 {
			return nil
		}
		if c.version == pdu.V34 && (b.MessageState < 1 || b.MessageState > 8) || c.version == pdu.V50 && b.MessageState > 9 {
			return invalidField("message_state")
		}
		if b.MessageState <= 1 && b.FinalDate != "" {
			return invalidField("final_date for non-final message")
		}
	}
	return nil
}
