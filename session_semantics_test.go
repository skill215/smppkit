// SPDX-AI-Disclosure: ai-generated
// SPDX-AI-Model: GPT-6.0 Astra
// SPDX-AI-Provider: OpenAI
// SPDX-AI-Scope: Test code generated and revised with Codex under maintainer direction.
// SPDX-AI-Date: 2026-10-28

package smpp

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/skill215/smppkit/codec"
	"github.com/skill215/smppkit/pdu"
)

func TestInvalidFieldsDoNotWriteOrConsumeSequence(t *testing.T) {
	client, server := sessionPair(t, Config{Window: 1})
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	for _, body := range []pdu.Body{
		&pdu.SubmitSM{SMFields: pdu.SMFields{ScheduleDeliveryTime: "230229000000000+"}},
		&pdu.SubmitSM{SMFields: pdu.SMFields{Source: pdu.Address{TON: 7}}},
		&pdu.DataSM{RegisteredDelivery: 0x80},
		&pdu.ReplaceSM{ValidityPeriod: "tomorrow"},
	} {
		result, err := client.Request(ctx, body)
		if !errors.Is(err, codec.ErrValidation) || result.BytesWritten != 0 || result.Sequence != 0 || result.Outcome != NotWritten {
			t.Fatal(result, err)
		}
	}
	done := make(chan error, 1)
	go func() {
		in, err := server.Receive(ctx)
		if err != nil {
			done <- err
			return
		}
		if in.PDU.Sequence != 2 {
			done <- errors.New("invalid request reached transport or consumed sequence")
			return
		}
		done <- in.Respond(ctx, pdu.OK, &pdu.SubmitSMResp{})
	}()
	if _, err := client.Request(ctx, &pdu.SubmitSM{}); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestBindSemanticsPrecedeAuthentication(t *testing.T) {
	for _, rawHex := range []string{
		"0000001700000002000000000000000100000050070000",   // TON 7
		"0000001800000002000000000000000100ff000050000000", // non-ASCII password
	} {
		var calls atomic.Int32
		srv, err := NewServer(ServerConfig{Profiles: []*codec.Codec{testCodec(t)}, Authenticate: func(context.Context, BindInfo) (BindDecision, error) { calls.Add(1); return BindDecision{}, nil }})
		if err != nil {
			t.Fatal(err)
		}
		a, b := net.Pipe()
		ctx, cancel := context.WithTimeout(t.Context(), time.Second)
		raw, err := hex.DecodeString(rawHex)
		if err != nil {
			t.Fatal(err)
		}
		done := make(chan error, 1)
		go func() { _, err := a.Write(raw); a.Close(); done <- err }()
		s, err := srv.Accept(ctx, b)
		cancel()
		a.Close()
		b.Close()
		if s != nil {
			s.Close()
			s.Wait()
			t.Fatal("invalid bind accepted")
		}
		if err == nil || calls.Load() != 0 {
			t.Fatal("authentication ran for malformed bind", err, calls.Load())
		}
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
}

func TestResponseSemanticsLeaveHandleCorrectable(t *testing.T) {
	client, server := sessionPair(t, Config{})
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		in, err := client.Receive(ctx)
		if err != nil {
			done <- err
			return
		}
		err = in.Respond(ctx, pdu.OK, &pdu.DeliverSMResp{MessageResponseFields: pdu.MessageResponseFields{MessageID: "unused"}})
		if !errors.Is(err, codec.ErrValidation) {
			done <- errors.New("invalid deliver response accepted")
			return
		}
		done <- in.Respond(ctx, pdu.OK, &pdu.DeliverSMResp{})
	}()
	r, err := server.Request(ctx, &pdu.DeliverSM{})
	if err != nil || r.Outcome != ResponseReceived {
		t.Fatal(r, err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestMalformedResponseFieldsAreNotSuccessfulResults(t *testing.T) {
	a, b := net.Pipe()
	s, err := newSession(a, testCodec(t), Config{}, false, Bound, Transmitter)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close(); b.Close(); s.Wait() })
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		// Read the request but write a literal 5.0 query response with state 10,
		// beyond the standard 0..9 range. No production encoder can create it.
		_, err := s.codec.Read(b)
		if err == nil {
			raw, _ := hex.DecodeString("0000001480000003000000000000000100000a00")
			_, err = bytes.NewReader(raw).WriteTo(b)
		}
		done <- err
	}()
	r, err := s.Request(ctx, &pdu.QuerySM{})
	if !errors.Is(err, codec.ErrValidation) || r.Outcome == ResponseReceived {
		t.Fatal(r, err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
