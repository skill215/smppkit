// SPDX-AI-Disclosure: ai-generated
// SPDX-AI-Model: GPT-6.0 Astra
// SPDX-AI-Provider: OpenAI
// SPDX-AI-Scope: Library code generated and revised with Codex under maintainer direction.
// SPDX-AI-Date: 2026-10-29

package message

import (
	"strings"

	"github.com/skill215/smppkit/codec"
	"github.com/skill215/smppkit/pdu"
	"github.com/skill215/smppkit/textcodec"
	"github.com/skill215/smppkit/tlv"
)

// ReceiptFormat selects an explicit peer representation, not a protocol default.
// Text formats use the documented ReceiptKeyValue dialect; text-only 3.4/5.0
// is a gateway compatibility convention, not the standard TLV representation.
type ReceiptFormat uint8

const (
	ReceiptTextOnly ReceiptFormat = iota + 1
	ReceiptTLVOnly
	ReceiptTextAndTLV
)

// ReceiptBuildOptions requires an explicit version, command and representation.
// Command must be DeliverSMID or DataSMID (3.4/5.0 only). deliver_sm text uses
// short_message; data_sm text uses message_payload. No automatic segmentation.
type ReceiptBuildOptions struct {
	Version pdu.Version
	Command pdu.CommandID
	Format  ReceiptFormat
}

// ReceiptTextSpec contains optional dialect fields. Empty scalar strings omit
// fields, rather than inventing counts or timestamps. Counts, when supplied,
// are three digits; dates are ten-digit YYMMDDhhmm civil components, with no
// century or zone interpretation. Error is an opaque 1..32 byte token.
// Text nil omits the field; a pointer to "" emits an explicitly empty field.
// Text is at most 20 printable ASCII characters; nothing is truncated.
type ReceiptTextSpec struct {
	Submitted, Delivered, SubmitDate, DoneDate, Error string
	Text                                              *string
}

// ReceiptSpec is caller-provided report evidence. MessageID is opaque, never
// generated or radix-normalized: 1..64 non-NUL ASCII octets for TLV-only output,
// further restricted to a token by text formats. State is message_state; it
// does not infer success from an earlier submission response. Source/Destination
// are the report addresses, normally reversed from the original message.
// Kind and State remain distinct; the helper does not decide delivery finality.
// Text is legal only in text-bearing formats; NetworkError only in TLV formats.
type ReceiptSpec struct {
	Kind                ReceiptKind
	MessageID           string
	State               uint8
	Source, Destination pdu.Address
	Text                *ReceiptTextSpec
	NetworkError        *ReceiptNetworkError
}

// BuildReceipt creates an owned existing concrete DeliverSM or DataSM body.
// It creates no connection, timers, correlation, ACK, retries or persistence.
// Format-specific checks use ErrMetadata; full protocol checks reuse a standard
// versioned Codec and return its errors. No custom TLVs are supplied here: callers
// may add them afterward and validate with their configured Codec/Session.
// There is no partial output on error. Nil Text in a text-bearing format emits
// only explicit id/stat; missing optional fields are not fabricated.
func BuildReceipt(spec ReceiptSpec, options ReceiptBuildOptions) (pdu.Body, error) {
	if !options.Version.Valid() || options.Command != pdu.DeliverSMID && options.Command != pdu.DataSMID || options.Format < ReceiptTextOnly || options.Format > ReceiptTextAndTLV {
		return nil, ErrMetadata
	}
	if spec.Kind != MCReceipt && spec.Kind != IntermediateNotification || !receiptStateValid(options.Version, spec.State) {
		return nil, ErrMetadata
	}
	if len(spec.MessageID) == 0 || len(spec.MessageID) > 64 || !receiptASCII([]byte(spec.MessageID), false) || len(spec.Source.Value) > 64 || len(spec.Destination.Value) > 64 {
		return nil, ErrMetadata
	}
	hasText, hasTLV := options.Format != ReceiptTLVOnly, options.Format != ReceiptTextOnly
	if options.Version == pdu.V33 && (options.Command != pdu.DeliverSMID || hasTLV || spec.Kind != MCReceipt) {
		return nil, ErrMetadata
	}
	if !hasText && spec.Text != nil || !hasTLV && spec.NetworkError != nil {
		return nil, ErrMetadata
	}
	var payload []byte
	coding := byte(0)
	if hasText {
		// There is no standard Appendix-B text spelling for the 5.0-only numeric
		// SCHEDULED/SKIPPED states. Use TLV-only instead of inventing one.
		if spec.State < 1 || int(spec.State) >= len(receiptStates) || !receiptToken(spec.MessageID, 64) {
			return nil, ErrMetadata
		}
		var b strings.Builder
		b.WriteString("id:")
		b.WriteString(spec.MessageID)
		field := func(label, value string) {
			b.WriteByte(' ')
			b.WriteString(label)
			b.WriteByte(':')
			b.WriteString(value)
		}
		if t := spec.Text; t != nil {
			for _, v := range []struct{ label, value string }{{"sub", t.Submitted}, {"dlvrd", t.Delivered}, {"submit date", t.SubmitDate}, {"done date", t.DoneDate}} {
				if v.value == "" {
					continue
				}
				if (v.label == "sub" || v.label == "dlvrd") && !receiptDigits(v.value, 3) || (v.label == "submit date" || v.label == "done date") && !receiptDate(v.value) {
					return nil, ErrMetadata
				}
				field(v.label, v.value)
			}
		}
		field("stat", receiptStates[spec.State])
		if t := spec.Text; t != nil {
			if t.Error != "" {
				if !receiptToken(t.Error, 32) {
					return nil, ErrMetadata
				}
				field("err", t.Error)
			}
			if t.Text != nil {
				if len(*t.Text) > 20 || !receiptASCII([]byte(*t.Text), true) {
					return nil, ErrMetadata
				}
				field("text", *t.Text)
			}
		}
		if options.Version == pdu.V33 {
			encoded, err := textcodec.GSM7.Encode(b.String())
			if err != nil {
				return nil, ErrMetadata
			}
			payload = encoded.Bytes
		} else {
			coding = 1
			payload = []byte(b.String())
		} // explicit ASCII; never guess SMSC default alphabet
	}
	var optional tlv.Fields
	if hasTLV {
		optional = append(optional, tlv.Field{Tag: tlv.ReceiptedMessageID, Value: append([]byte(spec.MessageID), 0)}, tlv.Field{Tag: tlv.MessageState, Value: []byte{spec.State}})
		if n := spec.NetworkError; n != nil {
			optional = append(optional, tlv.Field{Tag: tlv.NetworkErrorCode, Value: []byte{n.Type, byte(n.Code >> 8), byte(n.Code)}})
		}
	}
	var body pdu.Body
	if options.Command == pdu.DeliverSMID {
		body = &pdu.DeliverSM{SMFields: pdu.SMFields{Source: spec.Source, Destination: spec.Destination, ESMClass: byte(spec.Kind), DataCoding: coding, ShortMessage: payload, Optional: optional}}
	} else {
		if hasText {
			optional = append(optional, tlv.Field{Tag: tlv.MessagePayload, Value: payload})
		}
		body = &pdu.DataSM{Source: spec.Source, Destination: spec.Destination, ESMClass: byte(spec.Kind), DataCoding: coding, Optional: optional}
	}
	// Reuse protocol widths/ranges instead of maintaining another validation
	// catalog. The temporary wire buffer is discarded; output never borrows it.
	c, err := codec.New(codec.Config{Version: options.Version})
	if err != nil {
		return nil, err
	}
	if _, err = c.Append(nil, pdu.PDU{Sequence: 1, Body: body}); err != nil {
		return nil, err
	}
	if options.Command == pdu.DataSMID {
		if err = c.ValidateDataDirection(body, codec.DataDelivery); err != nil {
			return nil, err
		}
	}
	return body, nil
}
