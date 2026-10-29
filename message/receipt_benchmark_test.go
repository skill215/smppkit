// SPDX-AI-Disclosure: ai-generated
// SPDX-AI-Model: GPT-6.0 Astra
// SPDX-AI-Provider: OpenAI
// SPDX-AI-Scope: Benchmark code generated and revised with Codex under maintainer direction.
// SPDX-AI-Date: 2026-10-29

package message_test

import (
	"testing"

	"github.com/skill215/smppkit/message"
	"github.com/skill215/smppkit/pdu"
	"github.com/skill215/smppkit/tlv"
)

var benchmarkReceipt message.Receipt
var benchmarkReceiptBody pdu.Body

// Parsing includes owned evidence copies and diagnostics; body construction is
// excluded. No Session/ACK, sockets, aggregation or state tracking is measured.
func BenchmarkReceiptParse(b *testing.B) {
	fields := tlv.Fields{{Tag: tlv.ReceiptedMessageID, Value: []byte("00000A1\x00")}, {Tag: tlv.MessageState, Value: []byte{5}}, {Tag: tlv.NetworkErrorCode, Value: []byte{3, 0, 1}}}
	for _, tc := range []struct {
		name, text string
		optional   tlv.Fields
		dialect    message.ReceiptDialect
	}{
		{"FullText", fullReceiptText, nil, message.ReceiptKeyValue},
		{"TLVOnly", "", fields, message.ReceiptKeyValue},
		{"Mixed", fullReceiptText, fields, message.ReceiptKeyValue},
		{"MissingFields", "id:00000A1 stat:UNDELIV", nil, message.ReceiptKeyValue},
		{"Malformed", "id:a id:b sub:1 stat:NEW done date:bad", fields, message.ReceiptKeyValue},
		{"RawOnly", fullReceiptText, fields, message.ReceiptRawText},
	} {
		b.Run(tc.name, func(b *testing.B) {
			body := receiptBody(tc.text)
			body.Optional = tc.optional
			options := message.ReceiptParseOptions{Version: pdu.V34, Dialect: tc.dialect}
			b.ReportAllocs()
			for b.Loop() {
				r, e := message.ParseReceipt(body, options)
				if e != nil {
					b.Fatal(e)
				}
				benchmarkReceipt = r
			}
		})
	}
}

// Construction includes format checks, owned PDU storage and standard Codec
// preflight encoding (discarded wire buffer); it is not pure string formatting.
func BenchmarkReceiptBuild(b *testing.B) {
	text := "hi"
	for _, tc := range []struct {
		name    string
		v       pdu.Version
		format  message.ReceiptFormat
		command pdu.CommandID
	}{
		{"LegacyText", pdu.V33, message.ReceiptTextOnly, pdu.DeliverSMID},
		{"Text", pdu.V50, message.ReceiptTextOnly, pdu.DeliverSMID},
		{"TLVOnly", pdu.V50, message.ReceiptTLVOnly, pdu.DeliverSMID},
		{"Mixed", pdu.V50, message.ReceiptTextAndTLV, pdu.DeliverSMID},
		{"DataMixed", pdu.V50, message.ReceiptTextAndTLV, pdu.DataSMID},
	} {
		b.Run(tc.name, func(b *testing.B) {
			spec := message.ReceiptSpec{Kind: message.MCReceipt, MessageID: "00000A1", State: 5}
			if tc.format != message.ReceiptTLVOnly {
				spec.Text = &message.ReceiptTextSpec{Submitted: "001", Delivered: "000", SubmitDate: "2610291100", DoneDate: "2610291101", Error: "0AF", Text: &text}
			}
			options := message.ReceiptBuildOptions{Version: tc.v, Command: tc.command, Format: tc.format}
			b.ReportAllocs()
			for b.Loop() {
				body, e := message.BuildReceipt(spec, options)
				if e != nil {
					b.Fatal(e)
				}
				benchmarkReceiptBody = body
			}
		})
	}
}
