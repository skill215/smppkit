package message_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/skill215/smppkit/message"
	"github.com/skill215/smppkit/pdu"
	"github.com/skill215/smppkit/textcodec"
)

var benchmarkPlan *message.Plan
var benchmarkParts []pdu.SMFields
var benchmarkMessage message.Message

func benchmarkModeName(mode message.Mode) string {
	switch mode {
	case message.UDH8:
		return "UDH8"
	case message.UDH16:
		return "UDH16"
	case message.SAR:
		return "SAR"
	default:
		panic("unknown benchmark mode")
	}
}

func BenchmarkMessagePlan(b *testing.B) {
	for _, mode := range []message.Mode{message.UDH8, message.UDH16, message.SAR} {
		b.Run(benchmarkModeName(mode), func(b *testing.B) {
			text := message.Text{Text: strings.Repeat("Hello ^", 80)}
			policy := message.GSMText()
			policy.Mode = mode
			plan, err := message.Prepare(pdu.V50, text, policy)
			if err != nil {
				b.Fatal(err)
			}
			b.Run("Prepare", func(b *testing.B) {
				var got *message.Plan
				b.ReportAllocs()
				for b.Loop() {
					got, err = message.Prepare(pdu.V50, text, policy)
					if err != nil {
						b.Fatal(err)
					}
				}
				benchmarkPlan = got
				b.ReportMetric(float64(got.Count()), "segments/op")
			})
			// The immutable plan is built outside timing. Materialize includes
			// owned copies and headers/TLVs, but no reference lease or transport.
			b.Run("Materialize", func(b *testing.B) {
				var parts []pdu.SMFields
				b.ReportAllocs()
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

// One operation includes NewReceiver, all segment admissions, and Receive with
// decoding. Fresh bounded receivers avoid an ever-growing tombstone/reference
// set across iterations. Fixture preparation is excluded; construction is not.
func BenchmarkReceiverReassemble(b *testing.B) {
	for _, mode := range []message.Mode{message.UDH16, message.SAR} {
		for _, reverse := range []bool{false, true} {
			b.Run(fmt.Sprintf("%s/Reverse%t", benchmarkModeName(mode), reverse), func(b *testing.B) {
				text := strings.Repeat("Hello ^", 80)
				policy := message.GSMText()
				policy.Mode = mode
				plan, err := message.Prepare(pdu.V50, message.Text{Text: text}, policy)
				if err != nil {
					b.Fatal(err)
				}
				parts, err := plan.Materialize(42)
				if err != nil {
					b.Fatal(err)
				}
				cfg := message.ReceiveConfig{AckPolicy: message.AckOnBuffered, Encodings: map[uint8]textcodec.Encoding{0: textcodec.GSM7}}
				ctx := b.Context()
				now := time.Unix(1, 0)
				var got message.Message
				b.ReportAllocs()
				for b.Loop() {
					r, err := message.NewReceiver(cfg)
					if err != nil {
						b.Fatal(err)
					}
					var admission message.Admission
					for i := range parts {
						if reverse {
							i = len(parts) - 1 - i
						}
						if admission, err = r.Admit("bench", parts[i], now); err != nil {
							b.Fatal(err)
						}
					}
					if !admission.Complete {
						b.Fatal("last segment did not complete message")
					}
					got, err = r.Receive(ctx)
					if err != nil || got.DecodeError != nil {
						b.Fatalf("receive: %v decode: %v", err, got.DecodeError)
					}
				}
				benchmarkMessage = got
				if !got.Decoded || got.Text != text {
					b.Fatal("assembled text mismatch")
				}
				b.ReportMetric(float64(len(parts)), "segments/op")
			})
		}
	}
}

// Repeated admission of an already buffered segment measures parsing, cloning
// and duplicate hashing. It must not grow the group or consume completion slots.
func BenchmarkReceiverDuplicate(b *testing.B) {
	plan, err := message.Prepare(pdu.V50, message.Text{Text: strings.Repeat("A", 300)}, message.GSMText())
	if err != nil {
		b.Fatal(err)
	}
	parts, err := plan.Materialize(42)
	if err != nil {
		b.Fatal(err)
	}
	r, err := message.NewReceiver(message.ReceiveConfig{AckPolicy: message.AckOnBuffered})
	if err != nil {
		b.Fatal(err)
	}
	now := time.Unix(1, 0)
	if _, err = r.Admit("bench", parts[0], now); err != nil {
		b.Fatal(err)
	}
	before := r.Stats()
	b.ReportAllocs()
	for b.Loop() {
		admission, err := r.Admit("bench", parts[0], now)
		if err != nil || !admission.Duplicate {
			b.Fatalf("duplicate: %+v %v", admission, err)
		}
	}
	if r.Stats() != before {
		b.Fatal("duplicate changed retained resources")
	}
}
