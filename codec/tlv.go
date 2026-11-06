// SPDX-AI-Disclosure: ai-generated
// SPDX-AI-Model: GPT-6.0 Astra
// SPDX-AI-Provider: OpenAI
// SPDX-AI-Scope: Library code generated and revised with Codex under maintainer direction.
// SPDX-AI-Date: 2026-11-06

package codec

import (
	"encoding/binary"
	"errors"
	"slices"

	"github.com/skill215/smppkit/internal/wire"
	"github.com/skill215/smppkit/pdu"
	"github.com/skill215/smppkit/tlv"
)

func (c *Codec) readTLVs(r *wire.Reader, id pdu.CommandID) tlv.Fields {
	if r.Err != nil {
		return nil
	}
	// Mark errors at the optional-section boundary, never by parsing text.
	// A prior mandatory-field failure returned above is not reclassified.
	defer func() {
		if r.Err != nil {
			r.Err = &categorizedError{DecodeTLV, r.Err}
		}
	}()
	var f tlv.Fields
	for r.Pos < len(r.Data) && r.Err == nil {
		h := r.Bytes(4, "tlv.header")
		if h == nil {
			break
		}
		tag := binary.BigEndian.Uint16(h)
		n := int(binary.BigEndian.Uint16(h[2:]))
		v := r.Bytes(n, "tlv.value")
		if r.Err != nil {
			break
		}
		f = append(f, tlv.Field{Tag: tag, Value: v})
	}
	if r.Err == nil {
		r.Err = c.validateTLVs(f, id)
	}
	return f
}
func (c *Codec) writeTLVs(w *wire.Writer, f tlv.Fields, id pdu.CommandID) {
	if w.Err != nil {
		return
	}
	// Bound the optional section before validation allocates a seen-tag map
	// or invokes application callbacks. Subtraction avoids length overflow.
	remaining := w.Limit - len(w.Data)
	for _, field := range f {
		if remaining < 4 || len(field.Value) > remaining-4 {
			w.Err = wire.ErrLimit
			return
		}
		remaining -= 4 + len(field.Value)
	}
	if e := c.validateTLVs(f, id); e != nil {
		w.Err = e
		return
	}
	for _, x := range f {
		var h [4]byte
		binary.BigEndian.PutUint16(h[:], x.Tag)
		binary.BigEndian.PutUint16(h[2:], uint16(len(x.Value)))
		w.Bytes(h[:])
		w.Bytes(x.Value)
		if w.Err != nil {
			return
		}
	}
}
func (c *Codec) validateTLVs(f tlv.Fields, id pdu.CommandID) error {
	if len(f) == 0 {
		return nil
	}
	if c.version == pdu.V33 {
		return invalidField("optional", RuleVersion)
	}

	command := tlvCommand(id)
	if command == 0 || c.version == pdu.V34 && (command == submitResponseTLV || command == multiResponseTLV || command == deliverResponseTLV || command == otherResponseTLV) {
		return invalidField("optional", RuleCommand)
	}
	counts := make(map[uint16]int)
	for i, field := range f {
		if len(field.Value) > 65535 {
			return invalidTLV(field.Tag, i, RuleLength)
		}
		counts[field.Tag]++
		if def, ok := standardTLVs[field.Tag]; ok {
			allowed, min, max := def.v34, def.min, def.max
			if c.version == pdu.V50 {
				allowed = def.v50
				switch field.Tag {
				case tlv.AlertOnMessageDelivery:
					max = 1
				case tlv.MSValidity:
					max = 4
				}
			}
			if allowed == 0 {
				return invalidTLV(field.Tag, i, RuleVersion)
			}
			if allowed&command == 0 {
				return invalidTLV(field.Tag, i, RuleCommand)
			}
			if len(field.Value) < min || len(field.Value) > max {
				return invalidTLV(field.Tag, i, RuleLength)
			}
			if !def.repeat && counts[field.Tag] > 1 {
				return invalidTLV(field.Tag, i, RuleCardinality)
			}

			if !validStandardValue(field.Tag, field.Value, c.version) {
				return invalidTLV(field.Tag, i, RuleValue)
			}
		} else if field.Tag >= 0x0600 && field.Tag <= 0x060a {
			// Broadcast has its own future command scope; it is never an opaque way
			// to inject broadcast-only parameters into an SMS operation.
			return invalidTLV(field.Tag, i, RuleCommand)
		} else if def, ok := c.tlvs[field.Tag]; ok {
			if !slices.Contains(def.Versions, c.version) {
				return invalidTLV(field.Tag, i, RuleVersion)
			}
			if len(field.Value) < def.MinLength || len(field.Value) > def.MaxLength {
				return invalidTLV(field.Tag, i, RuleLength)
			}
			if !slices.Contains(def.Commands, id) {
				return invalidTLV(field.Tag, i, RuleCommand)
			}
			if !def.Repeatable && counts[field.Tag] > 1 {
				return invalidTLV(field.Tag, i, RuleCardinality)
			}

			// A validator may rely on its declared width, but must remain concurrency
			// safe and must not retain or mutate these borrowed bytes.
			if def.Validate != nil {
				if cause := def.Validate(field.Value); cause != nil {
					e := invalidTLV(field.Tag, i, RuleExtension)
					// Preserve validation identity and the original callback cause without
					// ever including its potentially sensitive text in the safe summary.
					e.cause = errors.Join(ErrValidation, cause)
					return e
				}
			}
		}
		// Unknown tags are preserved, including duplicates, without invented rules.
	}
	return validateTLVDependencies(f, counts)
}

func tlvCommand(id pdu.CommandID) tlvCommands {
	switch id {
	case pdu.SubmitSMID:
		return submitTLV
	case pdu.SubmitMultiID:
		return multiTLV
	case pdu.DeliverSMID:
		return deliverTLV
	case pdu.DataSMID:
		return dataTLV
	case pdu.SubmitSMRespID:
		return submitResponseTLV
	case pdu.SubmitMultiRespID:
		return multiResponseTLV
	case pdu.DeliverSMRespID:
		return deliverResponseTLV
	case pdu.DataSMRespID:
		return dataResponseTLV
	case pdu.BindTransmitterRespID, pdu.BindReceiverRespID, pdu.BindTransceiverRespID:
		return bindResponseTLV
	case pdu.AlertNotificationID:
		return alertTLV
	case pdu.QuerySMRespID, pdu.ReplaceSMRespID, pdu.CancelSMRespID, pdu.UnbindRespID, pdu.EnquireLinkRespID, pdu.GenericNACKID:
		return otherResponseTLV
	}
	return 0
}

// Dependency checks run only after every known value passed its width check.
// Callback associations are occurrence-based; sorting/coalescing would lose them.
func validateTLVDependencies(f tlv.Fields, counts map[uint16]int) error {
	if counts[tlv.SARMsgRefNum]+counts[tlv.SARTotalSegments]+counts[tlv.SARSegmentSeqnum] > 0 {
		total, ht := f.First(tlv.SARTotalSegments)
		part, hp := f.First(tlv.SARSegmentSeqnum)
		if counts[tlv.SARMsgRefNum] != 1 || !ht || !hp || total[0] == 0 || part[0] == 0 || part[0] > total[0] {
			return sarDependency(f, counts, ht, hp)
		}
	}
	for _, tag := range []uint16{tlv.CallbackNumATag, tlv.CallbackNumPresInd} {
		if counts[tag] > 0 && counts[tag] != counts[tlv.CallbackNum] {
			return tlvDependency(f, tag)
		}
	}
	for _, pair := range [][2]uint16{{tlv.SourceNetworkID, tlv.SourceNodeID}, {tlv.DestNetworkID, tlv.DestNodeID}} {
		if counts[pair[0]] != 0 && counts[pair[1]] == 0 {
			return tlvDependency(f, pair[0])
		}
	}
	if v, ok := f.First(tlv.DestAddrNPResolution); ok && v[0] == 2 && (counts[tlv.DestAddrNPInformation] == 0 || counts[tlv.DestAddrNPCountry] == 0) {
		return tlvDependency(f, tlv.DestAddrNPResolution)
	}
	return nil
}

// Dependencies name a stable trigger; missing SAR members name the absent tag.
// Index is into the original ordered Optional slice, not the per-tag count.
func tlvDependency(f tlv.Fields, tag uint16) *SemanticError {
	for i, field := range f {
		if field.Tag == tag {
			return invalidTLV(tag, i, RuleDependency)
		}
	}
	return invalidTLV(tag, -1, RuleDependency)
}

func sarDependency(f tlv.Fields, counts map[uint16]int, total, part bool) *SemanticError {
	if counts[tlv.SARMsgRefNum] != 1 {
		return tlvDependency(f, tlv.SARMsgRefNum)
	}
	if !total {
		return tlvDependency(f, tlv.SARTotalSegments)
	}
	if !part {
		return tlvDependency(f, tlv.SARSegmentSeqnum)
	}
	if v, _ := f.First(tlv.SARTotalSegments); v[0] == 0 {
		return tlvDependency(f, tlv.SARTotalSegments)
	}
	return tlvDependency(f, tlv.SARSegmentSeqnum)
}
