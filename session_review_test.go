// SPDX-AI-Disclosure: ai-generated
// SPDX-AI-Model: GPT-6.0 Astra
// SPDX-AI-Provider: OpenAI
// SPDX-AI-Scope: Test code generated and revised with Codex under maintainer direction.
// SPDX-AI-Date: 2026-10-28

package smpp

import (
	"context"
	"errors"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/skill215/smppkit/codec"
	"github.com/skill215/smppkit/message"
	"github.com/skill215/smppkit/pdu"
)

// Pause after the underlying Read supplied the final frame octet, modelling
// preemption between transport completion and protocol-state dispatch.
type pauseFrameRead struct {
	net.Conn
	remaining     int
	ready, resume chan struct{}
}

func (c *pauseFrameRead) Read(b []byte) (int, error) {
	n, err := c.Conn.Read(b)
	c.remaining -= n
	if err == nil && c.remaining == 0 {
		close(c.ready)
		<-c.resume
	}
	return n, err
}
func TestReviewCloseIsTerminalForLateBindResponse(t *testing.T) {
	a, b := net.Pipe()
	defer b.Close()
	wire := []byte{0, 0, 0, 17, 0x80, 0, 0, 9, 0, 0, 0, 0, 0, 0, 0, 1, 0}
	conn := &pauseFrameRead{Conn: a, remaining: len(wire), ready: make(chan struct{}), resume: make(chan struct{})}
	s, err := newSession(conn, testCodec(t), Config{}, false, Binding, Transceiver)
	if err != nil {
		t.Fatal(err)
	}
	// A pending bind may remain until its writer has been joined by Request.
	// Retain it explicitly to make this transport/state race deterministic.
	s.mu.Lock()
	s.pending[1] = &pending{expected: pdu.BindTransceiverRespID, request: pdu.BindTransceiverID, response: make(chan pdu.PDU, 1)}
	s.mu.Unlock()
	go func() { _, _ = b.Write(wire) }()
	<-conn.ready
	s.Close()
	close(conn.resume)
	s.Wait()
	if s.State() != Closed {
		t.Fatalf("closed session resurrected as state %d", s.State())
	}
	s.Close() // closing again must remain safe
}

func TestReviewInboundResponseIdentitySurvivesPayloadEdits(t *testing.T) {
	client, server := sessionPair(t, Config{})
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		in, err := server.Receive(ctx)
		if err == nil {
			in.PDU.Sequence += 123
			in.PDU.Body = &pdu.DeliverSM{}
			err = in.Respond(ctx, pdu.OK, &pdu.SubmitSMResp{})
		}
		done <- err
	}()
	result, err := client.Request(ctx, &pdu.SubmitSM{})
	if err != nil || result.Outcome != ResponseReceived {
		t.Fatalf("response handle lost original identity: %+v %v", result, err)
	}
	if err = <-done; err != nil {
		t.Fatal(err)
	}
}

type delayedEncode struct {
	started, release chan struct{}
	once             sync.Once
}

func (c *delayedEncode) DecodeBody(b []byte) (pdu.Body, error) {
	return &pdu.Opaque{ID: 0x10200, Data: append([]byte(nil), b...)}, nil
}
func (c *delayedEncode) AppendBody(dst []byte, b pdu.Body) ([]byte, error) {
	c.once.Do(func() { close(c.started) })
	<-c.release
	return append(dst, 1), nil
}
func TestReviewCancelDuringEncodingDoesNotQueueOrWrite(t *testing.T) {
	ext := &delayedEncode{started: make(chan struct{}), release: make(chan struct{})}
	c, err := codec.New(codec.Config{Version: pdu.V50, Commands: []codec.CommandDefinition{{Request: 0x10200, Response: 0x80010200, Versions: []pdu.Version{pdu.V50}, RequestCodec: ext, ResponseCodec: ext}}})
	if err != nil {
		t.Fatal(err)
	}
	a, b := net.Pipe()
	defer b.Close()
	s, err := newSession(a, c, Config{}, false, Bound, Transceiver)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { s.Close(); s.Wait() }()
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan Result, 1)
	go func() {
		r, e := s.Request(ctx, &pdu.Opaque{ID: 0x10200})
		if !errors.Is(e, context.Canceled) {
			t.Errorf("error %v", e)
		}
		done <- r
	}()
	<-ext.started
	cancel()
	close(ext.release)
	r := <-done
	if r.Sequence != 0 || r.Outcome != NotWritten || s.State() != Bound {
		t.Fatalf("cancelled encoding changed protocol state or consumed a sequence: %+v state=%d", r, s.State())
	}
}

func TestReviewInvalidTextRequestDoesNotConsumeReference(t *testing.T) {
	client, _ := sessionPair(t, Config{})
	refs, err := message.NewReferences(8, 1, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	policy := message.GSMText()
	policy.Mode = message.UDH8
	result, err := client.SendText(t.Context(), TextMessage{Source: pdu.Address{Value: strings.Repeat("s", 21)}, Text: strings.Repeat("A", 200)}, policy, refs)
	if err == nil {
		t.Fatal("invalid source accepted")
	}
	if len(result.Segments) != 2 {
		t.Fatalf("preflight lost the prepared plan: %+v", result)
	}
	for i, part := range result.Segments {
		if part.Number != i+1 || part.Result.Outcome != NotAttempted || part.Result.Sequence != 0 || part.Result.BytesWritten != 0 || part.Err != nil {
			t.Fatalf("preflight attempted a part: %+v", part)
		}
	}
	client.mu.Lock()
	admitted := client.seq != 1 || len(client.pending) != 0 || len(client.requests) != 0
	client.mu.Unlock()
	if admitted {
		t.Fatal("preflight entered Request")
	}
	if _, err = refs.Reserve(time.Now()); err != nil {
		t.Fatalf("local validation failure exhausted the reference space: %v", err)
	}
}
