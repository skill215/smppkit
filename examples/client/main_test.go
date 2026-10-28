// SPDX-AI-Disclosure: ai-generated
// SPDX-AI-Model: GPT-6.0 Astra
// SPDX-AI-Provider: OpenAI
// SPDX-AI-Scope: Test code generated and revised with Codex under maintainer direction.
// SPDX-AI-Date: 2026-10-28

package main

import (
	"context"
	"net"
	"testing"
	"time"

	smpp "github.com/skill215/smppkit"
	"github.com/skill215/smppkit/codec"
	"github.com/skill215/smppkit/message"
	"github.com/skill215/smppkit/pdu"
)

// A ready delivery must not postpone expiry until an idle poll. This checks the
// example's composition of existing public APIs, not another protocol fixture.
func TestReadDeliveriesSweepsBeforeBusyReceive(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	c, err := codec.New(codec.Config{Version: pdu.V50})
	if err != nil {
		t.Fatal(err)
	}
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	client, err := smpp.NewSession(a, c, smpp.Config{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { client.Close(); client.Wait() }()
	srv, err := smpp.NewServer(smpp.ServerConfig{Profiles: []*codec.Codec{c}, Authenticate: func(context.Context, smpp.BindInfo) (smpp.BindDecision, error) { return smpp.BindDecision{}, nil }})
	if err != nil {
		t.Fatal(err)
	}
	type accepted struct {
		session *smpp.Session
		err     error
	}
	ready := make(chan accepted, 1)
	go func() { s, err := srv.Accept(ctx, b); ready <- accepted{s, err} }()
	_, bindErr := client.Bind(ctx, &pdu.BindTransceiver{BindFields: pdu.BindFields{InterfaceVersion: pdu.V50}})
	bound := <-ready
	if bound.session != nil {
		defer func() { bound.session.Close(); bound.session.Wait() }()
	}
	if bindErr != nil || bound.err != nil {
		t.Fatal(bindErr, bound.err)
	}
	r, err := message.NewReceiver(message.ReceiveConfig{AckPolicy: message.AckOnBuffered, Lifetime: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	_, err = r.Admit("old", pdu.SMFields{ESMClass: 0x40, ShortMessage: []byte{5, 0, 3, 7, 2, 1, 'A'}}, time.Now().Add(-2*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	// Queue a live delivery before starting the example reader. It must sweep
	// before accepting this ready input, without waiting one second for idle.
	sent := make(chan error, 1)
	go func() {
		_, err := bound.session.Request(ctx, &pdu.DeliverSM{SMFields: pdu.SMFields{ShortMessage: []byte("live")}})
		sent <- err
	}()
	deliveries := make(chan message.Message)
	done := make(chan error, 1)
	go func() { done <- readDeliveries(ctx, client, r, deliveries) }()
	defer func() { cancel(); client.Close(); <-done }()
	select {
	case msg := <-deliveries:
		if string(msg.Payload) != "live" {
			t.Fatal(msg)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if err := <-sent; err != nil {
		t.Fatal(err)
	}
	if stats := r.Stats(); stats.Groups != 0 || stats.Events != 0 || stats.BufferedBytes != 0 {
		t.Fatal("busy receive skipped expiry/event drain", stats)
	}
}
