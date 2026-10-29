// SPDX-AI-Disclosure: ai-generated
// SPDX-AI-Model: GPT-6.0 Astra
// SPDX-AI-Provider: OpenAI
// SPDX-AI-Scope: Library code generated and revised with Codex under maintainer direction.
// SPDX-AI-Date: 2026-10-29

package message

import (
	"bytes"
	"encoding/binary"
	"errors"

	"github.com/skill215/smppkit/pdu"
	"github.com/skill215/smppkit/tlv"
)

// ErrNotReceipt means the body is not deliver_sm/data_sm with an explicit MC
// receipt or intermediate-notification type. Text or a receipt TLV alone never
// classifies an ordinary message as a receipt.
var ErrNotReceipt = errors.New("smpp: body is not an explicitly marked receipt")

// ReceiptKind preserves the esm_class message type, not a delivery decision.
// In 3.3, using marker 0x04 is an explicit legacy convention: the main 3.3
// specification delegates marker assignment to its Applications Guide.
type ReceiptKind uint8

const (
	MCReceipt                ReceiptKind = 0x04
	IntermediateNotification ReceiptKind = 0x20
)

// ReceiptDialect explicitly selects text interpretation. The key/value grammar
// is Appendix-B-inspired, not a universally required SMPP receipt format.
type ReceiptDialect uint8

const (
	ReceiptRawText  ReceiptDialect = iota // retain bytes without interpreting text
	ReceiptKeyValue                       // fixed labels, optional fields, terminal text; see receipts.md
)

// ReceiptParseOptions requires an explicit version. Limits are fixed: 64 KiB
// total raw evidence including TLV headers, 256 TLVs, 32 fields per text carrier.
// Dialect zero retains raw text while still extracting standard receipt TLVs.
type ReceiptParseOptions struct {
	Version pdu.Version
	Dialect ReceiptDialect
}

// ReceiptIssue is a diagnostic category. Diagnostics never contain input values.
type ReceiptIssue string

const (
	ReceiptMissing     ReceiptIssue = "missing"
	ReceiptDuplicate   ReceiptIssue = "duplicate"
	ReceiptMalformed   ReceiptIssue = "malformed"
	ReceiptUnsupported ReceiptIssue = "unsupported"
	ReceiptConflict    ReceiptIssue = "conflict"
)

// ReceiptDiagnostic identifies a field without including message content.
// Source is "pdu", "short_message", "message_payload" or "tlv". Index is the
// original optional-field index for a TLV/payload, otherwise -1. Field is a fixed
// field name chosen by the library, never an untrusted label copied from input.
type ReceiptDiagnostic struct {
	Issue  ReceiptIssue
	Source string
	Index  int
	Field  string
}

// ReceiptField preserves one recognized text field, including duplicate values.
// Name is one of id/sub/dlvrd/submit date/done date/stat/err/text. Values retain
// opaque IDs, zeros, status spelling and timestamps; no numeric/date conversion.
type ReceiptField struct{ Name, Value string }

// ReceiptText describes one text carrier. Raw owns the encoded payload. Fields
// contain decoded strings only for an explicitly selected, supported dialect.
// TLVIndex is -1 for short_message, otherwise its original optional-field index.
type ReceiptText struct {
	Source   string
	TLVIndex int
	Raw      []byte
	Fields   []ReceiptField
}

// Value returns a uniquely present field, including a present empty text field.
// Missing or duplicate fields return false; malformed/unsupported values remain
// observable and must be interpreted alongside Receipt.Diagnostics.
func (r ReceiptText) Value(name string) (value string, unique bool) {
	for _, f := range r.Fields {
		if f.Name == name {
			if unique {
				return "", false
			}
			value, unique = f.Value, true
		}
	}
	return
}

// ReceiptNetworkError is the standard three-octet network_error_code layout.
// Code is a two-octet network-specific number, not a status/error byte pair and
// not necessarily equivalent to a text err value. No business mapping is added.
type ReceiptNetworkError struct {
	Type uint8
	Code uint16
}

// ReceiptTLVs exposes only uniquely present, structurally valid values. Unknown
// numeric states/network types remain available with Unsupported diagnostics.
// Duplicates are never silently resolved; all original TLVs remain in Optional.
type ReceiptTLVs struct {
	MessageID    string
	HasMessageID bool
	State        uint8
	HasState     bool
	NetworkError *ReceiptNetworkError
}

// Receipt owns its raw evidence and parsed fields. There is no merged ID/state,
// precedence rule, finality flag, submission correlation or ACK policy. Texts
// contains short_message and, when unique, message_payload separately. Duplicate
// message_payload values remain in Optional and are not selected for parsing.
// Optional and Texts.Raw have independent backing storage, including from input.
type Receipt struct {
	Kind                 ReceiptKind
	Source, Destination  pdu.Address
	ESMClass, DataCoding uint8
	Texts                []ReceiptText
	Optional             tlv.Fields
	TLV                  ReceiptTLVs
	Diagnostics          []ReceiptDiagnostic
}

const receiptMaxBytes = 64 << 10
const receiptMaxTLVs = 256
const receiptMaxFields = 32

func (r *Receipt) issue(issue ReceiptIssue, source string, index int, field string) {
	r.Diagnostics = append(r.Diagnostics, ReceiptDiagnostic{issue, source, index, field})
}

// ParseReceipt extracts bounded evidence without changing strict Codec rules.
// It accepts typed bodies, including diagnostically malformed TLVs supplied by
// standalone callers; normal Session callers have already passed Codec checks.
// Use Codec separately for full command/field/version validation. Invalid
// options/body shape use ErrMetadata, hard bounds ErrCapacity, and unmarked
// messages ErrNotReceipt. Those errors return no partial result. Field problems
// instead return owned evidence plus Diagnostics and a nil error.
func ParseReceipt(body pdu.Body, options ReceiptParseOptions) (Receipt, error) {
	var r Receipt
	if !options.Version.Valid() || options.Dialect > ReceiptKeyValue {
		return r, ErrMetadata
	}
	var short []byte
	var optional tlv.Fields
	switch b := body.(type) {
	case *pdu.DeliverSM:
		if b == nil {
			return r, ErrMetadata
		}
		r.Source, r.Destination, r.ESMClass, r.DataCoding = b.Source, b.Destination, b.ESMClass, b.DataCoding
		short, optional = b.ShortMessage, b.Optional
		if len(b.ServiceType) > 5 {
			return Receipt{}, ErrMetadata
		}
	case *pdu.DataSM:
		if b == nil || options.Version == pdu.V33 {
			return r, ErrMetadata
		}
		r.Source, r.Destination, r.ESMClass, r.DataCoding = b.Source, b.Destination, b.ESMClass, b.DataCoding
		optional = b.Optional
		if len(b.ServiceType) > 5 {
			return Receipt{}, ErrMetadata
		}
	default:
		return r, ErrNotReceipt
	}
	r.Kind = ReceiptKind(r.ESMClass & 0x3c)
	if r.Kind != MCReceipt && r.Kind != IntermediateNotification {
		return Receipt{}, ErrNotReceipt
	}
	if options.Version == pdu.V33 && r.Kind != MCReceipt {
		return Receipt{}, ErrMetadata
	}
	if len(r.Source.Value) > 64 || len(r.Destination.Value) > 64 {
		return Receipt{}, ErrMetadata
	}
	// Bound work and copying before cloning any potentially attacker-sized input.
	total := len(short)
	if total > receiptMaxBytes || len(optional) > receiptMaxTLVs {
		return Receipt{}, ErrCapacity
	}
	for _, f := range optional {
		if len(f.Value) > 65535 || len(f.Value)+4 > receiptMaxBytes-total {
			return Receipt{}, ErrCapacity
		}
		total += 4 + len(f.Value)
	}
	r.Optional = optional.Clone()
	if options.Version == pdu.V33 && len(optional) != 0 {
		r.issue(ReceiptUnsupported, "pdu", -1, "tlv")
	}
	if len(short) > 0 {
		r.Texts = append(r.Texts, ReceiptText{Source: "short_message", TLVIndex: -1, Raw: bytes.Clone(short)})
	}
	// Count first: neither duplicate values nor duplicate payloads get precedence.
	counts := map[uint16]int{}
	for _, f := range optional {
		counts[f.Tag]++
	}
	for i, f := range optional {
		field := ""
		switch f.Tag {
		case tlv.ReceiptedMessageID:
			field = "id"
		case tlv.MessageState:
			field = "stat"
		case tlv.NetworkErrorCode:
			field = "network_error_code"
		case tlv.MessagePayload:
			field = "message_payload"
		default:
			continue
		}
		if counts[f.Tag] > 1 {
			r.issue(ReceiptDuplicate, "tlv", i, field)
			continue
		}
		if options.Version == pdu.V33 {
			continue // preserve unsupported bytes without interpreting any 3.3 TLV
		}
		if f.Tag == tlv.MessagePayload {
			r.Texts = append(r.Texts, ReceiptText{Source: "message_payload", TLVIndex: i, Raw: bytes.Clone(f.Value)})
			if len(short) > 0 {
				r.issue(ReceiptConflict, "pdu", -1, "payload")
			}
			continue
		}
		switch f.Tag {
		case tlv.ReceiptedMessageID:
			if len(f.Value) < 1 || len(f.Value) > 65 || f.Value[len(f.Value)-1] != 0 || bytes.IndexByte(f.Value[:len(f.Value)-1], 0) >= 0 || !receiptASCII(f.Value[:len(f.Value)-1], false) {
				r.issue(ReceiptMalformed, "tlv", i, field)
				continue
			}
			r.TLV.MessageID = string(f.Value[:len(f.Value)-1])
			r.TLV.HasMessageID = true
			if r.TLV.MessageID == "" {
				r.issue(ReceiptMissing, "tlv", i, field)
			}
		case tlv.MessageState:
			if len(f.Value) != 1 {
				r.issue(ReceiptMalformed, "tlv", i, field)
				continue
			}
			r.TLV.State, r.TLV.HasState = f.Value[0], true
			if !receiptStateValid(options.Version, f.Value[0]) {
				r.issue(ReceiptUnsupported, "tlv", i, field)
			}
		case tlv.NetworkErrorCode:
			if len(f.Value) != 3 {
				r.issue(ReceiptMalformed, "tlv", i, field)
				continue
			}
			r.TLV.NetworkError = &ReceiptNetworkError{f.Value[0], binary.BigEndian.Uint16(f.Value[1:])}
			maxType := byte(3)
			if options.Version == pdu.V50 {
				maxType = 8
			}
			if f.Value[0] < 1 || f.Value[0] > maxType {
				r.issue(ReceiptUnsupported, "tlv", i, field)
			}
		}
	}
	if options.Version != pdu.V33 && (len(r.Texts) == 0 || counts[tlv.ReceiptedMessageID] > 0 || counts[tlv.MessageState] > 0 || counts[tlv.NetworkErrorCode] > 0 || options.Version == pdu.V50) {
		if counts[tlv.ReceiptedMessageID] == 0 {
			r.issue(ReceiptMissing, "tlv", -1, "id")
		}
		if counts[tlv.MessageState] == 0 {
			r.issue(ReceiptMissing, "tlv", -1, "stat")
		}
	}
	if options.Version == pdu.V33 && len(r.Texts) == 0 {
		r.issue(ReceiptMissing, "pdu", -1, "payload")
	}
	for i := range r.Texts {
		if options.Dialect == ReceiptRawText {
			continue
		}
		if err := r.parseReceiptText(i, options.Version); err != nil {
			return Receipt{}, err
		}
		t := &r.Texts[i]
		if id, ok := t.Value("id"); ok && r.TLV.HasMessageID && id != r.TLV.MessageID {
			r.issue(ReceiptConflict, t.Source, t.TLVIndex, "id")
		}
		if state, ok := t.Value("stat"); ok && r.TLV.HasState {
			if n, known := receiptTextState(state); known && receiptStateValid(options.Version, r.TLV.State) && n != r.TLV.State {
				r.issue(ReceiptConflict, t.Source, t.TLVIndex, "stat")
			}
		}
	}
	// Simultaneous short_message and message_payload is already a carrier
	// conflict. Also expose differing comparable identity/state evidence rather
	// than silently choosing the first carrier when no standard TLV is present.
	if len(r.Texts) == 2 {
		left, right := r.Texts[0], r.Texts[1]
		if a, ok := left.Value("id"); ok {
			if b, ok := right.Value("id"); ok && a != b {
				r.issue(ReceiptConflict, right.Source, right.TLVIndex, "id")
			}
		}
		if a, ok := left.Value("stat"); ok {
			if b, ok := right.Value("stat"); ok {
				x, hx := receiptTextState(a)
				y, hy := receiptTextState(b)
				if hx && hy && x != y {
					r.issue(ReceiptConflict, right.Source, right.TLVIndex, "stat")
				}
			}
		}
	}
	return r, nil
}

func receiptStateValid(v pdu.Version, state byte) bool {
	if v == pdu.V50 {
		return state <= 9
	}
	return state >= 1 && state <= 8
}
func receiptASCII(b []byte, printable bool) bool {
	for _, c := range b {
		if c > 127 || c == 0 || printable && (c < 32 || c == 127) {
			return false
		}
	}
	return true
}
