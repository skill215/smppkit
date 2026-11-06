// SPDX-AI-Disclosure: ai-generated
// SPDX-AI-Model: GPT-6.0 Astra
// SPDX-AI-Provider: OpenAI
// SPDX-AI-Scope: Library code generated and revised with Codex under maintainer direction.
// SPDX-AI-Date: 2026-11-06

package codec

import (
	"encoding/binary"
	"fmt"
	"io"
	"reflect"

	"github.com/skill215/smppkit/internal/wire"
	"github.com/skill215/smppkit/pdu"
	"github.com/skill215/smppkit/tlv"
)

// FieldError provides safe field/offset diagnostics without exposing values.
type FieldError = wire.Error

// Read rejects invalid lengths after four bytes, before acquiring a body. The
// caller owns partial-frame deadlines (Session applies a finite read timeout).
func (c *Codec) Read(r io.Reader) (pdu.PDU, error) {
	b, e := ReadFrame(r, c.max)
	if e != nil {
		return pdu.PDU{}, e
	}
	p, e := c.Decode(b)
	if e != nil {
		e = readFailure(e, len(b), true)
	}
	return p, e
}

// Decode requires exactly one complete frame. Returned payloads are owned.
// Failures wrap DecodeError while preserving prior error causes. Its context
// makes no assertion about stream consumption; use Read for that information.
// Standard byte fields may share one owned body allocation, but each field's
// capacity ends at its length: appending cannot overwrite a sibling field.
func (c *Codec) Decode(src []byte) (pdu.PDU, error) {
	p, err := c.decode(src)
	if err != nil {
		return p, decodeFailure(err, 0, src, c.max)
	}
	return p, nil
}

func (c *Codec) decode(src []byte) (pdu.PDU, error) {
	var p pdu.PDU
	if !validFrame(src, c.max) {
		return p, &categorizedError{DecodeFraming, fmt.Errorf("%w: command_length", ErrValidation)}
	}
	id := pdu.CommandID(binary.BigEndian.Uint32(src[4:8]))
	p.Status = pdu.Status(binary.BigEndian.Uint32(src[8:12]))
	p.Sequence = binary.BigEndian.Uint32(src[12:16])
	if e := header(id, p.Sequence, p.Status); e != nil {
		return p, e
	}
	if standard(id) && !c.supported(id) {
		return p, unsupportedField("command_id", RuleCommand)
	}
	b := append([]byte(nil), src[16:]...)
	if !standard(id) {
		if ext, ok := c.commands[id]; ok {
			if ext == nil {
				return p, unsupportedField("command_id", RuleVersion)
			}
			body, e := ext.DecodeBody(b)
			if e != nil {
				return p, &categorizedError{DecodeExtension, e}
			}
			if nilBody(body) || body.CommandID() != id {
				return p, &categorizedError{DecodeExtension, ErrValidation}
			}
			p.Body = body
			return p, nil
		}
		p.Body = &pdu.Opaque{ID: id, Data: b}
		return p, nil
	}
	if len(b) == 0 && p.Status != pdu.OK && hasResponseBody(id) && c.version != pdu.V33 {
		p.Body = &pdu.Omitted{ID: id}
		return p, nil
	}
	body, e := c.decodeBody(id, b)
	if e != nil {
		return p, e
	}
	p.Body = body
	if e := c.validateResponseSemantics(p); e != nil {
		p.Body = nil // preserve header diagnostics without exposing an invalid body
		return p, e
	}
	return p, nil
}
func nilBody(b pdu.Body) bool {
	if b == nil {
		return true
	}
	v := reflect.ValueOf(b)
	return v.Kind() == reflect.Pointer && v.IsNil()
}
func header(id pdu.CommandID, seq uint32, status pdu.Status) error {
	if seq > 0x7fffffff || (seq == 0 && id != pdu.GenericNACKID) {
		return invalidField("sequence_number", RuleValue)
	}
	if (!id.IsResponse() && status != pdu.OK) || (id == pdu.GenericNACKID && status == pdu.OK) {
		return invalidField("command_status", RuleStatus)
	}
	return nil
}
func hasResponseBody(id pdu.CommandID) bool {
	switch id {
	case pdu.BindReceiverRespID, pdu.BindTransmitterRespID, pdu.BindTransceiverRespID, pdu.SubmitSMRespID, pdu.DeliverSMRespID, pdu.QuerySMRespID, pdu.DataSMRespID, pdu.SubmitMultiRespID:
		return true
	}
	return false
}

// Append returns dst at its original length on error. Bytes before that length
// are untouched. Encoding never mutates the PDU or writes an invalid prefix.
func (c *Codec) Append(dst []byte, p pdu.PDU) ([]byte, error) {
	original := dst
	if nilBody(p.Body) {
		return original, invalidField("body", RuleBody)
	}
	id := p.Body.CommandID()
	if e := header(id, p.Sequence, p.Status); e != nil {
		return original, e
	}
	if standard(id) && !c.supported(id) {
		return original, unsupportedField("command_id", RuleCommand)
	}
	w := wire.Writer{Data: make([]byte, 16), Limit: c.max}
	if omitted, ok := p.Body.(*pdu.Omitted); ok {
		if omitted == nil || !hasResponseBody(id) || p.Status == pdu.OK || c.version == pdu.V33 {
			return original, invalidField("body", RuleBody)
		}
	} else if !standard(id) {
		if ext, ok := c.commands[id]; ok {
			if ext == nil {
				return original, unsupportedField("command_id", RuleVersion)
			}
			b, e := ext.AppendBody(nil, p.Body)
			if e != nil {
				return original, e
			}
			w.Bytes(b)
		} else if raw, ok := p.Body.(*pdu.Opaque); ok {
			w.Bytes(raw.Data)
		} else {
			return original, invalidField("body", RuleBody)
		}
	} else {
		if e := c.validateResponseSemantics(p); e != nil {
			return original, e
		}
		c.encodeBody(&w, p.Body)
	}
	if w.Err == wire.ErrLimit {
		return original, fmt.Errorf("%w: frame exceeds MaxFrame", ErrValidation)
	}
	if w.Err != nil {
		return original, w.Err
	}
	if len(w.Data) > c.max {
		return original, fmt.Errorf("%w: frame exceeds MaxFrame", ErrValidation)
	}
	binary.BigEndian.PutUint32(w.Data, uint32(len(w.Data)))
	binary.BigEndian.PutUint32(w.Data[4:], uint32(id))
	binary.BigEndian.PutUint32(w.Data[8:], uint32(p.Status))
	binary.BigEndian.PutUint32(w.Data[12:], p.Sequence)
	return append(dst, w.Data...), nil
}
func (c *Codec) messageIDLimit() int {
	if c.version == pdu.V33 {
		return 9
	}
	return 65
}
func (c *Codec) addressLimit(id pdu.CommandID) int {
	if id == pdu.DataSMID || id == pdu.AlertNotificationID {
		return 65
	}
	return 21
}

func (c *Codec) readAddr(r *wire.Reader, max int, field string) pdu.Address {
	a := pdu.Address{TON: r.U8(field + ".ton"), NPI: r.U8(field + ".npi"), Value: r.C(max, field)}
	if r.Err == nil {
		r.Err = c.addressError(a, field)
	}
	return a
}
func (c *Codec) writeAddr(w *wire.Writer, a pdu.Address, max int, field string) {
	if w.Err != nil {
		return
	}
	if err := c.addressError(a, field); err != nil {
		w.Err = err
		return
	}
	w.U8(a.TON)
	w.U8(a.NPI)
	w.C(a.Value, max, field)
}
func (c *Codec) readBind(r *wire.Reader) pdu.BindFields {
	b := pdu.BindFields{SystemID: r.C(16, "system_id"), Password: r.C(9, "password"), SystemType: r.C(13, "system_type")}
	b.InterfaceVersion = pdu.Version(r.U8("interface_version"))
	b.AddrTON = r.U8("addr_ton")
	b.AddrNPI = r.U8("addr_npi")
	b.AddressRange = r.C(41, "address_range")
	if r.Err == nil {
		r.Err = c.addressType(b.AddrTON, b.AddrNPI, "addr_ton", "addr_npi")
	}
	if b.InterfaceVersion != c.version {
		failSemantic(r, "interface_version", RuleProfile, "profile mismatch")
	}
	return b
}
func (c *Codec) writeBind(w *wire.Writer, b pdu.BindFields) {
	if err := c.addressType(b.AddrTON, b.AddrNPI, "addr_ton", "addr_npi"); err != nil {
		w.Err = err
		return
	}
	w.C(b.SystemID, 16, "system_id")
	w.C(b.Password, 9, "password")
	w.C(b.SystemType, 13, "system_type")
	w.U8(byte(b.InterfaceVersion))
	w.U8(b.AddrTON)
	w.U8(b.AddrNPI)
	w.C(b.AddressRange, 41, "address_range")
	if b.InterfaceVersion != c.version {
		w.Err = invalidField("interface_version", RuleProfile)
	}
}
func (c *Codec) readSM(r *wire.Reader, id pdu.CommandID) pdu.SMFields {
	b := pdu.SMFields{ServiceType: r.C(6, "service_type")}
	b.Source = c.readAddr(r, c.addressLimit(id), "source_addr")
	b.Destination = c.readAddr(r, c.addressLimit(id), "destination_addr")
	c.readSMTail(r, &b, id)
	return b
}

// readSMTail decodes the shared fields following the destination list/address.
func (c *Codec) readSMTail(r *wire.Reader, b *pdu.SMFields, id pdu.CommandID) {
	b.ESMClass = r.U8("esm_class")
	b.ProtocolID = r.U8("protocol_id")
	b.PriorityFlag = r.U8("priority_flag")
	b.ScheduleDeliveryTime = r.C(17, "schedule_delivery_time")
	b.ValidityPeriod = r.C(17, "validity_period")
	b.RegisteredDelivery = r.U8("registered_delivery")
	b.ReplaceIfPresent = r.U8("replace_if_present_flag")
	b.DataCoding = r.U8("data_coding")
	b.DefaultMessageID = r.U8("sm_default_msg_id")
	n := r.U8("sm_length")
	b.ShortMessage = r.Bytes(int(n), "short_message")
	b.Optional = c.readTLVs(r, id)
	if e := c.validateSM(*b, id); e != nil && r.Err == nil {
		r.Err = e
	}
}
func (c *Codec) writeSM(w *wire.Writer, b pdu.SMFields, id pdu.CommandID) {
	if e := c.validateSM(b, id); e != nil {
		w.Err = e
		return
	}
	w.C(b.ServiceType, 6, "service_type")
	c.writeAddr(w, b.Source, c.addressLimit(id), "source_addr")
	c.writeAddr(w, b.Destination, c.addressLimit(id), "destination_addr")
	c.writeSMTail(w, b, id)
}

func (c *Codec) writeSMTail(w *wire.Writer, b pdu.SMFields, id pdu.CommandID) {
	w.U8(b.ESMClass)
	w.U8(b.ProtocolID)
	w.U8(b.PriorityFlag)
	w.C(b.ScheduleDeliveryTime, 17, "schedule_delivery_time")
	w.C(b.ValidityPeriod, 17, "validity_period")
	w.U8(b.RegisteredDelivery)
	w.U8(b.ReplaceIfPresent)
	w.U8(b.DataCoding)
	w.U8(b.DefaultMessageID)
	w.U8(byte(len(b.ShortMessage)))
	w.Bytes(b.ShortMessage)
	c.writeTLVs(w, b.Optional, id)
}
func (c *Codec) validateSM(b pdu.SMFields, id pdu.CommandID) error {
	max := c.shortMessageLimit()
	if e := c.validateMessageFlags(id, b.ESMClass, b.RegisteredDelivery, b.DataCoding); e != nil {
		return e
	}
	if b.PriorityFlag > 3 || c.version == pdu.V33 && b.PriorityFlag > 1 {
		return invalidField("priority_flag", RuleValue)
	}
	if b.ReplaceIfPresent > 1 {
		return invalidField("replace_if_present_flag", RuleValue)
	}
	if !c.validDefaultMessageID(b.DefaultMessageID) {
		return invalidField("sm_default_msg_id", RuleValue)
	}
	if !c.validTime(b.ScheduleDeliveryTime, false) {
		return invalidField("schedule_delivery_time", RuleTime)
	}
	if !c.validTime(b.ValidityPeriod, false) {
		return invalidField("validity_period", RuleTime)
	}
	if len(b.ShortMessage) > max {
		return invalidField("short_message", RuleLength)
	}
	if _, ok := b.Optional.First(tlv.MessagePayload); ok && len(b.ShortMessage) != 0 {
		return invalidField("short_message", RuleConflict)
	}
	if err := validateUDHI(b.ESMClass, b.Optional); err != nil {
		return err
	}
	if c.version == pdu.V34 && id == pdu.DeliverSMID {
		if b.ScheduleDeliveryTime != "" {
			return invalidField("schedule_delivery_time", RuleUnused)
		}
		if b.ValidityPeriod != "" {
			return invalidField("validity_period", RuleUnused)
		}
		if b.ReplaceIfPresent != 0 {
			return invalidField("replace_if_present_flag", RuleUnused)
		}
		if b.DefaultMessageID != 0 {
			return invalidField("sm_default_msg_id", RuleUnused)
		}
	}

	return nil
}

// SMPP 3.4 section 5.2.12 forbids SAR and port TLVs alongside encoded UDH.
func validateUDHI(esm byte, fields tlv.Fields) error {
	if esm&0x40 == 0 {
		return nil
	}
	for i, f := range fields {
		switch f.Tag {
		case tlv.SARMsgRefNum, tlv.SARTotalSegments, tlv.SARSegmentSeqnum, tlv.SourcePort, tlv.DestinationPort:
			return invalidTLV(f.Tag, i, RuleConflict)
		}
	}
	return nil
}

// The 5.0 octet-count field permits 255; 3.4 reserves that value.
func (c *Codec) shortMessageLimit() int {
	switch c.version {
	case pdu.V33:
		return 160
	case pdu.V34:
		return 254
	default:
		return 255
	}
}
