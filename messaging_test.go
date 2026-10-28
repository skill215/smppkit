package smpp

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/skill215/smppkit/message"
	"github.com/skill215/smppkit/pdu"
	"github.com/skill215/smppkit/textcodec"
)

func TestWindowOneCompleteTextBothDirections(t *testing.T) {
	client, server := sessionPair(t, Config{Window: 1})
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	r, e := message.NewReceiver(message.ReceiveConfig{AckPolicy: message.AckOnBuffered, Encodings: map[uint8]textcodec.Encoding{0: textcodec.GSM7, 8: textcodec.UCS2}})
	if e != nil {
		t.Fatal(e)
	}
	done := make(chan error, 1)
	go func() { done <- server.RunReceiver(ctx, r, "") }()
	text := strings.Repeat("Hello ^ ", 100)
	sent, e := client.SendText(ctx, TextMessage{Source: pdu.Address{Value: "alice"}, Destination: pdu.Address{Value: "bob"}, Text: text}, message.GSMText())
	if e != nil {
		t.Fatal(e)
	}
	if len(sent.Segments) < 2 {
		t.Fatal("not multipart")
	}
	for _, s := range sent.Segments {
		if s.Result.Outcome != ResponseReceived || s.MessageID == "" {
			t.Fatal(s)
		}
	}
	got, e := r.Receive(ctx)
	if e != nil || got.Text != text {
		t.Fatal(e)
	}
	// Server delivery uses the same planner and API.
	reverse, _ := message.NewReceiver(message.ReceiveConfig{AckPolicy: message.AckOnBuffered, Encodings: map[uint8]textcodec.Encoding{8: textcodec.UCS2}})
	reverseDone := make(chan error, 1)
	go func() { reverseDone <- client.RunReceiver(ctx, reverse, "") }()
	text = strings.Repeat("中文", 100)
	if _, e = server.SendText(ctx, TextMessage{Text: text}, message.GSMText()); e != nil {
		t.Fatal(e)
	}
	got, e = reverse.Receive(ctx)
	if e != nil || got.Text != text {
		t.Fatal(e)
	}
	cancel()
	<-done
	<-reverseDone
}
func TestMultipartPreservesPartialResults(t *testing.T) {
	client, server := sessionPair(t, Config{})
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		for i := range 2 {
			in, e := server.Receive(ctx)
			if e != nil {
				done <- e
				return
			}
			status := pdu.OK
			var response pdu.Body = &pdu.SubmitSMResp{MessageResponseFields: pdu.MessageResponseFields{MessageID: "first-accepted"}}
			if i == 1 {
				status = pdu.Throttled
				response = &pdu.Omitted{ID: pdu.SubmitSMRespID}
			}
			if e = in.Respond(ctx, status, response); e != nil {
				done <- e
				return
			}
		}
		done <- nil
	}()
	result, e := client.SendText(ctx, TextMessage{Text: strings.Repeat("A", 400)}, message.GSMText())
	if _, ok := errors.AsType[*PeerError](e); !ok || len(result.Segments) != 3 || result.Segments[0].MessageID != "first-accepted" || result.Segments[1].Result.Outcome != ResponseReceived || result.Segments[2].Result.Outcome != NotAttempted {
		t.Fatal(result, e)
	}
	if e = <-done; e != nil {
		t.Fatal(e)
	}
	accepted, rejected, unsent := result.Segments[0], result.Segments[1], result.Segments[2]
	if accepted.Result.Outcome != ResponseReceived || accepted.Err != nil || accepted.Result.BytesWritten == 0 || accepted.Result.Sequence != 2 {
		t.Fatalf("accepted result lost: %+v", accepted)
	}
	if peerErr, ok := errors.AsType[*PeerError](rejected.Err); !ok || peerErr.Status != pdu.Throttled || rejected.Result.Response.Status != pdu.Throttled || rejected.Result.BytesWritten == 0 || rejected.Result.Sequence != 3 {
		t.Fatalf("rejection lost: %+v", rejected)
	}
	if unsent.Result.Sequence != 0 || unsent.Result.BytesWritten != 0 || unsent.Err != nil || unsent.MessageID != "" {
		t.Fatalf("unsent result acquired progress: %+v", unsent)
	}
	client.mu.Lock()
	defer client.mu.Unlock()
	if client.seq != 3 || len(client.pending) != 0 || len(client.requests) != 0 {
		t.Fatal("rejected send retried or scheduled another part")
	}
}
func TestLateInvalidCharacterWritesNoSegment(t *testing.T) {
	client, server := sessionPair(t, Config{})
	_, e := client.SendText(t.Context(), TextMessage{Text: strings.Repeat("A", 1000) + "中"}, message.Policy{Encoding: textcodec.GSM7})
	if e == nil {
		t.Fatal("invalid character accepted")
	}
	server.mu.Lock()
	n := len(server.inbound)
	server.mu.Unlock()
	client.mu.Lock()
	seq := client.seq
	client.mu.Unlock()
	if n != 0 || seq != 1 {
		t.Fatal("transmission occurred before validation")
	}
}
