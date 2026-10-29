// SPDX-AI-Disclosure: ai-generated
// SPDX-AI-Model: GPT-6.0 Astra
// SPDX-AI-Provider: OpenAI
// SPDX-AI-Scope: Benchmark code generated and revised with Codex under maintainer direction.
// SPDX-AI-Date: 2026-10-29

package smpp

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/skill215/smppkit/pdu"
)

// Enabled sink separates record construction from formatting and output I/O.
// Embedding DiscardHandler supplies unused WithAttrs/WithGroup methods.
type benchmarkLogSink struct{ slog.Handler }

func (benchmarkLogSink) Enabled(context.Context, slog.Level) bool  { return true }
func (benchmarkLogSink) Handle(context.Context, slog.Record) error { return nil }

func loggingBenchmarks() []struct {
	name   string
	logger *slog.Logger
} {
	return []struct {
		name   string
		logger *slog.Logger
	}{
		{"DefaultNil", nil},
		{"Discard", slog.New(slog.DiscardHandler)},
		{"FilteredInfo", slog.New(slog.NewJSONHandler(io.Discard, nil))},
		{"EnabledSink", slog.New(benchmarkLogSink{slog.DiscardHandler})},
		{"JSON", slog.New(slog.NewJSONHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelDebug}))},
		{"Text", slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelDebug}))},
	}
}

// One successful request diagnostic, excluding Session/network work. Disabled
// cases must not build the record; enabled output cases include formatting but
// write to io.Discard, so they do not measure file or terminal performance.
func BenchmarkLoggingEvent(b *testing.B) {
	for _, tc := range loggingBenchmarks() {
		b.Run(tc.name, func(b *testing.B) {
			cfg, err := (Config{Logger: tc.logger}).normalized()
			if err != nil {
				b.Fatal(err)
			}
			s := &Session{cfg: cfg, codec: testCodec(b), namespace: "benchmark-session"}
			r := Result{Sequence: 42, Outcome: ResponseReceived, BytesWritten: 16}
			ctx := b.Context()
			b.ReportAllocs()
			for b.Loop() {
				s.logRequest(ctx, pdu.EnquireLinkID, r, nil)
			}
		})
	}
}

// Both endpoints use the selected logger. Setup/teardown are outside b.Loop;
// each operation is one manual enquiry and its automatic response, over net.Pipe.
func BenchmarkSessionLogging(b *testing.B) {
	for _, tc := range loggingBenchmarks() {
		b.Run(tc.name, func(b *testing.B) {
			s := benchmarkSessionsConfig(b, Config{Window: 1, Logger: tc.logger})
			ctx := b.Context()
			body := &pdu.EnquireLink{}
			b.ReportAllocs()
			for b.Loop() {
				r, err := s.Request(ctx, body)
				if err != nil || r.Outcome != ResponseReceived {
					b.Fatal(r, err)
				}
			}
		})
	}
}
