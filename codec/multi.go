// SPDX-AI-Disclosure: ai-generated
// SPDX-AI-Model: GPT-6.0 Astra
// SPDX-AI-Provider: OpenAI
// SPDX-AI-Scope: Library code generated and revised with Codex under maintainer direction.
// SPDX-AI-Date: 2026-10-28

package codec

import (
	"encoding/binary"
	"fmt"

	"github.com/skill215/smppkit/internal/wire"
	"github.com/skill215/smppkit/pdu"
)

// 3.3 defines an octet count without 3.4's explicit 254-destination ceiling.
func (c *Codec) destinationLimit() int {
	if c.version == pdu.V34 {
		return 254
	}
	return 255
}

// multiFields adapts only the common suffix for validation/encoding. A private
// helper avoids expanding the public PDU with a meaningless single destination.
func multiFields(b *pdu.SubmitMulti) pdu.SMFields {
	return pdu.SMFields{ESMClass: b.ESMClass, ProtocolID: b.ProtocolID, PriorityFlag: b.PriorityFlag,
		ScheduleDeliveryTime: b.ScheduleDeliveryTime, ValidityPeriod: b.ValidityPeriod,
		RegisteredDelivery: b.RegisteredDelivery, ReplaceIfPresent: b.ReplaceIfPresent,
		DataCoding: b.DataCoding, DefaultMessageID: b.DefaultMessageID,
		ShortMessage: b.ShortMessage, Optional: b.Optional}
}
func (c *Codec) validateMulti(b *pdu.SubmitMulti) error {
	if len(b.Destinations) < 1 || len(b.Destinations) > c.destinationLimit() {
		return fmt.Errorf("%w: number_of_dests", ErrValidation)
	}
	for _, d := range b.Destinations {
		switch d.Kind {
		case pdu.SMEAddress:
			if d.ListName != "" {
				return fmt.Errorf("%w: inactive distribution-list arm", ErrValidation)
			}
		case pdu.DistributionList:
			if d.Address != (pdu.Address{}) {
				return fmt.Errorf("%w: inactive SME address arm", ErrValidation)
			}
		default:
			return fmt.Errorf("%w: dest_flag", ErrValidation)
		}
	}
	if b.ReplaceIfPresent > 1 {
		return fmt.Errorf("%w: replace_if_present_flag", ErrValidation)
	}
	// 3.4 reserves this field; 3.3 permits replacement for one explicit SME only.
	if b.ReplaceIfPresent != 0 && (c.version == pdu.V34 || c.version == pdu.V33 && (len(b.Destinations) != 1 || b.Destinations[0].Kind != pdu.SMEAddress)) {
		return fmt.Errorf("%w: multi-destination replacement", ErrValidation)
	}
	return c.validateSM(multiFields(b), pdu.SubmitMultiID)
}
func (c *Codec) readMulti(r *wire.Reader) *pdu.SubmitMulti {
	b := &pdu.SubmitMulti{ServiceType: r.C(6, "service_type"), Source: c.readAddr(r, 21, "source_addr")}
	n := int(r.U8("number_of_dests"))
	if r.Err != nil {
		return b
	}
	if n < 1 || n > c.destinationLimit() {
		r.Fail("number_of_dests", "outside version bound")
		return b
	}
	// Counts are one octet. Stop on the first bad element; never walk a claimed
	// list after truncation or interpret an unknown flag as another layout.
	for i := 0; i < n && r.Err == nil; i++ {
		d := pdu.MultiDestination{Kind: pdu.DestinationKind(r.U8("dest_flag"))}
		switch d.Kind {
		case pdu.SMEAddress:
			d.Address = c.readAddr(r, 21, "destination_addr")
		case pdu.DistributionList:
			d.ListName = r.C(21, "dl_name")
		default:
			r.Fail("dest_flag", "unknown destination kind")
		}
		b.Destinations = append(b.Destinations, d)
	}
	var fields pdu.SMFields
	c.readSMTail(r, &fields, pdu.SubmitMultiID)
	b.ESMClass, b.ProtocolID, b.PriorityFlag = fields.ESMClass, fields.ProtocolID, fields.PriorityFlag
	b.ScheduleDeliveryTime, b.ValidityPeriod = fields.ScheduleDeliveryTime, fields.ValidityPeriod
	b.RegisteredDelivery, b.ReplaceIfPresent, b.DataCoding, b.DefaultMessageID = fields.RegisteredDelivery, fields.ReplaceIfPresent, fields.DataCoding, fields.DefaultMessageID
	b.ShortMessage, b.Optional = fields.ShortMessage, fields.Optional
	if r.Err == nil {
		r.Err = c.validateMulti(b)
	}
	return b
}
func (c *Codec) writeMulti(w *wire.Writer, b *pdu.SubmitMulti) {
	if err := c.validateMulti(b); err != nil {
		w.Err = err
		return
	}
	w.C(b.ServiceType, 6, "service_type")
	c.writeAddr(w, b.Source, 21, "source_addr")
	w.U8(byte(len(b.Destinations)))
	for _, d := range b.Destinations {
		w.U8(byte(d.Kind))
		if d.Kind == pdu.SMEAddress {
			c.writeAddr(w, d.Address, 21, "destination_addr")
		} else {
			w.C(d.ListName, 21, "dl_name")
		}
	}
	c.writeSMTail(w, multiFields(b), pdu.SubmitMultiID)
}
func (c *Codec) readMultiResp(r *wire.Reader) *pdu.SubmitMultiResp {
	b := &pdu.SubmitMultiResp{MessageID: r.C(c.messageIDLimit(), "message_id")}
	n := int(r.U8("no_unsuccess"))
	for i := 0; i < n && r.Err == nil; i++ {
		failure := pdu.UnsuccessfulSME{Address: c.readAddr(r, 21, "unsuccess_sme.address")}
		status := r.Bytes(4, "error_status_code")
		if len(status) == 4 {
			failure.Status = pdu.Status(binary.BigEndian.Uint32(status))
		}
		b.Unsuccessful = append(b.Unsuccessful, failure)
	}
	b.Optional = c.readTLVs(r, pdu.SubmitMultiRespID)
	return b
}
func (c *Codec) writeMultiResp(w *wire.Writer, b *pdu.SubmitMultiResp) {
	if len(b.Unsuccessful) > 255 {
		w.Err = fmt.Errorf("%w: no_unsuccess", ErrValidation)
		return
	}
	w.C(b.MessageID, c.messageIDLimit(), "message_id")
	w.U8(byte(len(b.Unsuccessful)))
	for _, failure := range b.Unsuccessful {
		c.writeAddr(w, failure.Address, 21, "unsuccess_sme.address")
		var status [4]byte
		binary.BigEndian.PutUint32(status[:], uint32(failure.Status))
		w.Bytes(status[:])
	}
	c.writeTLVs(w, b.Optional, pdu.SubmitMultiRespID)
}
