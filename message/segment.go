// SPDX-AI-Disclosure: ai-generated
// SPDX-AI-Model: GPT-6.0 Astra
// SPDX-AI-Provider: OpenAI
// SPDX-AI-Scope: Library code generated and revised with Codex under maintainer direction.
// SPDX-AI-Date: 2026-10-28

package message

import (
	"crypto/sha256"
	"encoding/binary"
	"hash"

	"github.com/skill215/smppkit/pdu"
	"github.com/skill215/smppkit/tlv"
)

// Concatenation records wire identity and one-based segment numbering.
// Total is a declared count, not a guarantee all parts will arrive.
type Concatenation struct {
	Mode          Mode
	Reference     uint16
	Total, Number uint8
}

// Segment owns both original fields and extracted metadata. Receipt payloads
// remain raw and bypass concatenation; no receipt dialect is inferred.
type Segment struct {
	Fields  pdu.SMFields // owned per-segment provenance, including ordered TLVs
	Payload []byte       // user data with UDH removed
	Concat  *Concatenation
	UDH     []byte // non-concatenation elements, in original order
	Ports   []byte // identity bytes: UDH port IE, or ordered port TLV tag/value pairs
	Receipt bool
}

// ParseSegment classifies receipts before concatenation, rejects incomplete SAR,
// mixed UDH/SAR and malformed UDH, and retains unknown UDH elements and TLVs.
// It copies the input, but is not a versioned SMPP validator. Standalone callers
// must use Codec for command/field/TLV validation before admission.
func ParseSegment(fields pdu.SMFields) (Segment, error) {
	var s Segment
	if len(fields.Source.Value) > 64 || len(fields.Destination.Value) > 64 || len(fields.ServiceType) > 5 || len(fields.ShortMessage) > 65535 {
		return s, ErrMetadata
	}
	payload, hasPayload := fields.Optional.First(tlv.MessagePayload)
	if hasPayload && len(fields.ShortMessage) != 0 {
		return s, ErrMetadata
	}
	if !hasPayload {
		payload = fields.ShortMessage
	}
	bytesCount := len(payload)
	seen := map[uint16]bool{}
	for _, f := range fields.Optional {
		if len(f.Value) > 65535 {
			return s, ErrMetadata
		}
		bytesCount += 4 + len(f.Value)
		if bytesCount > 256<<10 {
			return s, ErrCapacity
		}
		switch f.Tag {
		case tlv.SourcePort, tlv.DestinationPort, tlv.MessagePayload, tlv.SARMsgRefNum, tlv.SARTotalSegments, tlv.SARSegmentSeqnum, tlv.ReceiptedMessageID, tlv.MessageState:
			if seen[f.Tag] {
				return s, ErrMetadata
			}
			seen[f.Tag] = true
		}
	}
	// A canonical tag order and explicit presence distinguish absent and zero
	// ports and prevent different applications sharing a SAR reference from mixing.
	for _, tag := range []uint16{tlv.SourcePort, tlv.DestinationPort} {
		if port, ok := fields.Optional.First(tag); ok {
			if len(port) != 2 || fields.ESMClass&0x40 != 0 {
				return s, ErrMetadata
			}
			s.Ports = append(s.Ports, byte(tag>>8), byte(tag), port[0], port[1])
		}
	}
	s.Fields = fields.Clone()
	s.Payload = append([]byte(nil), payload...)
	kind := fields.ESMClass & 0x3c
	s.Receipt = kind == 4 || kind == 0x20 || seen[tlv.ReceiptedMessageID]
	if s.Receipt {
		return s, nil
	}
	ref, hr := fields.Optional.First(tlv.SARMsgRefNum)
	total, ht := fields.Optional.First(tlv.SARTotalSegments)
	part, hp := fields.Optional.First(tlv.SARSegmentSeqnum)
	if hr || ht || hp {
		if !hr || !ht || !hp || len(ref) != 2 || len(total) != 1 || len(part) != 1 || total[0] == 0 || part[0] == 0 || part[0] > total[0] || fields.ESMClass&0x40 != 0 {
			return s, ErrMetadata
		}
		s.Concat = &Concatenation{SAR, binary.BigEndian.Uint16(ref), total[0], part[0]}
	}
	if fields.ESMClass&0x40 != 0 {
		if len(payload) == 0 || int(payload[0])+1 > len(payload) {
			return s, ErrMetadata
		}
		end := int(payload[0]) + 1
		s.Payload = append([]byte(nil), payload[end:]...)
		for i := 1; i < end; {
			if i+2 > end {
				return s, ErrMetadata
			}
			id, n := payload[i], int(payload[i+1])
			if i+2+n > end {
				return s, ErrMetadata
			}
			value := payload[i+2 : i+2+n]
			switch id {
			case 0:
				if n != 3 || s.Concat != nil {
					return s, ErrMetadata
				}
				s.Concat = &Concatenation{UDH8, uint16(value[0]), value[1], value[2]}
			case 8:
				if n != 4 || s.Concat != nil {
					return s, ErrMetadata
				}
				s.Concat = &Concatenation{UDH16, binary.BigEndian.Uint16(value), value[2], value[3]}
			default:
				s.UDH = append(s.UDH, payload[i:i+2+n]...)
				if id == 4 || id == 5 {
					if id == 4 && n != 2 || id == 5 && n != 4 {
						return s, ErrMetadata
					}
					if len(s.Ports) > 0 {
						return s, ErrMetadata
					}
					s.Ports = append(s.Ports, payload[i:i+2+n]...)
				}
			}
			i += 2 + n
		}
	}
	if s.Concat != nil && (s.Concat.Total == 0 || s.Concat.Number == 0 || s.Concat.Number > s.Concat.Total) {
		return s, ErrMetadata
	}
	return s, nil
}
func hashBytes(h hash.Hash, b []byte) {
	var length [4]byte
	binary.BigEndian.PutUint32(length[:], uint32(len(b)))
	h.Write(length[:])
	h.Write(b)
}

// signature intentionally includes ordered unknown TLVs and provenance. A
// repeated segment with changed metadata is a conflict, not a harmless replay.
func signature(s Segment) [32]byte {
	h := sha256.New()
	b := s.Fields
	hashBytes(h, []byte(b.ServiceType))
	h.Write([]byte{b.ESMClass, b.ProtocolID, b.PriorityFlag, b.RegisteredDelivery, b.ReplaceIfPresent, b.DataCoding, b.DefaultMessageID})
	hashBytes(h, []byte(b.ScheduleDeliveryTime))
	hashBytes(h, []byte(b.ValidityPeriod))
	hashBytes(h, s.Payload)
	hashBytes(h, s.UDH)
	if s.Concat != nil {
		h.Write([]byte{s.Concat.Total, s.Concat.Number})
	}
	for _, f := range b.Optional {
		h.Write([]byte{byte(f.Tag >> 8), byte(f.Tag)})
		hashBytes(h, f.Value)
	}
	var out [32]byte
	copy(out[:], h.Sum(nil))
	return out
}
func segmentBytes(s Segment) int {
	n := len(s.Fields.ShortMessage) + len(s.Payload) + len(s.UDH) + len(s.Ports) + len(s.Fields.Source.Value) + len(s.Fields.Destination.Value) + len(s.Fields.ServiceType) + len(s.Fields.ScheduleDeliveryTime) + len(s.Fields.ValidityPeriod) + 256
	for _, f := range s.Fields.Optional {
		n += len(f.Value) + 32
	}
	return n
}
