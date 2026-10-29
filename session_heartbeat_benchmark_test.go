// SPDX-AI-Disclosure: ai-generated
// SPDX-AI-Model: GPT-6.0 Astra
// SPDX-AI-Provider: OpenAI
// SPDX-AI-Scope: Benchmark code generated and revised with Codex under maintainer direction.
// SPDX-AI-Date: 2026-10-29

package smpp

import (
	"errors"
	"net"
	"testing"
	"time"

	"github.com/skill215/smppkit/pdu"
)

// Compare the shared request paths without waiting on wall-clock intervals.
// Both peers, framing/correlation and net.Pipe copies are included. Timer wakeup
// latency and real network throughput are deliberately not measured here.
func BenchmarkHeartbeatRequest(b *testing.B) {
	for _, automatic := range []bool{false, true} {
		name := "Manual"
		if automatic {
			name = "Automatic"
		}
		b.Run(name, func(b *testing.B) {
			s := benchmarkSessions(b)
			ctx := b.Context()
			body := &pdu.EnquireLink{}
			b.ReportAllocs()
			for b.Loop() {
				r, err := s.request(ctx, body, automatic)
				if err != nil || r.Outcome != ResponseReceived {
					b.Fatal(r, err)
				}
				benchmarkResult = r
			}
		})
	}
	b.Run("WindowFull", func(b *testing.B) {
		s := benchmarkSessions(b)
		s.slots <- struct{}{}
		defer func() { <-s.slots }()
		ctx := b.Context()
		body := &pdu.EnquireLink{}
		b.ReportAllocs()
		for b.Loop() {
			r, err := s.request(ctx, body, true)
			if !errors.Is(err, ErrOverloaded) || r.Sequence != 0 {
				b.Fatal(r, err)
			}
		}
	})
}

// Includes construction, runtime startup, Close/Wait and net.Pipe allocation.
// Enabled cases use a long interval so the benchmark never waits for a probe.
func BenchmarkHeartbeatLifecycle(b *testing.B) {
	for _, tc := range []struct {
		name     string
		interval time.Duration
		state    State
	}{
		{"DisabledBound", 0, Bound}, {"EnabledBeforeBind", time.Hour, Connected}, {"EnabledBound", time.Hour, Bound},
	} {
		b.Run(tc.name, func(b *testing.B) {
			c := testCodec(b)
			cfg := Config{EnquireLinkInterval: tc.interval}
			b.ReportAllocs()
			for b.Loop() {
				a, peer := net.Pipe()
				s, err := newSession(a, c, cfg, false, tc.state, Transmitter)
				if err != nil {
					a.Close()
					peer.Close()
					b.Fatal(err)
				}
				s.Close()
				peer.Close()
				s.Wait()
			}
		})
	}
}
