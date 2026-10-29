// SPDX-AI-Disclosure: ai-generated
// SPDX-AI-Model: GPT-6.0 Astra
// SPDX-AI-Provider: OpenAI
// SPDX-AI-Scope: Test code generated and revised with Codex under maintainer direction.
// SPDX-AI-Date: 2026-10-29

package message_test

import (
	"bytes"
	"testing"

	"github.com/skill215/smppkit/message"
	"github.com/skill215/smppkit/pdu"
	"github.com/skill215/smppkit/tlv"
)

// Typed malformed evidence complements the strict wire decoder fuzzers: receipt
// extraction must bound work and retain ownership even for direct callers that
// did not pass through Codec. Construction success must still pass that Codec.
func FuzzReceipt(f *testing.F) {
	for _, s := range []string{fullReceiptText, "id:a stat:DELIVRD", "id:a id:b stat:UNKNOWN text:id:x", "private:x stat:UNDELIV", "id: stat:NEW", "\x1b"} {
		f.Add([]byte(s), []byte{2}, byte(1), byte(0))
	}
	f.Fuzz(func(t *testing.T, payload, value []byte, coding, flags byte) {
		if len(payload) > 70000 || len(value) > 70000 {
			return
		}
		versions := [...]pdu.Version{pdu.V33, pdu.V34, pdu.V50}
		v := versions[int(flags)%3]
		original := bytes.Clone(payload)
		originalValue := bytes.Clone(value)
		body := &pdu.DeliverSM{SMFields: pdu.SMFields{ESMClass: 4, DataCoding: coding, ShortMessage: payload, Optional: tlv.Fields{{Tag: tlv.MessageState, Value: value}}}}
		if flags&4 != 0 {
			body.ESMClass = 0x20
		}
		if flags&8 != 0 {
			body.ESMClass |= 0x40
		}
		if flags&16 != 0 {
			body.Optional = append(body.Optional, tlv.Field{Tag: tlv.MessageState, Value: value})
		}
		if flags&32 != 0 {
			body.Optional = append(body.Optional, tlv.Field{Tag: tlv.ReceiptedMessageID, Value: value}, tlv.Field{Tag: tlv.NetworkErrorCode, Value: value})
		}
		if flags&64 != 0 {
			body.Optional = append(body.Optional, tlv.Field{Tag: tlv.MessagePayload, Value: payload})
		}
		dialect := message.ReceiptKeyValue
		if flags&128 != 0 {
			dialect = message.ReceiptRawText
		}
		r, e := message.ParseReceipt(body, message.ReceiptParseOptions{Version: v, Dialect: dialect})
		if e == nil {
			if len(r.Diagnostics) > 600 || len(r.Texts) > 2 {
				t.Fatal("unbounded result")
			}
			for _, text := range r.Texts {
				if len(text.Fields) > 32 {
					t.Fatal("unbounded text fields")
				}
				if len(text.Raw) > 0 {
					text.Raw[0] ^= 0xff
				}
			}
			for _, field := range r.Optional {
				if len(field.Value) > 0 {
					field.Value[0] ^= 0xff
				}
			}
		}
		if !bytes.Equal(payload, original) || !bytes.Equal(value, originalValue) {
			t.Fatal("parser aliases input")
		}
		spec := message.ReceiptSpec{Kind: message.MCReceipt, MessageID: string(payload), State: coding, NetworkError: &message.ReceiptNetworkError{Type: flags, Code: 0x1234}}
		built, e := message.BuildReceipt(spec, message.ReceiptBuildOptions{Version: v, Command: pdu.DeliverSMID, Format: message.ReceiptTLVOnly})
		if e == nil {
			if built == nil {
				t.Fatal("nil successful construction")
			}
			wire, e := receiptCodec(t, v).Append(nil, pdu.PDU{Sequence: 1, Body: built})
			if e != nil {
				t.Fatal(e)
			}
			if _, e = receiptCodec(t, v).Decode(wire); e != nil {
				t.Fatal(e)
			}
		} else if built != nil {
			t.Fatal("partial construction on error")
		}
	})
}
