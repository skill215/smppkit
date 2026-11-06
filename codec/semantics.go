// SPDX-AI-Disclosure: ai-generated
// SPDX-AI-Model: GPT-6.0 Astra
// SPDX-AI-Provider: OpenAI
// SPDX-AI-Scope: Library code generated and revised with Codex under maintainer direction.
// SPDX-AI-Date: 2026-11-06

package codec

import (
	"github.com/skill215/smppkit/pdu"
)

// addressType retains the existing version-specific acceptance predicate while
// identifying TON and NPI separately. Names are fixed library identifiers.
func (c *Codec) addressType(ton, npi byte, tonField, npiField string) error {
	if ton > 6 {
		return invalidField(tonField, RuleValue)
	}
	if c.version == pdu.V33 {
		// 3.3 delegates NPI to GSM: retain the four-bit representation rule,
		// without imposing the later SMPP IP/WAP assignment table.
		if npi > 15 {
			return invalidField(npiField, RuleValue)
		}
	} else if !validNPI(npi) {
		return invalidField(npiField, RuleValue)
	}
	return nil
}

func (c *Codec) addressError(a pdu.Address, field string) error {
	switch field {
	case "source_addr":
		return c.addressType(a.TON, a.NPI, "source_addr_ton", "source_addr_npi")
	case "esme_addr":
		return c.addressType(a.TON, a.NPI, "esme_addr_ton", "esme_addr_npi")
	default:
		return c.addressType(a.TON, a.NPI, "dest_addr_ton", "dest_addr_npi")
	}
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
		return invalidField("esm_class", RuleValue)
	}
	if !c.validRegisteredDelivery(registered) {
		return invalidField("registered_delivery", RuleValue)
	}
	if !validDataCoding(coding, c.version) {
		return invalidField("data_coding", RuleValue)
	}
	return nil
}

func (c *Codec) validateReplacement(b *pdu.ReplaceSM) error {
	if !c.validTime(b.ScheduleDeliveryTime, false) {
		return invalidField("schedule_delivery_time", RuleTime)
	}
	if !c.validTime(b.ValidityPeriod, false) {
		return invalidField("validity_period", RuleTime)
	}
	if !c.validRegisteredDelivery(b.RegisteredDelivery) {
		return invalidField("registered_delivery", RuleValue)
	}
	if !c.validDefaultMessageID(b.DefaultMessageID) {
		return invalidField("sm_default_msg_id", RuleValue)
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
			return invalidField("message_id", RuleUnused)
		}
	case *pdu.QuerySMResp:
		if !c.validTime(b.FinalDate, true) {
			return invalidField("final_date", RuleTime)
		}
		if p.Status != pdu.OK || c.version == pdu.V33 {
			return nil
		}
		if c.version == pdu.V34 && (b.MessageState < 1 || b.MessageState > 8) || c.version == pdu.V50 && b.MessageState > 9 {
			return invalidField("message_state", RuleValue)
		}
		if b.MessageState <= 1 && b.FinalDate != "" {
			return invalidField("final_date", RuleDependency)
		}
	}
	return nil
}
