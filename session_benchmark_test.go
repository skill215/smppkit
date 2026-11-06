// SPDX-AI-Disclosure: ai-generated
// SPDX-AI-Model: GPT-6.0 Astra
// SPDX-AI-Provider: OpenAI
// SPDX-AI-Scope: Benchmark code generated and revised with Codex under maintainer direction.
// SPDX-AI-Date: 2026-11-06

package smpp

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/skill215/smppkit/message"
	"github.com/skill215/smppkit/pdu"
)

var benchmarkResult Result
var benchmarkSendResult SendResult

// Sessions begin bound outside the timed loop. Each operation includes both
// endpoints' encoding, framing, correlation and in-memory transport work, but
// excludes TCP/TLS, bind, startup and teardown. This is not a network TPS test.
func benchmarkSessions(b *testing.B) *Session {
	return benchmarkSessionsConfig(b, Config{Window: 1})
}

func benchmarkSessionsConfig(b *testing.B, cfg Config) *Session {
	b.Helper()
	a, peer := net.Pipe()
	c := testCodec(b)
	client, err := newSession(a, c, cfg, false, Bound, Transceiver)
	if err != nil {
		a.Close()
		peer.Close()
		b.Fatal(err)
	}
	server, err := newSession(peer, c, cfg, true, Bound, Transceiver)
	if err != nil {
		client.Close()
		client.Wait()
		peer.Close()
		b.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		for {
			in, err := server.Receive(b.Context())
			if err != nil {
				done <- err
				return
			}
			if err = in.Respond(b.Context(), pdu.OK, &pdu.SubmitSMResp{MessageResponseFields: pdu.MessageResponseFields{MessageID: "bench"}}); err != nil {
				done <- err
				return
			}
		}
	}()
	b.Cleanup(func() {
		client.Close()
		server.Close()
		client.Wait()
		server.Wait()
		// Closing one endpoint may make the other observe EOF/closed pipe.
		if err := <-done; err != nil && !errors.Is(err, ErrClosed) && !errors.Is(err, context.Canceled) && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrClosedPipe) && !errors.Is(err, net.ErrClosed) {
			b.Errorf("peer cleanup: %v", err)
		}
	})
	// Start reader/writer activity before timing; cleanup remains outside b.Loop.
	if _, err := client.Request(b.Context(), &pdu.EnquireLink{}); err != nil {
		b.Fatal(err)
	}
	return client
}

func BenchmarkSessionRequest(b *testing.B) {
	for _, tc := range []struct {
		name string
		body pdu.Body
	}{
		{"EnquireLink", &pdu.EnquireLink{}},
		{"SubmitSM140", &pdu.SubmitSM{SMFields: pdu.SMFields{ShortMessage: bytes.Repeat([]byte{'A'}, 140)}}},
	} {
		b.Run(tc.name, func(b *testing.B) {
			s := benchmarkSessions(b)
			ctx := b.Context()
			var result Result
			b.ReportAllocs()
			for b.Loop() {
				var err error
				result, err = s.Request(ctx, tc.body)
				if err != nil || result.Outcome != ResponseReceived {
					b.Fatalf("response: %+v %v", result, err)
				}
			}
			benchmarkResult = result
		})
	}
}

// Single-part SendText includes preparation, preflight and one submission. It
// does not exhaust multipart reference leases as benchmark iteration counts grow.
func BenchmarkSessionSendText(b *testing.B) {
	for _, tc := range []struct{ name, text string }{
		{"GSM7Single", strings.Repeat("A", 140)},
		{"UCS2FallbackSingle", strings.Repeat("中", 70)},
	} {
		b.Run(tc.name, func(b *testing.B) {
			s := benchmarkSessions(b)
			msg, policy, ctx := TextMessage{Text: tc.text}, message.GSMText(), b.Context()
			var result SendResult
			b.ReportAllocs()
			for b.Loop() {
				var err error
				result, err = s.SendText(ctx, msg, policy)
				if err != nil || len(result.Segments) != 1 || result.Segments[0].MessageID != "bench" {
					b.Fatalf("send: %+v %v", result, err)
				}
			}
			benchmarkSendResult = result
		})
	}
}

// This measures in-memory submission, not TCP/TLS or network TPS. Multipart
// creates a fresh application-owned reference domain per operation, including
// its allocation cost, so iterations cannot exhaust a long-lived lease space.
// Real applications share domains across sends and retain leases until expiry.
func BenchmarkSessionSendBinary(b *testing.B) {
	for _, tc := range []struct {
		name string
		size int
		mode message.Mode
	}{
		{"Single140", 140, message.UDH16},
		{"Payload1024", 1024, message.Payload},
		{"UDH16_1024NewDomain", 1024, message.UDH16},
	} {
		b.Run(tc.name, func(b *testing.B) {
			s := benchmarkSessions(b)
			msg := BinaryMessage{Data: make([]byte, tc.size), DataCoding: 4}
			policy := message.BinaryPolicy{Mode: tc.mode}
			ctx := b.Context()
			plan, err := message.PrepareBytes(pdu.V50, msg, policy)
			if err != nil {
				b.Fatal(err)
			}
			var result SendResult
			b.ReportAllocs()
			b.SetBytes(int64(tc.size))
			for b.Loop() {
				if plan.Multipart() {
					refs, e := message.NewReferences(16, 1, time.Hour)
					if e != nil {
						b.Fatal(e)
					}
					result, err = s.SendBinary(ctx, msg, policy, refs)
				} else {
					result, err = s.SendBinary(ctx, msg, policy)
				}
				if err != nil || len(result.Segments) != plan.Count() {
					b.Fatal(result, err)
				}
				for _, part := range result.Segments {
					if part.MessageID != "bench" {
						b.Fatal(part)
					}
				}
			}
			benchmarkSendResult = result
			b.ReportMetric(float64(plan.Count()), "segments/op")
		})
	}
}
