// SPDX-AI-Disclosure: ai-generated
// SPDX-AI-Model: GPT-6.0 Astra
// SPDX-AI-Provider: OpenAI
// SPDX-AI-Scope: Test code generated and revised with Codex under maintainer direction.
// SPDX-AI-Date: 2026-11-06

package smpp

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/skill215/smppkit/codec"
	"github.com/skill215/smppkit/message"
	"github.com/skill215/smppkit/pdu"
	"github.com/skill215/smppkit/tlv"
)

// Both inputs produce exactly three parts. Reuse the existing independent fault
// transport and peer-error tests to verify the shared send path's outcomes.
func multipartTestSenders() []struct {
	name string
	send func(*Session, context.Context) (SendResult, error)
} {
	return []struct {
		name string
		send func(*Session, context.Context) (SendResult, error)
	}{
		{"text", func(s *Session, ctx context.Context) (SendResult, error) {
			return s.SendText(ctx, TextMessage{Text: strings.Repeat("A", 400)}, message.GSMText())
		}},
		{"binary", func(s *Session, ctx context.Context) (SendResult, error) {
			return s.SendBinary(ctx, BinaryMessage{Data: bytes.Repeat([]byte{0, 255, 128}, 100), DataCoding: 4}, message.BinaryPolicy{})
		}},
	}
}

func TestBinaryBothDirectionsWindowOne(t *testing.T) {
	for _, mode := range []message.Mode{message.UDH8, message.UDH16, message.SAR, message.Payload} {
		for _, dcs := range []byte{2, 4} {
			t.Run(fmt.Sprintf("mode=%d/dcs=%d", mode, dcs), func(t *testing.T) {
				client, server := sessionPair(t, Config{Window: 1})
				ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
				defer cancel()
				data := bytes.Repeat([]byte{0, 255, 128}, 100)
				for _, pair := range [][2]*Session{{client, server}, {server, client}} {
					r, err := message.NewReceiver(message.ReceiveConfig{AckPolicy: message.AckOnBuffered})
					if err != nil {
						t.Fatal(err)
					}
					plan, err := message.PrepareBytes(pdu.V50, message.Binary{Data: data, DataCoding: dcs}, message.BinaryPolicy{Mode: mode})
					if err != nil {
						t.Fatal(err)
					}
					done := make(chan error, 1)
					go func() {
						for range plan.Count() {
							in, err := pair[1].Receive(ctx)
							if err == nil {
								err = AdmitMessage(ctx, r, in, "")
							}
							if err != nil {
								done <- err
								return
							}
						}
						done <- nil
					}()
					result, err := pair[0].SendBinary(ctx, BinaryMessage{Data: data, DataCoding: dcs}, message.BinaryPolicy{Mode: mode})
					if err != nil {
						t.Fatal(err)
					}
					if err = <-done; err != nil {
						t.Fatal(err)
					}
					if result.Encoding != "binary" || len(result.Segments) != plan.Count() {
						t.Fatal(result)
					}
					for _, part := range result.Segments {
						if part.Result.Outcome != ResponseReceived || part.Err != nil || !pair[0].server && part.MessageID == "" {
							t.Fatal(part)
						}
					}
					got, err := r.Receive(ctx)
					if err != nil || !bytes.Equal(got.Payload, data) || got.DataCoding != dcs || got.Decoded || got.DecodeError != nil {
						t.Fatal(got, err)
					}
				}
			})
		}
	}
}

func TestBinaryPreflightBeforeLeaseAndWrite(t *testing.T) {
	for _, name := range []string{"invalid-dcs", "33-multi", "33-tlv", "33-udh", "invalid-address", "invalid-tlv", "no-negotiation", "role", "canceled", "wrong-width", "nil-refs", "extra-refs", "frame-limit"} {
		t.Run(name, func(t *testing.T) {
			v := pdu.V50
			if strings.HasPrefix(name, "33-") {
				v = pdu.V33
			}
			c, _ := codec.New(codec.Config{Version: v})
			a, b := net.Pipe()
			conn := &r1PreflightConn{Conn: a}
			s, err := newSession(conn, c, Config{}, false, Bound, Transmitter)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { s.Close(); b.Close(); s.Wait() }()
			refs, _ := message.NewReferences(16, 1, time.Hour)
			provided := []*message.References{refs}
			msg := BinaryMessage{Data: make([]byte, 141), DataCoding: 4}
			policy := message.BinaryPolicy{}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			want := message.ErrMetadata
			switch name {
			case "invalid-dcs":
				msg.DataCoding = 0
			case "33-tlv":
				msg.Data = nil
				msg.Optional = tlv.Fields{{Tag: 0x1400}}
			case "33-udh":
				msg.Data = nil
				policy.AdditionalUDH = []message.InformationElement{{ID: 5, Data: []byte{1, 2, 3, 4}}}
			case "invalid-address":
				msg.Source.TON = 7
				want = codec.ErrValidation
			case "invalid-tlv":
				msg.Optional = tlv.Fields{{Tag: tlv.SourcePort, Value: []byte{0}}}
			case "no-negotiation":
				policy.Mode = message.SAR
				s.mu.Lock()
				s.tlvAllowed = false
				s.mu.Unlock()
				want = ErrState
			case "role":
				s.mu.Lock()
				s.role = Receiver
				s.mu.Unlock()
				want = ErrState
			case "canceled":
				cancel()
				want = context.Canceled
			case "wrong-width":
				policy.Mode = message.UDH8
			case "nil-refs":
				provided[0] = nil
			case "extra-refs":
				provided = append(provided, refs)
			case "frame-limit":
				policy.Mode = message.Payload
				msg.Data = make([]byte, 65535)
				want = codec.ErrValidation
			}
			got, err := s.SendBinary(ctx, msg, policy, provided...)
			if !errors.Is(err, want) {
				t.Fatalf("got %v want %v", err, want)
			}
			for _, part := range got.Segments {
				if part.Result.Outcome != NotAttempted || part.Result.Sequence != 0 || part.Result.BytesWritten != 0 || part.Err != nil {
					t.Fatal(part)
				}
			}
			if _, err = refs.Reserve(time.Now()); err != nil {
				t.Fatal("preflight consumed lease", err)
			}
			s.Close()
			s.Wait()
			if conn.writes.Load() != 0 {
				t.Fatal("preflight wrote transport")
			}
		})
	}
}

func TestBinaryAndTextShareReferencesAcrossSessions(t *testing.T) {
	client, server := sessionPair(t, Config{})
	other, _ := sessionPair(t, Config{})
	refs, _ := message.NewReferences(16, 1, time.Hour)
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		for range 2 {
			in, err := server.Receive(ctx)
			if err == nil {
				err = in.Respond(ctx, pdu.OK, &pdu.SubmitSMResp{})
			}
			if err != nil {
				done <- err
				return
			}
		}
		done <- nil
	}()
	if _, err := client.SendBinary(ctx, BinaryMessage{Data: make([]byte, 141), DataCoding: 4}, message.BinaryPolicy{}, refs); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	// ACK success retains the binary lease. Text on another connection sees the
	// same occupied sending domain, and so does another binary send.
	for _, binary := range []bool{false, true} {
		var got SendResult
		var err error
		if binary {
			got, err = other.SendBinary(ctx, BinaryMessage{Data: make([]byte, 141), DataCoding: 4}, message.BinaryPolicy{}, refs)
		} else {
			got, err = other.SendText(ctx, TextMessage{Text: strings.Repeat("A", 200)}, message.GSMText(), refs)
		}
		if !errors.Is(err, message.ErrCapacity) || len(got.Segments) != 2 {
			t.Fatal(got, err)
		}
		for _, part := range got.Segments {
			if part.Result.Outcome != NotAttempted || part.Result.Sequence != 0 {
				t.Fatal(part)
			}
		}
	}
}

func TestConcurrentBinarySendsSharedReferences(t *testing.T) {
	client, server := sessionPair(t, Config{Window: 1})
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	r, _ := message.NewReceiver(message.ReceiveConfig{AckPolicy: message.AckOnBuffered})
	done := make(chan error, 1)
	go func() { done <- server.RunReceiver(ctx, r, "") }()
	defer func() { cancel(); <-done }()
	refs, _ := message.NewReferences(16, 8, time.Hour)
	var wg sync.WaitGroup
	for id := range 8 {
		wg.Go(func() {
			result, err := client.SendBinary(ctx, BinaryMessage{Data: bytes.Repeat([]byte{byte(id)}, 141), DataCoding: 4}, message.BinaryPolicy{}, refs)
			if err != nil || len(result.Segments) != 2 {
				t.Error(result, err)
			}
		})
	}
	wg.Wait()
	seen := make(map[byte]bool)
	for range 8 {
		got, err := r.Receive(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if len(got.Payload) != 141 || !bytes.Equal(got.Payload, bytes.Repeat(got.Payload[:1], 141)) || seen[got.Payload[0]] {
			t.Fatal("mixed or duplicate data")
		}
		seen[got.Payload[0]] = true
	}
	if _, err := refs.Reserve(time.Now()); !errors.Is(err, message.ErrCapacity) {
		t.Fatal("leases released early", err)
	}
}

func TestBinarySingleAcrossVersions(t *testing.T) {
	for _, v := range []pdu.Version{pdu.V33, pdu.V34, pdu.V50} {
		t.Run(fmt.Sprint(v), func(t *testing.T) {
			c, _ := codec.New(codec.Config{Version: v})
			a, b := net.Pipe()
			s, err := newSession(a, c, Config{}, false, Bound, Transmitter)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { s.Close(); b.Close(); s.Wait() }()
			ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
			defer cancel()
			done := make(chan error, 1)
			go func() {
				request, e := c.Read(b)
				if e == nil {
					sm, ok := request.Body.(*pdu.SubmitSM)
					if !ok || sm.DataCoding != 4 || sm.ESMClass != 0 || len(sm.Optional) != 0 || !bytes.Equal(sm.ShortMessage, []byte{0, 255, 128}) {
						e = errors.New("bad binary single")
					}
				}
				if e == nil {
					_, e = b.Write([]byte{0, 0, 0, 19, 0x80, 0, 0, 4, 0, 0, 0, 0, 0, 0, 0, 1, 'o', 'k', 0})
				}
				done <- e
			}()
			// A single-part plan uses no allocator, even with an explicit nil override.
			got, err := s.SendBinary(ctx, BinaryMessage{Data: []byte{0, 255, 128}, DataCoding: 4}, message.BinaryPolicy{}, nil)
			if err != nil || len(got.Segments) != 1 || got.Segments[0].MessageID != "ok" {
				t.Fatal(got, err)
			}
			if err = <-done; err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestBinaryPreflightsEverySegment(t *testing.T) {
	// A deliberately stateful test callback fails only on the second part. This
	// is a test probe, not a recommended production validator: validation may
	// occur multiple times during preflight and Request.
	calls := 0
	failure := errors.New("second preflight")
	c, err := codec.New(codec.Config{Version: pdu.V50, TLVs: []codec.TLVDefinition{{Tag: 0x1400, MinLength: 1, MaxLength: 1, Commands: []pdu.CommandID{pdu.SubmitSMID}, Versions: []pdu.Version{pdu.V50}, Validate: func([]byte) error {
		calls++
		if calls == 2 {
			return failure
		}
		return nil
	}}}})
	if err != nil {
		t.Fatal(err)
	}
	a, b := net.Pipe()
	conn := &r1PreflightConn{Conn: a}
	s, err := newSession(conn, c, Config{}, false, Bound, Transmitter)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { s.Close(); b.Close(); s.Wait() }()
	refs, _ := message.NewReferences(16, 1, time.Hour)
	result, err := s.SendBinary(t.Context(), BinaryMessage{Data: make([]byte, 300), DataCoding: 4, Optional: tlv.Fields{{Tag: 0x1400, Value: []byte{1}}}}, message.BinaryPolicy{}, refs)
	if !errors.Is(err, failure) || calls != 2 || len(result.Segments) != 3 {
		t.Fatal(result, err, calls)
	}
	for _, part := range result.Segments {
		if part.Result.Outcome != NotAttempted || part.Err != nil {
			t.Fatal(part)
		}
	}
	if _, err = refs.Reserve(time.Now()); err != nil {
		t.Fatal("lease consumed", err)
	}
	s.Close()
	s.Wait()
	if conn.writes.Load() != 0 {
		t.Fatal("preflight wrote")
	}
}
