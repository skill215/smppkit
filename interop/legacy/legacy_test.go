// SPDX-AI-Disclosure: ai-generated
// SPDX-AI-Model: GPT-6.0 Astra
// SPDX-AI-Provider: OpenAI
// SPDX-AI-Scope: Test code generated and revised with Codex under maintainer direction.
// SPDX-AI-Date: 2026-10-28

//go:build legacyinterop

// This optional integration harness runs in a temporary module; the legacy
// stack is only a peer dependency, never part of smppkit's module/runtime.
package legacyinterop

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	oldpdu "github.com/skill215/go-smpp/smpp/pdu"
	"github.com/skill215/go-smpp/smpp/pdu/pdufield"
	"github.com/skill215/go-smpp/smpp/smpptest"
	smpp "github.com/skill215/smppkit"
	"github.com/skill215/smppkit/codec"
	"github.com/skill215/smppkit/message"
	"github.com/skill215/smppkit/pdu"
	"github.com/skill215/smppkit/textcodec"
)

// The unmodified simulator allocates bind_resp from a process-global counter
// instead of echoing the request sequence. A fresh process's first PDU happens
// to match smppkit's first bind (1). Make that limitation explicit and isolate
// each run; do not rewrite peer bytes or relax Session correlation to hide it.
func TestLegacySimulator(t *testing.T) {
	if os.Getenv("SMPPKIT_LEGACY_CHILD") != "1" {
		ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestLegacySimulator$", "-test.v")
		cmd.Env = append(os.Environ(), "SMPPKIT_LEGACY_CHILD=1")
		out, err := cmd.CombinedOutput()
		t.Logf("fresh simulator process:\n%s", out)
		if err != nil {
			t.Fatal(err)
		}
		return
	}
	ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
	defer cancel()
	srv := smpptest.NewUnstartedServer()
	defer srv.Close()
	peerConn := make(chan smpptest.Conn, 1)
	submitted := make(chan []byte, 8)
	acks := make(chan uint32, 4)
	peerErrors := make(chan error, 16)
	var submissions atomic.Int32
	// Handler is a supported simulator hook. Explicit responses replace its
	// default request echo; all encoding/decoding still belongs to old go-smpp.
	srv.Handler = func(c smpptest.Conn, m oldpdu.Body) {
		var response oldpdu.Body
		switch m.Header().ID {
		case oldpdu.SubmitSMID:
			n := submissions.Add(1)
			if n == 1 {
				peerConn <- c
			}
			payload := m.Fields()[pdufield.ShortMessage].Bytes()
			submitted <- append([]byte(nil), payload...)
			if string(payload) == "timeout" {
				return
			}
			response = oldpdu.NewSubmitSMResp()
			if err := response.Fields().Set(pdufield.MessageID, fmt.Sprintf("old-%d", n)); err != nil {
				peerErrors <- err
				return
			}
			if string(payload) == "reject" {
				response.Header().Status = 0x58
			}
		case oldpdu.DeliverSMRespID:
			if m.Header().Status != 0 {
				peerErrors <- fmt.Errorf("delivery ACK status %x", m.Header().Status)
			}
			acks <- m.Header().Seq
			return
		case oldpdu.EnquireLinkID:
			response = oldpdu.NewEnquireLinkResp()
		case oldpdu.UnbindID:
			response = oldpdu.NewUnbindResp()
		default:
			peerErrors <- fmt.Errorf("unexpected command %x", m.Header().ID)
			return
		}
		response.Header().Seq = m.Header().Seq
		if err := c.Write(response); err != nil {
			peerErrors <- err
		}
	}
	srv.Start()
	c, err := codec.New(codec.Config{Version: pdu.V34})
	if err != nil {
		t.Fatal(err)
	}
	s, err := smpp.Dial(ctx, srv.Addr(), smpp.ClientConfig{Codec: c, Session: smpp.Config{Window: 1}, Bind: &pdu.BindTransceiver{BindFields: pdu.BindFields{SystemID: "client", Password: "secret", InterfaceVersion: pdu.V34}}})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { s.Close(); s.Wait() }()
	if s.TLVEnabled() {
		t.Fatal("legacy bind has no sc_interface_version; TLVs must stay disabled")
	}
	result, err := s.SendText(ctx, smpp.TextMessage{Source: pdu.Address{Value: "100"}, Destination: pdu.Address{Value: "200"}, Text: strings.Repeat("A", 301)}, message.GSMText())
	if err != nil || len(result.Segments) != 2 {
		t.Fatal(result, err)
	}
	var joined []byte
	var reference []byte
	for i, part := range result.Segments {
		if part.Result.Outcome != smpp.ResponseReceived || part.MessageID != fmt.Sprintf("old-%d", i+1) {
			t.Fatal(part)
		}
		var raw []byte
		select {
		case raw = <-submitted:
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
		// Independently inspect the actual received UDH16 bytes, without using
		// smppkit's planner/parser on the simulator side.
		if len(raw) < 7 || !bytes.Equal(raw[:3], []byte{6, 8, 4}) || raw[5] != 2 || raw[6] != byte(i+1) {
			t.Fatalf("UDH16: %x", raw)
		}
		if i == 0 {
			reference = append([]byte(nil), raw[3:5]...)
		}
		if !bytes.Equal(raw[3:5], reference) {
			t.Fatal("reference changed between parts")
		}
		want := 152
		if i == 1 {
			want = 149
		}
		if len(raw[7:]) != want {
			t.Fatal("wrong segment boundary", len(raw[7:]))
		}
		joined = append(joined, raw[7:]...)
	}
	if string(joined) != strings.Repeat("A", 301) {
		t.Fatal("submitted content changed")
	}
	var conn smpptest.Conn
	select {
	case conn = <-peerConn:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	defer conn.Close() // old Server.Close only closes the listener
	r, err := message.NewReceiver(message.ReceiveConfig{AckPolicy: message.AckOnBuffered, Encodings: map[uint8]textcodec.Encoding{0: textcodec.GSM7}})
	if err != nil {
		t.Fatal(err)
	}
	// Send reversed UDH8 segments with one identical duplicate. Wait for every
	// ACK before sending the next, proving progress at peer window one.
	for i, number := range []byte{2, 2, 1} {
		body := "world"
		if number == 1 {
			body = "hello "
		}
		delivery := oldpdu.NewDeliverSM()
		delivery.Header().Seq = uint32(41 + i)
		for name, value := range map[pdufield.Name]interface{}{pdufield.SourceAddr: "200", pdufield.DestinationAddr: "100", pdufield.ESMClass: 0x40, pdufield.DataCoding: 0, pdufield.ShortMessage: append([]byte{5, 0, 3, 42, 2, number}, []byte(body)...)} {
			if err := delivery.Fields().Set(name, value); err != nil {
				t.Fatal(err)
			}
		}
		if err := conn.Write(delivery); err != nil {
			t.Fatal(err)
		}
		in, err := s.Receive(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if err = smpp.AdmitMessage(ctx, r, in, "legacy-demo"); err != nil {
			t.Fatal(err)
		}
		select {
		case seq := <-acks:
			if seq != uint32(41+i) {
				t.Fatal("wrong ACK sequence", seq)
			}
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
		if i < 2 && r.Stats().Queued != 0 {
			t.Fatal("incomplete message queued")
		}
	}
	m, err := r.Receive(ctx)
	if err != nil || m.Text != "hello world" || len(m.Segments) != 2 || r.Stats().Queued != 0 {
		t.Fatal(m, err)
	}
	negative, err := s.Request(ctx, &pdu.SubmitSM{SMFields: pdu.SMFields{ShortMessage: []byte("reject")}})
	var peerErr *smpp.PeerError
	if !errors.As(err, &peerErr) || negative.Outcome != smpp.ResponseReceived || negative.Response.Status != pdu.Throttled {
		t.Fatal(negative, err)
	}
	short, stop := context.WithTimeout(ctx, 80*time.Millisecond)
	uncertain, err := s.Request(short, &pdu.SubmitSM{SMFields: pdu.SMFields{ShortMessage: []byte("timeout")}})
	stop()
	if !errors.Is(err, context.DeadlineExceeded) || uncertain.Outcome != smpp.Written {
		t.Fatal(uncertain, err)
	}
	if _, err := s.Request(ctx, &pdu.EnquireLink{}); err != nil {
		t.Fatal(err)
	}
	if submissions.Load() != 4 {
		t.Fatal("unexpected replay", submissions.Load())
	}
	if err := s.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	s.Wait()
	select {
	case err := <-peerErrors:
		t.Fatal(err)
	default:
	}
	t.Log("3.4 TRX: UDH16 submit, UDH8 reversed/duplicate delivery and per-segment ACK, negative/uncertain results, enquire, unbind PASS; TLVs disabled")
}

// Record the simulator's bind limitation as an expected refusal, not as a
// compatibility workaround in smppkit. Consuming a legacy sequence before
// authentication guarantees bind_resp does not echo our first bind's sequence.
func TestLegacyBindSequenceMismatch(t *testing.T) {
	_ = oldpdu.NewEnquireLink()
	srv := smpptest.NewUnstartedServer()
	defer srv.Close()
	srv.Start()
	c, err := codec.New(codec.Config{Version: pdu.V34})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	s, err := smpp.Dial(ctx, srv.Addr(), smpp.ClientConfig{Codec: c, Session: smpp.Config{RequestTimeout: 80 * time.Millisecond}, Bind: &pdu.BindTransmitter{BindFields: pdu.BindFields{SystemID: "client", Password: "secret", InterfaceVersion: pdu.V34}}})
	if s != nil {
		s.Close()
		s.Wait()
		t.Fatal("mismatched bind response was accepted")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("expected bounded bind failure", err)
	}
	t.Log("legacy bind_resp sequence mismatch correctly remains uncorrelated and times out")
}
