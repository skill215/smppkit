// SPDX-AI-Disclosure: ai-generated
// SPDX-AI-Model: GPT-6.0 Astra
// SPDX-AI-Provider: OpenAI
// SPDX-AI-Scope: Benchmark code generated and revised with Codex under maintainer direction.
// SPDX-AI-Date: 2026-11-06

package codec_test

import (
	"bytes"
	"errors"
	"testing"

	"github.com/skill215/smppkit/codec"
	"github.com/skill215/smppkit/pdu"
)

var semanticBenchmarkError error
var semanticBenchmarkDiagnostic *codec.SemanticError
var semanticBenchmarkText string

// BenchmarkSemanticDiagnostics separates successful protocol paths, failure
// construction, contextual validation, inspection and formatting. Buffers and
// codecs are prepared before b.Loop. These are not transport or copy counters.
func BenchmarkSemanticDiagnostics(b *testing.B) {
	c := newCodec(b, pdu.V50)
	payload := bytes.Repeat([]byte{'A'}, 140)
	packet := pdu.PDU{Sequence: 7, Body: &pdu.SubmitSM{SMFields: pdu.SMFields{ShortMessage: payload}}}
	body := make([]byte, 17)
	body[16] = 140
	raw := semanticFrame(pdu.SubmitSMID, pdu.OK, append(body, payload...))
	badTON := unhex(b, diagnosticTONHex)
	badTLV := unhex(b, diagnosticTLVHex)
	badPacket := pdu.PDU{Sequence: 7, Body: &pdu.SubmitSM{SMFields: pdu.SMFields{Source: pdu.Address{TON: 7}}}}
	if _, err := c.Decode(raw); err != nil {
		b.Fatal(err)
	}
	_, diagnosticErr := c.Decode(badTON)
	diagnostic := wantDiagnostic(b, diagnosticErr, "source_addr_ton", codec.RuleValue, -1, -1)
	for _, tc := range []struct {
		name  string
		raw   []byte
		valid bool
	}{
		{"DecodeValid140", raw, true}, {"DecodeInvalidTON", badTON, false}, {"DecodeDuplicateTLV", badTLV, false},
	} {
		b.Run(tc.name, func(b *testing.B) {
			_, err := c.Decode(tc.raw)
			if (err == nil) != tc.valid {
				b.Fatal(err)
			}
			b.ReportAllocs()
			for b.Loop() {
				benchmarkPacket, semanticBenchmarkError = c.Decode(tc.raw)
			}
		})
	}
	b.Run("AppendValid140Reuse", func(b *testing.B) {
		dst := make([]byte, 0, len(raw))
		b.ReportAllocs()
		for b.Loop() {
			benchmarkBytes, semanticBenchmarkError = c.Append(dst[:0], packet)
		}
	})
	b.Run("AppendInvalidTON", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			benchmarkBytes, semanticBenchmarkError = c.Append(nil, badPacket)
		}
	})
	for _, tc := range []struct {
		name string
		body *pdu.DataSM
	}{
		{"ContextValid", &pdu.DataSM{}},
		{"ContextDatagramFailure", &pdu.DataSM{ESMClass: 1, RegisteredDelivery: 1}},
	} {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				semanticBenchmarkError = c.ValidateDataDirection(tc.body, codec.DataSubmission)
			}
		})
	}
	b.Run("InspectAsType", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			semanticBenchmarkDiagnostic, _ = errors.AsType[*codec.SemanticError](diagnosticErr)
		}
	})
	b.Run("Format", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			semanticBenchmarkText = diagnostic.Error()
		}
	})
}
