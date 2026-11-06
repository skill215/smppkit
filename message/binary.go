// SPDX-AI-Disclosure: ai-generated
// SPDX-AI-Model: GPT-6.0 Astra
// SPDX-AI-Provider: OpenAI
// SPDX-AI-Scope: Library code generated and revised with Codex under maintainer direction.
// SPDX-AI-Date: 2026-11-06

package message

import (
	"github.com/skill215/smppkit/pdu"
	"github.com/skill215/smppkit/tlv"
)

// Binary is a logical message of uninterpreted octets, including NUL and invalid
// UTF-8. DataCoding is required: 0x04 in 3.3, 0x02 or 0x04 in 3.4/5.0. Zero is
// not a default. Other DCS policies remain available through concrete PDUs.
// Optional values are copied into each part; do not supply generated SAR/payload
// tags. Callers must not mutate Data, Optional or policy UDH during preparation.
type Binary struct {
	Source, Destination pdu.Address
	Data                []byte
	DataCoding          uint8
	ServiceType         string
	RegisteredDelivery  uint8
	Optional            tlv.Fields
}

// BinaryPolicy controls octet segmentation without a text encoding or fallback.
// SAR budgets the same conservative radio overhead as Policy. AdditionalUDH is
// allowed only with UDH8/UDH16, rejects concat and language-table declarations,
// and remains opaque except for validated application ports.
//
// MaxOutputBytes bounds accounted storage for one Materialize result: 256 bytes
// per part, address/service string lengths per part, payload/header octets, and
// 32 bytes plus value length per TLV (including generated tags). It includes
// repeated metadata, not just Data. This is a conservative accounting budget,
// not an exact heap/RSS limit; allocator rounding, temporary buffers and the
// caller's retained outputs are outside it. Input/output limits may be 1..16 MiB.
type BinaryPolicy struct {
	Mode           Mode // default UDH16
	PayloadOctets  int  // default 140, range 1..140
	AdditionalUDH  []InformationElement
	MaxInputBytes  int // default 64 KiB
	MaxOutputBytes int // default 256 KiB
	MaxSegments    int // default 255, range 1..255
}

func (p BinaryPolicy) normalized() (BinaryPolicy, error) {
	if p.Mode == 0 {
		p.Mode = UDH16
	}
	if p.PayloadOctets == 0 {
		p.PayloadOctets = 140
	}
	if p.MaxInputBytes == 0 {
		p.MaxInputBytes = 64 << 10
	}
	if p.MaxOutputBytes == 0 {
		p.MaxOutputBytes = 256 << 10
	}
	if p.MaxSegments == 0 {
		p.MaxSegments = 255
	}
	if p.Mode < UDH16 || p.Mode > Payload || p.PayloadOctets < 1 || p.PayloadOctets > 140 || p.MaxInputBytes < 1 || p.MaxInputBytes > 16<<20 || p.MaxOutputBytes < 1 || p.MaxOutputBytes > 16<<20 || p.MaxSegments < 1 || p.MaxSegments > 255 {
		return p, ErrCapacity
	}
	if (p.Mode == SAR || p.Mode == Payload) && len(p.AdditionalUDH) != 0 {
		return p, ErrMetadata
	}
	return p, nil
}

// PrepareBytes snapshots bytes and metadata into an immutable Plan, without
// encoding, I/O or reference reservation. Every octet is a split boundary; this
// does not promise character-safe splitting for pre-encoded text. Empty input
// produces one request. Payload produces one message_payload request (at most
// 65535 octets), with radio segmentation delegated to the peer.
//
// 3.3 supports only single-part, TLV-free, header-free preparation. There is no
// silent mode or DCS fallback. Codec validation of materialized fields remains
// necessary; SendBinary preflights every part before leasing or transmitting.
func PrepareBytes(version pdu.Version, msg Binary, policy BinaryPolicy) (*Plan, error) {
	if !version.Valid() || msg.DataCoding != 4 && (version == pdu.V33 || msg.DataCoding != 2) {
		return nil, ErrMetadata
	}
	policy, err := policy.normalized()
	if err != nil {
		return nil, err
	}
	if len(msg.Data) > policy.MaxInputBytes {
		return nil, ErrCapacity
	}
	// Check the unmultiplied metadata budget before copying any optional values.
	// Subtraction keeps even hostile lengths/counts from overflowing the budget.
	base, err := binaryMetadataSize(msg, policy.MaxOutputBytes)
	if err != nil {
		return nil, err
	}
	ports, err := prepareOptional(version, policy.Mode, msg.Optional)
	if err != nil {
		return nil, err
	}
	additional, err := prepareUDH(policy.AdditionalUDH)
	if err != nil {
		return nil, err
	}
	if ports > 0 && len(additional) > 0 || version == pdu.V33 && len(additional) > 0 {
		return nil, ErrMetadata
	}
	count, limit, header, generated := 1, policy.PayloadOctets, 0, 0
	multipart := false
	if policy.Mode == Payload {
		if len(msg.Data) > 65535 {
			return nil, ErrCapacity
		}
		generated = 32 // message_payload value bytes are counted as Data below
	} else {
		if len(additional) > 0 {
			header = 1 + len(additional)
		}
		radioHeader := header
		if ports > 0 {
			radioHeader = 1 + ports
		}
		limit -= radioHeader
		// An empty message can exactly fill the budget with its header alone.
		if len(msg.Data) > limit {
			if version == pdu.V33 {
				return nil, ErrMetadata
			}
			if ports > 0 && policy.Mode != SAR {
				return nil, ErrMetadata
			}
			multipart = true
			switch policy.Mode {
			case UDH8:
				header = 6 + len(additional)
				radioHeader = header
			case UDH16:
				header = 7 + len(additional)
				radioHeader = header
			case SAR:
				radioHeader = 7 + ports
				generated = 3*32 + 4 // reference, total, sequence TLVs
			}
			limit = policy.PayloadOctets - radioHeader
			if limit < 1 {
				return nil, ErrCapacity
			}
			count = (len(msg.Data) + limit - 1) / limit
		}
	}
	if count > policy.MaxSegments || len(msg.Data) > policy.MaxOutputBytes || base+header+generated > (policy.MaxOutputBytes-len(msg.Data))/count {
		return nil, ErrCapacity
	}
	p := &Plan{
		mode: policy.Mode, encoding: "binary", additional: additional, multipart: multipart,
		template: pdu.SMFields{
			Source: msg.Source, Destination: msg.Destination, ServiceType: msg.ServiceType,
			RegisteredDelivery: msg.RegisteredDelivery, DataCoding: msg.DataCoding,
			Optional: msg.Optional.Clone(),
		},
		chunks: make([][]byte, count),
	}
	// One snapshot for all chunks; their full-slice capacities prevent accidental
	// append into a neighboring part. Materialize/Preview make independent copies.
	data := append([]byte(nil), msg.Data...)
	for i := range count {
		end := len(data)
		if multipart {
			end = min(end, limit)
		}
		p.chunks[i] = data[:end:end]
		data = data[end:]
	}
	return p, nil
}

func binaryMetadataSize(msg Binary, maxBytes int) (int, error) {
	remaining := maxBytes
	for _, n := range []int{256, len(msg.Source.Value), len(msg.Destination.Value), len(msg.ServiceType)} {
		if n > remaining {
			return 0, ErrCapacity
		}
		remaining -= n
	}
	for _, f := range msg.Optional {
		if len(f.Value) > 65535 {
			return 0, ErrMetadata
		}
		if 32+len(f.Value) > remaining {
			return 0, ErrCapacity
		}
		remaining -= 32 + len(f.Value)
	}
	return maxBytes - remaining, nil
}
