// SPDX-AI-Disclosure: ai-generated
// SPDX-AI-Model: GPT-6.0 Astra
// SPDX-AI-Provider: OpenAI
// SPDX-AI-Scope: Benchmark code generated and revised with Codex under maintainer direction.
// SPDX-AI-Date: 2026-11-06

package message_test

import (
	"errors"
	"testing"

	"github.com/skill215/smppkit/message"
	"github.com/skill215/smppkit/pdu"
	"github.com/skill215/smppkit/tlv"
)

// Prepare includes owned snapshots; Materialize excludes preparation and includes
// owned output buffers. Neither measures reference allocation or transport.
func BenchmarkBinaryPlan(b *testing.B) {
	for _, tc := range []struct {
		name string
		size int
		mode message.Mode
	}{
		{"Single140", 140, message.UDH16},
		{"UDH8_1024", 1024, message.UDH8},
		{"UDH16_1024", 1024, message.UDH16},
		{"SAR1024", 1024, message.SAR},
		{"Payload65535", 65535, message.Payload},
	} {
		b.Run(tc.name, func(b *testing.B) {
			msg := message.Binary{Data: make([]byte, tc.size), DataCoding: 4}
			policy := message.BinaryPolicy{Mode: tc.mode}
			plan, err := message.PrepareBytes(pdu.V50, msg, policy)
			if err != nil {
				b.Fatal(err)
			}
			b.Run("Prepare", func(b *testing.B) {
				var got *message.Plan
				b.ReportAllocs()
				b.SetBytes(int64(tc.size))
				for b.Loop() {
					got, err = message.PrepareBytes(pdu.V50, msg, policy)
					if err != nil {
						b.Fatal(err)
					}
				}
				benchmarkPlan = got
				b.ReportMetric(float64(got.Count()), "segments/op")
			})
			b.Run("Materialize", func(b *testing.B) {
				var parts []pdu.SMFields
				b.ReportAllocs()
				b.SetBytes(int64(tc.size))
				for b.Loop() {
					parts, err = plan.Materialize(42)
					if err != nil {
						b.Fatal(err)
					}
				}
				benchmarkParts = parts
				b.ReportMetric(float64(len(parts)), "segments/op")
			})
		})
	}
}

// Repeated TLV materialization would exceed the output budget. Rejection must
// precede copying the input or duplicating metadata into hundreds of segments.
func BenchmarkBinaryOutputRejection(b *testing.B) {
	msg := message.Binary{Data: make([]byte, 32<<10), DataCoding: 4, Optional: tlv.Fields{{Tag: 0x1400, Value: make([]byte, 4096)}}}
	b.ReportAllocs()
	for b.Loop() {
		if _, err := message.PrepareBytes(pdu.V50, msg, message.BinaryPolicy{}); !errors.Is(err, message.ErrCapacity) {
			b.Fatal(err)
		}
	}
}
