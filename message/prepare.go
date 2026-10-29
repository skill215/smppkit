// SPDX-AI-Disclosure: ai-generated
// SPDX-AI-Model: GPT-6.0 Astra
// SPDX-AI-Provider: OpenAI
// SPDX-AI-Scope: Library code generated and revised with Codex under maintainer direction.
// SPDX-AI-Date: 2026-10-29

// Package message prepares text, assembles received segments, and optionally
// parses or constructs receipt evidence without owning a transport. Submission
// acceptance, later receipt content and receipt ACKs are distinct outcomes.
package message

import (
	"errors"
	"fmt"

	"github.com/skill215/smppkit/pdu"
	"github.com/skill215/smppkit/textcodec"
	"github.com/skill215/smppkit/tlv"
)

var (
	ErrCapacity = errors.New("smpp: message resource capacity exceeded")
	ErrMetadata = errors.New("smpp: invalid or conflicting message metadata")
	ErrConflict = errors.New("smpp: conflicting segment")
)

// Mode chooses how message boundaries are represented to the peer.
type Mode uint8

const (
	UDH16   Mode = iota + 1 // 16-bit concatenation reference in user-data headers
	UDH8                    // smaller reference space, one fewer header octet
	SAR                     // segmentation metadata in TLVs, no encoded concatenation UDH
	Payload                 // one message_payload request; peer owns any further segmentation
)

// InformationElement is an additional UDH element without IE length or UDHL.
// Prepare supplies lengths, rejects caller-supplied concatenation elements and
// validates application ports. National-language declarations (0x24/0x25) are
// rejected with ErrUnsupportedNationalLanguage; other contents remain opaque.
type InformationElement struct {
	ID   uint8
	Data []byte
}

// Text is a logical message before encoding and segmentation. Optional values
// are cloned into each segment; generated SAR/payload tags must not be supplied.
type Text struct {
	Source, Destination pdu.Address
	Text                string
	ServiceType         string
	RegisteredDelivery  uint8
	Optional            tlv.Fields
}

// Policy is explicit peer policy. PayloadOctets describes radio user data;
// GSM7 is sent as unpacked septets. Packed septets are not supported here.
// SAR conservatively budgets a 16-bit concatenation UDH at the SMSC and,
// when port TLVs are supplied, a 16-bit application-port element. It emits
// only TLVs; the peer must support that mapping to radio user data.
// Custom encoders must satisfy textcodec.Encoding's unit/boundary contract.
// Their DCS mapping must also match the selected protocol version and peer.
type Policy struct {
	Encoding        textcodec.Encoding
	Fallback        textcodec.Encoding // nil rejects unrepresentable text
	Mode            Mode
	PayloadOctets   int // default 140, 1..140
	AdditionalUDH   []InformationElement
	MaxInputBytes   int // default 64 KiB
	MaxEncodedBytes int // default 256 KiB
	MaxSegments     int // default 255
}

// GSMText is an explicit unpacked GSM default-alphabet policy with UCS-2
// fallback, 16-bit UDH, and 140-octet radio budget. Peers must support it.
func GSMText() Policy { return Policy{Encoding: textcodec.GSM7, Fallback: textcodec.UCS2, Mode: UDH16} }
func (p Policy) normalized() (Policy, error) {
	if p.Encoding == nil {
		return p, errors.New("smpp: explicit text encoding required")
	}
	if p.Mode == 0 {
		p.Mode = UDH16
	}
	if p.PayloadOctets == 0 {
		p.PayloadOctets = 140
	}
	if p.MaxInputBytes == 0 {
		p.MaxInputBytes = 64 << 10
	}
	if p.MaxEncodedBytes == 0 {
		p.MaxEncodedBytes = 256 << 10
	}
	if p.MaxSegments == 0 {
		p.MaxSegments = 255
	}
	if p.Mode < UDH16 || p.Mode > Payload || p.PayloadOctets < 1 || p.PayloadOctets > 140 || p.MaxInputBytes < 1 || p.MaxInputBytes > 16<<20 || p.MaxEncodedBytes < 1 || p.MaxEncodedBytes > 16<<20 || p.MaxSegments < 1 || p.MaxSegments > 255 {
		return p, ErrCapacity
	}
	for _, e := range []textcodec.Encoding{p.Encoding, p.Fallback} {
		if e != nil && e.UnitBits() != 7 && e.UnitBits() != 8 {
			return p, errors.New("smpp: encoder unit width must be 7 or 8")
		}
	}
	if (p.Mode == SAR || p.Mode == Payload) && len(p.AdditionalUDH) != 0 {
		return p, ErrMetadata
	}
	return p, nil
}

// Plan is immutable. Preview and Materialize return owned copies. It contains
// no leased reference; each execution must reserve a new one.
type Plan struct {
	mode       Mode
	encoding   string
	template   pdu.SMFields
	chunks     [][]byte
	additional []byte
	multipart  bool
}

// Encoding reports the selected encoding name, including any fallback.
func (p *Plan) Encoding() string { return p.encoding }

// DataCoding reports the frozen DCS used by Materialize.
func (p *Plan) DataCoding() uint8 { return p.template.DataCoding }

// Mode reports the chosen policy, even when the text fits one segment.
func (p *Plan) Mode() Mode { return p.mode }

// Count is the number of requests; an empty message still produces one.
func (p *Plan) Count() int { return len(p.chunks) }

// Multipart reports whether Materialize will add concatenation metadata.
func (p *Plan) Multipart() bool { return p.multipart }

// ReferenceBits is the allocator width needed when Multipart is true.
func (p *Plan) ReferenceBits() int {
	if p.mode == UDH8 {
		return 8
	}
	return 16
}

// Preview returns owned encoded chunks without UDH or optional parameters.
// Use Materialize for complete per-segment fields and final Codec validation.
func (p *Plan) Preview() [][]byte {
	out := make([][]byte, len(p.chunks))
	for i, b := range p.chunks {
		out[i] = append([]byte(nil), b...)
	}
	return out
}

// budget accounts for radio octets even though GSM is carried as unpacked
// septets in SMPP. Round header usage up to whole septets to include fill bits.
func budget(octets, header, unitBits int) int {
	if unitBits == 7 {
		return octets*8/7 - (header*8+6)/7
	}
	return octets - header
}

// Prepare encodes all text before segmenting at safe character boundaries.
// It performs no I/O or reference reservation. Codec-specific field/TLV rules
// must still be checked on Materialize output before transmission.
// The selected built-in Latin1 encoding is rejected for 3.3, including after
// fallback: its DCS=3 assignment belongs to 3.4/5.0, not the GSM DCS used by 3.3.
// Fallback handles text encoding errors, not an incompatible protocol policy.
// UDH national-language declarations are rejected before invoking either encoder,
// since Encoding has no header-aware contract to verify the selected table.
func Prepare(version pdu.Version, msg Text, policy Policy) (*Plan, error) {
	if !version.Valid() {
		return nil, ErrMetadata
	}
	policy, e := policy.normalized()
	if e != nil {
		return nil, e
	}
	additional, e := prepareUDH(policy.AdditionalUDH)
	if e != nil {
		return nil, e
	}
	if len(msg.Text) > policy.MaxInputBytes {
		return nil, ErrCapacity
	}
	portElements := 0
	seenPorts := map[uint16]bool{}
	for _, x := range msg.Optional {
		switch x.Tag {
		case tlv.SARMsgRefNum, tlv.SARTotalSegments, tlv.SARSegmentSeqnum, tlv.MessagePayload:
			return nil, ErrMetadata
		case tlv.SourcePort, tlv.DestinationPort:
			if len(x.Value) != 2 || seenPorts[x.Tag] {
				return nil, ErrMetadata
			}
			seenPorts[x.Tag] = true
			portElements = 6
		}
	}
	if version == pdu.V33 && (len(msg.Optional) > 0 || policy.Mode == SAR || policy.Mode == Payload) {
		return nil, ErrMetadata
	}
	enc := policy.Encoding
	encoded, e := enc.Encode(msg.Text)
	if e != nil && policy.Fallback != nil {
		enc = policy.Fallback
		encoded, e = enc.Encode(msg.Text)
	}
	if e != nil {
		return nil, e
	}
	if e = validateEncodingVersion(version, enc); e != nil {
		return nil, e
	}
	if e = textcodec.Validate(encoded); e != nil {
		return nil, e
	}
	if len(encoded.Bytes) > policy.MaxEncodedBytes {
		return nil, ErrCapacity
	}
	p := &Plan{
		mode: policy.Mode, encoding: enc.Name(), additional: additional,
		template: pdu.SMFields{
			ServiceType: msg.ServiceType, Source: msg.Source, Destination: msg.Destination,
			RegisteredDelivery: msg.RegisteredDelivery, DataCoding: enc.DataCoding(),
			Optional: msg.Optional.Clone(),
		},
	}
	if portElements > 0 && len(p.additional) > 0 {
		return nil, ErrMetadata
	}
	if p.mode == Payload {
		if len(encoded.Bytes) > 65535 {
			return nil, ErrCapacity
		}
		p.chunks = [][]byte{append([]byte(nil), encoded.Bytes...)}
		return p, nil
	}
	header := 0
	if len(p.additional) > 0 {
		header = len(p.additional) + 1
	}
	if portElements > 0 {
		header = 1 + portElements
	}
	unitBits := enc.UnitBits()
	limit := budget(policy.PayloadOctets, header, unitBits)
	if len(encoded.Bytes) <= limit {
		p.chunks = [][]byte{append([]byte(nil), encoded.Bytes...)}
		if version == pdu.V33 && header != 0 {
			return nil, fmt.Errorf("%w: 3.3 submit_sm UDHI unavailable", ErrMetadata)
		}
		return p, nil
	}
	if version == pdu.V33 {
		return nil, fmt.Errorf("%w: 3.3 standard submit_sm does not define UDHI; use a peer-specific low-level policy", ErrMetadata)
	}
	if portElements > 0 && p.mode != SAR {
		return nil, ErrMetadata
	}
	p.multipart = true
	switch p.mode {
	case UDH16:
		header = len(p.additional) + 7
	case UDH8:
		header = len(p.additional) + 6
	case SAR:
		header = 7 + portElements
	}
	limit = budget(policy.PayloadOctets, header, unitBits)
	if limit < 1 {
		return nil, ErrCapacity
	}
	// Boundaries keep a GSM escape pair or UTF-16 surrogate pair indivisible.
	// Never split raw bytes merely because the next byte fits the radio budget.
	start, last := 0, 0
	for _, end := range encoded.Boundaries {
		if end-start > limit {
			if last == start {
				return nil, ErrCapacity
			}
			p.chunks = append(p.chunks, append([]byte(nil), encoded.Bytes[start:last]...))
			start = last
			if end-start > limit {
				return nil, ErrCapacity
			}
		}
		last = end
	}
	if last > start {
		p.chunks = append(p.chunks, append([]byte(nil), encoded.Bytes[start:last]...))
	}
	if len(p.chunks) > policy.MaxSegments {
		return nil, ErrCapacity
	}
	return p, nil
}

// Only built-in mappings are inferred. A custom Encoding is trusted peer
// policy, like its alphabet and boundary implementation; do not reject its raw
// DCS or compare arbitrary (possibly non-comparable) interface values.
func validateEncodingVersion(version pdu.Version, enc textcodec.Encoding) error {
	if version != pdu.V33 {
		return nil
	}
	var alphabet textcodec.Alphabet
	switch e := enc.(type) {
	case textcodec.Alphabet:
		alphabet = e
	case *textcodec.Alphabet:
		alphabet = *e // selected Encode already succeeded
	}
	if alphabet == textcodec.Latin1 {
		return fmt.Errorf("%w: built-in Latin1 DCS is unavailable in SMPP 3.3; select an explicit peer encoding", ErrMetadata)
	}
	return nil
}

// Materialize returns independent per-segment fields using ref for all parts.
// It does not reserve ref or record a send. Reusing a Plan for another message
// requires a fresh reference lease when Multipart is true.
func (p *Plan) Materialize(ref uint16) ([]pdu.SMFields, error) {
	if p.mode == UDH8 && ref > 255 {
		return nil, ErrMetadata
	}
	out := make([]pdu.SMFields, len(p.chunks))
	for i, chunk := range p.chunks {
		b := p.template.Clone()
		var h []byte
		if p.multipart {
			switch p.mode {
			case UDH16:
				h = []byte{8, 4, byte(ref >> 8), byte(ref), byte(len(p.chunks)), byte(i + 1)}
			case UDH8:
				h = []byte{0, 3, byte(ref), byte(len(p.chunks)), byte(i + 1)}
			case SAR:
				b.Optional = append(b.Optional, tlv.Uint16(tlv.SARMsgRefNum, ref), tlv.Field{Tag: tlv.SARTotalSegments, Value: []byte{byte(len(p.chunks))}}, tlv.Field{Tag: tlv.SARSegmentSeqnum, Value: []byte{byte(i + 1)}})
			}
		}
		h = append(h, p.additional...)
		if len(h) > 0 {
			if len(h) > 255 {
				return nil, ErrMetadata
			}
			b.ESMClass |= 0x40
			b.ShortMessage = append([]byte{byte(len(h))}, h...)
		}
		if p.mode == Payload {
			b.Optional = append(b.Optional, tlv.Field{Tag: tlv.MessagePayload, Value: append([]byte(nil), chunk...)})
		} else {
			b.ShortMessage = append(b.ShortMessage, chunk...)
		}
		out[i] = b
	}
	return out, nil
}

// prepareUDH validates known port elements and reserves room for the largest
// concatenation IE before copying any bytes. Unknown IEs remain opaque. A port
// pair has one representation; mixing 8-bit and 16-bit elements is ambiguous.
func prepareUDH(elements []InformationElement) ([]byte, error) {
	total := 0
	ports := false
	for _, ie := range elements {
		if nationalLanguageIE(ie.ID) {
			return nil, ErrUnsupportedNationalLanguage
		}
		if ie.ID == 0 || ie.ID == 8 || len(ie.Data) > 255 {
			return nil, ErrMetadata
		}
		if ie.ID == 4 || ie.ID == 5 {
			if ports || ie.ID == 4 && len(ie.Data) != 2 || ie.ID == 5 && len(ie.Data) != 4 {
				return nil, ErrMetadata
			}
			ports = true
		}
		// Existing policy caps extra IEs at 248 bytes (plus a 6-byte concat IE
		// and the UDHL octet). Check by subtraction to avoid integer overflow.
		if len(ie.Data)+2 > 248-total {
			return nil, ErrMetadata
		}
		total += len(ie.Data) + 2
	}
	if total == 0 {
		return nil, nil
	}
	out := make([]byte, 0, total)
	for _, ie := range elements {
		out = append(out, ie.ID, byte(len(ie.Data)))
		out = append(out, ie.Data...)
	}
	return out, nil
}
