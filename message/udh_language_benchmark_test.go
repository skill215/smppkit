// SPDX-AI-Disclosure: ai-generated
// SPDX-AI-Model: GPT-6.0 Astra
// SPDX-AI-Provider: OpenAI
// SPDX-AI-Scope: Benchmark code generated and revised with Codex under maintainer direction.
// SPDX-AI-Date: 2026-10-29

package message_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/skill215/smppkit/message"
	"github.com/skill215/smppkit/pdu"
	"github.com/skill215/smppkit/textcodec"
)

// Equal-size inputs compare the complete bounded admission/receive path, not
// just the guard: ownership copies, message ID allocation and optional decoding
// are included. The receiver is reused and does not accumulate queued messages.
func BenchmarkReceiveLanguagePolicy(b *testing.B) {
	for _, tc := range []struct {
		name                string
		raw                 []byte
		decode, unsupported bool
	}{
		{"OrdinaryText", []byte{0, 0x41, 0x41, 0x41, 0x41}, true, false},
		{"OpaqueIE", []byte{3, 0x70, 1, 1, 0x41}, true, false},
		{"LanguageBlocked", []byte{3, 0x25, 1, 1, 0x0b}, true, true},
		{"LanguageRaw", []byte{3, 0x25, 1, 1, 0x0b}, false, false},
	} {
		b.Run(tc.name, func(b *testing.B) {
			cfg := message.ReceiveConfig{}
			if tc.decode {
				cfg.Encodings = map[uint8]textcodec.Encoding{0: textcodec.GSM7}
			}
			r := receiver(b, cfg)
			fields := pdu.SMFields{ESMClass: 0x40, ShortMessage: tc.raw}
			ctx := context.Background()
			now := time.Now()
			b.ReportAllocs()
			b.SetBytes(int64(len(tc.raw)))
			for b.Loop() {
				if _, err := r.Admit("benchmark", fields, now); err != nil {
					b.Fatal(err)
				}
				m, err := r.Receive(ctx)
				if err != nil || errors.Is(m.DecodeError, message.ErrUnsupportedNationalLanguage) != tc.unsupported {
					b.Fatalf("receive=%v decode=%v", err, m.DecodeError)
				}
			}
		})
	}
}
