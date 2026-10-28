package smpp

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/skill215/smppkit/message"
	"github.com/skill215/smppkit/pdu"
)

func TestAdmitMessageACKCommitBoundary(t *testing.T) {
	t.Run("write-failure-keeps-admission", func(t *testing.T) {
		a, peer := net.Pipe()
		defer peer.Close()
		r, err := message.NewReceiver(message.ReceiveConfig{AckPolicy: message.AckOnBuffered})
		if err != nil {
			t.Fatal(err)
		}
		failure := errors.New("ACK write failed")
		observed := make(chan message.Stats, 1)
		conn := &faultConn{Conn: a, write: func([]byte) (int, error) {
			// Observe exactly at the first transport write, not after AdmitMessage
			// returns: an implementation that ACKs before admission must fail.
			observed <- r.Stats()
			return 0, failure
		}}
		s, err := newSession(conn, testCodec(t), Config{}, true, Bound, Transceiver)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { s.Close(); s.Wait() }()
		ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
		defer cancel()
		raw := r1Wire(rv21Data)
		raw[23] = 0 // configured non-transaction default
		in := rv21Inbound(t, s, peer, ctx, raw)
		if err = AdmitMessage(ctx, r, in, ""); !errors.Is(err, failure) {
			t.Fatal(err)
		}
		if stats := <-observed; stats.Queued != 1 {
			t.Fatalf("ACK preceded admission: %+v", stats)
		}
		msg, err := r.Receive(ctx)
		if err != nil || !bytes.Equal(msg.Payload, []byte("Hi")) || r.Stats().BufferedBytes != 0 {
			t.Fatal(msg, err, r.Stats())
		}
	})
	t.Run("slow-consumer-rejects-before-OK", func(t *testing.T) {
		s, peer, ctx := rv21Session(t, pdu.V50, true)
		r, err := message.NewReceiver(message.ReceiveConfig{AckPolicy: message.AckOnBuffered, QueueSize: 1})
		if err != nil {
			t.Fatal(err)
		}
		if _, err = r.Admit("occupied", pdu.SMFields{ShortMessage: []byte("first")}, time.Now()); err != nil {
			t.Fatal(err)
		}
		before := r.Stats()
		raw := r1Wire(rv21Data)
		raw[23] = 0
		in := rv21Inbound(t, s, peer, ctx, raw)
		ack := make(chan error, 1)
		go func() { ack <- rv21ReadResponse(peer, pdu.DataSMRespID, pdu.Throttled) }()
		if err = AdmitMessage(ctx, r, in, ""); !errors.Is(err, message.ErrCapacity) {
			t.Fatal(err)
		}
		if err = <-ack; err != nil {
			t.Fatal(err)
		}
		if r.Stats() != before {
			t.Fatal("failed admission mutated receiver")
		}
		if _, err = r.Receive(ctx); err != nil {
			t.Fatal(err)
		}
		// Application-controlled new request after draining, not an automatic retry.
		raw[15] = 2
		in = rv21Inbound(t, s, peer, ctx, raw)
		go func() {
			p, err := s.codec.Read(peer)
			if err == nil && (p.Sequence != 2 || p.Status != pdu.OK) {
				err = errors.New("bad recovery ACK")
			}
			ack <- err
		}()
		if err = AdmitMessage(ctx, r, in, ""); err != nil {
			t.Fatal(err)
		}
		if err = <-ack; err != nil {
			t.Fatal(err)
		}
		if r.Stats().Queued != 1 {
			t.Fatal("capacity did not recover")
		}
	})
}

func TestAdmitMessageCrossSessionNamespace(t *testing.T) {
	for _, namespace := range []string{"", "shared-tenant"} {
		t.Run("namespace="+namespace, func(t *testing.T) {
			r, err := message.NewReceiver(message.ReceiveConfig{AckPolicy: message.AckOnBuffered})
			if err != nil {
				t.Fatal(err)
			}
			for part := byte(1); part <= 2; part++ {
				s, peer, ctx := rv21Session(t, pdu.V50, true)
				// Independent empty-address submit_sm mandatory fields followed by
				// UDH8: UDHL=5, IEI=0, length=3, ref=7, total=2, part=1/2.
				raw := append(r1Wire(rv21Submit)[:33], 5, 0, 3, 7, 2, part, 'A'+part-1)
				raw[3], raw[23], raw[32] = byte(len(raw)), 0x40, 7
				in := rv21Inbound(t, s, peer, ctx, raw)
				ack := make(chan error, 1)
				go func() { ack <- rv21ReadResponse(peer, pdu.SubmitSMRespID, pdu.OK) }()
				if err = AdmitMessage(ctx, r, in, namespace); err != nil {
					t.Fatal(err)
				}
				if err = <-ack; err != nil {
					t.Fatal(err)
				}
			}
			if namespace == "" {
				if r.Stats().Groups != 2 || r.Stats().Queued != 0 {
					t.Fatal("default namespaces merged across sessions", r.Stats())
				}
			} else {
				if r.Stats().Groups != 0 || r.Stats().Queued != 1 {
					t.Fatal("explicit namespace failed to merge", r.Stats())
				}
				msg, err := r.Receive(t.Context())
				if err != nil || msg.Namespace != namespace || string(msg.Payload) != "AB" {
					t.Fatal(msg, err)
				}
			}
		})
	}
}

// Exercise the high-level result assembly, not another copy of Request's fault
// tests. The first part is accepted by an independent 19-byte submit_sm_resp;
// the next transport write fails or is cancelled without a response. No Codec
// is used to construct the peer response or inspect outgoing headers.
func TestSendTextTransportOutcomes(t *testing.T) {
	for _, tc := range []struct {
		name    string
		written int // -1 means the complete frame
		outcome Outcome
	}{{"zero", 0, NotWritten}, {"partial", 7, PartlyWritten}, {"written-no-response", -1, Written}} {
		t.Run(tc.name, func(t *testing.T) {
			a, peer := net.Pipe()
			defer peer.Close()
			ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
			defer cancel()
			failure := errors.New("injected second segment write failure")
			first := make(chan struct{})
			var frames [][]byte // accessed by the writer, then after Session.Wait
			conn := &faultConn{Conn: a, write: func(frame []byte) (int, error) {
				frames = append(frames, append([]byte(nil), frame...))
				if len(frames) == 1 {
					close(first)
					return len(frame), nil
				}
				if tc.written < 0 {
					cancel()
					return len(frame), nil
				}
				return tc.written, failure
			}}
			s, err := newSession(conn, testCodec(t), Config{}, false, Bound, Transceiver)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { s.Close(); s.Wait() }()
			ackDone := make(chan error, 1)
			go func() {
				select {
				case <-first:
					_, err := peer.Write([]byte{0, 0, 0, 19, 0x80, 0, 0, 4, 0, 0, 0, 0, 0, 0, 0, 1, 'o', 'k', 0})
					ackDone <- err
				case <-ctx.Done():
					ackDone <- ctx.Err()
				}
			}()
			got, err := s.SendText(ctx, TextMessage{Text: strings.Repeat("A", 400)}, message.GSMText())
			s.Close()
			s.Wait()
			if ackErr := <-ackDone; ackErr != nil {
				t.Fatal(ackErr)
			}
			wantErr := failure
			if tc.written < 0 {
				wantErr = context.Canceled
			}
			if !errors.Is(err, wantErr) || len(got.Segments) != 3 || len(frames) != 2 {
				t.Fatalf("result=%+v writes=%d error=%v", got, len(frames), err)
			}
			for i, frame := range frames {
				if len(frame) < 16 || int(binary.BigEndian.Uint32(frame)) != len(frame) || binary.BigEndian.Uint32(frame[4:]) != 4 || binary.BigEndian.Uint32(frame[12:]) != uint32(i+1) {
					t.Fatalf("unexpected outgoing submit header: %x", frame)
				}
			}
			accepted, failed, unsent := got.Segments[0], got.Segments[1], got.Segments[2]
			if accepted.Number != 1 || accepted.Err != nil || accepted.MessageID != "ok" || accepted.Result.Outcome != ResponseReceived || accepted.Result.BytesWritten != len(frames[0]) || accepted.Result.Sequence != 1 {
				t.Fatalf("lost accepted part: %+v", accepted)
			}
			n := tc.written
			if n < 0 {
				n = len(frames[1])
			}
			if failed.Number != 2 || !errors.Is(failed.Err, wantErr) || failed.MessageID != "" || failed.Result.Outcome != tc.outcome || failed.Result.Sequence != 2 || failed.Result.BytesWritten != n || failed.Result.Response.Body != nil {
				t.Fatalf("incorrect uncertainty: %+v", failed)
			}
			if unsent.Number != 3 || unsent.Err != nil || unsent.MessageID != "" || unsent.Result.Outcome != NotAttempted || unsent.Result.Sequence != 0 || unsent.Result.BytesWritten != 0 || unsent.Result.Response.Body != nil {
				t.Fatalf("unscheduled part acquired an outcome: %+v", unsent)
			}
		})
	}
}

func TestSendTextSharedReferenceCapacity(t *testing.T) {
	// A successful ACK does not release a lease. Two separately bound sessions
	// share one explicit sending domain; the second must fail before any Request.
	client, server := sessionPair(t, Config{})
	other, _ := sessionPair(t, Config{})
	refs, err := message.NewReferences(16, 1, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	acked := make(chan error, 1)
	go func() {
		for range 2 {
			in, err := server.Receive(ctx)
			if err == nil {
				err = in.Respond(ctx, pdu.OK, &pdu.SubmitSMResp{})
			}
			if err != nil {
				acked <- err
				return
			}
		}
		acked <- nil
	}()
	if _, err = client.SendText(ctx, TextMessage{Text: strings.Repeat("A", 200)}, message.GSMText(), refs); err != nil {
		t.Fatal(err)
	}
	if err = <-acked; err != nil {
		t.Fatal(err)
	}
	got, err := other.SendText(ctx, TextMessage{Text: strings.Repeat("A", 200)}, message.GSMText(), refs)
	if !errors.Is(err, message.ErrCapacity) || len(got.Segments) != 2 {
		t.Fatal(got, err)
	}
	for _, part := range got.Segments {
		if part.Result.Outcome != NotAttempted || part.Result.Sequence != 0 || part.Result.BytesWritten != 0 {
			t.Fatal(part)
		}
	}
	other.mu.Lock()
	defer other.mu.Unlock()
	if other.seq != 1 || len(other.pending) != 0 || len(other.requests) != 0 {
		t.Fatal("exhaustion entered Request")
	}
}
