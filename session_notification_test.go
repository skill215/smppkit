// SPDX-AI-Disclosure: ai-generated
// SPDX-AI-Model: GPT-6.0 Astra
// SPDX-AI-Provider: OpenAI
// SPDX-AI-Scope: Test code generated and revised with Codex under maintainer direction.
// SPDX-AI-Date: 2026-10-28

package smpp

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/skill215/smppkit/codec"
	"github.com/skill215/smppkit/pdu"
	"github.com/skill215/smppkit/tlv"
)

func noticeSession(t *testing.T, server bool, state State, role Role, cfg Config) (*Session, net.Conn) {
	t.Helper()
	a, b := net.Pipe()
	s, err := newSession(a, testCodec(t), cfg, server, state, role)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close(); b.Close(); s.Wait() })
	return s, b
}

func TestNotificationRolesAndVersions(t *testing.T) {
	for _, v := range []pdu.Version{pdu.V34, pdu.V50} {
		for _, role := range []Role{Transmitter, Receiver, Transceiver} {
			t.Run(fmt.Sprintf("%x/%d", v, role), func(t *testing.T) {
				client, server := dataSessionPair(t, v, role)
				ctx, cancel := context.WithTimeout(t.Context(), time.Second)
				defer cancel()
				alert := &pdu.AlertNotification{Optional: tlv.Fields{{Tag: tlv.MSAvailabilityStatus, Value: []byte{0}}}}
				if r, err := client.Notify(ctx, alert); !errors.Is(err, ErrState) || r.Sequence != 0 {
					t.Fatal(r, err)
				}
				if role == Transmitter {
					if r, err := server.Notify(ctx, alert); !errors.Is(err, ErrState) || r.Sequence != 0 {
						t.Fatal(r, err)
					}
					return
				}
				r, err := server.Notify(ctx, alert)
				if err != nil || r.Outcome != Written || r.Response.Body != nil || r.BytesWritten != 27 {
					t.Fatal(r, err)
				}
				packet, err := client.ReceiveNotification(ctx)
				if err != nil || packet.Sequence != r.Sequence || packet.Body.CommandID() != pdu.AlertNotificationID {
					t.Fatal(packet, err)
				}
				if r, err := server.Request(ctx, alert); !errors.Is(err, ErrState) || r.Sequence != 0 {
					t.Fatal(r, err)
				}
				for _, body := range []pdu.Body{&pdu.Outbind{}, &pdu.EnquireLink{}, &pdu.DataSM{}, (*pdu.AlertNotification)(nil)} {
					if r, err := server.Notify(ctx, body); err == nil || r.Sequence != 0 {
						t.Fatal("invalid Notify", r, err)
					}
				}
			})
		}
	}
	for _, limit := range []int{-1, 65537} {
		if _, err := (Config{NotificationLimit: limit}).normalized(); err == nil {
			t.Fatal("invalid limit")
		}
	}
}

func TestNotificationIndependentPeerNoACK(t *testing.T) {
	s, peer := noticeSession(t, false, Bound, Receiver, Config{InboundTimeout: 10 * time.Millisecond})
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		raw, _ := hex.DecodeString("000000210000010200000000000000070101313233000500617070000422000100")
		for _, b := range raw {
			if _, err := peer.Write([]byte{b}); err != nil {
				done <- err
				return
			}
		}
		// Next bytes must be the application's enquire_link, not an invented ACK.
		want, _ := hex.DecodeString("00000010000000150000000000000001")
		got := make([]byte, 16)
		if _, err := io.ReadFull(peer, got); err != nil {
			done <- err
			return
		}
		if !bytes.Equal(got, want) {
			done <- fmt.Errorf("unexpected ACK/frame %x", got)
			return
		}
		response, _ := hex.DecodeString("00000010800000150000000000000001")
		_, err := peer.Write(response)
		done <- err
	}()
	packet, err := s.ReceiveNotification(ctx)
	if err != nil || packet.Body.(*pdu.AlertNotification).Source.Value != "123" {
		t.Fatal(packet, err)
	}
	time.Sleep(30 * time.Millisecond) // notification has no inbound ACK deadline
	if s.State() != Bound {
		t.Fatal(s.Err())
	}
	r, err := s.Request(ctx, &pdu.EnquireLink{})
	if err != nil || r.Outcome != ResponseReceived {
		t.Fatal(r, err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	pending, inbound := len(s.pending), len(s.inbound)
	s.mu.Unlock()
	if pending != 0 || inbound != 0 {
		t.Fatal("notification acquired request reservation")
	}
}

func TestNotificationWindowAndConcurrentWrites(t *testing.T) {
	client, server := sessionPair(t, Config{Window: 1, NotificationLimit: 32})
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	delivery := make(chan error, 1)
	go func() { _, err := server.Request(ctx, &pdu.DeliverSM{}); delivery <- err }()
	in, err := client.Receive(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// Hold the only outbound request slot, yet notifications and control
	// responses must continue. Notification and request sequences share a space.
	const count = 24
	var wg sync.WaitGroup
	errs := make(chan error, count+1)
	for range count {
		wg.Go(func() {
			r, err := server.Notify(ctx, &pdu.AlertNotification{})
			if err == nil && r.Outcome != Written {
				err = errors.New("not Written")
			}
			errs <- err
		})
	}
	wg.Go(func() { _, err := client.Request(ctx, &pdu.EnquireLink{}); errs <- err })
	seen := map[uint32]bool{in.sequence: true}
	for range count {
		packet, err := client.ReceiveNotification(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if seen[packet.Sequence] {
			t.Fatal("reused sequence", packet.Sequence)
		}
		seen[packet.Sequence] = true
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := in.Respond(ctx, pdu.OK, &pdu.DeliverSMResp{}); err != nil {
		t.Fatal(err)
	}
	if err := <-delivery; err != nil {
		t.Fatal(err)
	}
}

func TestNotificationOverflowAndState(t *testing.T) {
	for _, tc := range []struct {
		name   string
		server bool
		state  State
		role   Role
		raw    string
		count  int
		want   error
	}{
		{"full", false, Bound, Receiver, "00000016000001020000000000000007000000000000", 2, ErrOverloaded},
		{"wrong-direction", true, Bound, Receiver, "00000016000001020000000000000007000000000000", 1, ErrState},
		{"unbound-alert", false, Connected, 0, "00000016000001020000000000000007000000000000", 1, ErrState},
		{"tx-alert", false, Bound, Transmitter, "00000016000001020000000000000007000000000000", 1, ErrState},
		{"bound-outbind", false, Bound, Receiver, "000000160000000b00000000000000076d6300707700", 1, ErrState},
		{"duplicate-outbind", false, Connected, 0, "000000160000000b00000000000000076d6300707700", 2, ErrState},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, peer := noticeSession(t, tc.server, tc.state, tc.role, Config{NotificationLimit: 1})
			raw, _ := hex.DecodeString(tc.raw)
			for range tc.count {
				if _, err := peer.Write(raw); err != nil {
					t.Fatal(err)
				}
			}
			select {
			case <-s.Done():
			case <-time.After(time.Second):
				t.Fatal("reader blocked")
			}
			if !errors.Is(s.Err(), tc.want) {
				t.Fatal(s.Err())
			}
		})
	}
}

func TestNotificationLocalStateAndTLVPolicy(t *testing.T) {
	for _, state := range []State{Connected, Binding, Unbinding} {
		s, _ := noticeSession(t, true, state, Receiver, Config{})
		if r, err := s.Notify(t.Context(), &pdu.AlertNotification{}); !errors.Is(err, ErrState) || r.Sequence != 0 {
			t.Fatal(r, err)
		}
	}
	s, _ := noticeSession(t, true, Bound, Receiver, Config{})
	s.mu.Lock()
	s.tlvAllowed = false
	s.mu.Unlock()
	if r, err := s.Notify(t.Context(), &pdu.AlertNotification{Optional: tlv.Fields{{Tag: tlv.MSAvailabilityStatus, Value: []byte{0}}}}); !errors.Is(err, ErrState) || r.Sequence != 0 {
		t.Fatal(r, err)
	}
	c, err := codec.New(codec.Config{Version: pdu.V33})
	if err != nil {
		t.Fatal(err)
	}
	a, b := net.Pipe()
	defer b.Close()
	legacy, err := newSession(a, c, Config{}, true, Bound, Receiver)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { legacy.Close(); legacy.Wait() }()
	if r, err := legacy.Notify(t.Context(), &pdu.AlertNotification{}); !errors.Is(err, codec.ErrUnsupported) || r.Sequence != 0 {
		t.Fatal(r, err)
	}
}

func TestNotificationWriterFairness(t *testing.T) {
	a, b := net.Pipe()
	// Preload a sustained response backlog and both application queues before
	// starting the real writer. Observe wire order, not its scheduling fields.
	s := &Session{conn: a, cfg: Config{WriteTimeout: time.Second}, state: Bound,
		requests: make(chan *writeJob, 4), notifications: make(chan *writeJob, 4), responses: make(chan *writeJob, 64), done: make(chan struct{})}
	t.Cleanup(func() { s.Close(); b.Close(); s.Wait() })
	for i := uint32(1); i <= 64; i++ {
		raw, _ := hex.DecodeString("00000010800000150000000000000001")
		binary.BigEndian.PutUint32(raw[12:], i)
		s.responses <- job(raw)
	}
	for i := uint32(1); i <= 4; i++ {
		raw, _ := hex.DecodeString("00000010000000150000000000000001")
		binary.BigEndian.PutUint32(raw[12:], i)
		s.requests <- job(raw)
		raw, _ = hex.DecodeString("00000016000001020000000000000001000000000000")
		binary.BigEndian.PutUint32(raw[12:], i+4)
		s.notifications <- job(raw)
	}
	s.wg.Go(s.writeLoop)
	if err := b.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	burst, applications := 0, 0
	for range 72 {
		header := make([]byte, 16)
		if _, err := io.ReadFull(b, header); err != nil {
			t.Fatal(err)
		}
		length := binary.BigEndian.Uint32(header)
		if length != 16 && length != 22 {
			t.Fatal("interleaved frame", length)
		}
		if _, err := io.ReadFull(b, make([]byte, int(length)-16)); err != nil {
			t.Fatal(err)
		}
		id := binary.BigEndian.Uint32(header[4:])
		if id == 0x80000015 {
			burst++
			if applications < 8 && burst > 8 {
				t.Fatal("response starvation")
			}
			continue
		}
		want := uint32(0x15)
		if applications%2 == 1 {
			want = 0x102
		}
		if id != want {
			t.Fatal("application queues did not alternate", id, want)
		}
		applications++
		burst = 0
	}
	if applications != 8 {
		t.Fatal(applications)
	}
}

func TestNotificationSequenceRetirement(t *testing.T) {
	s, peer := noticeSession(t, true, Bound, Receiver, Config{})
	s.mu.Lock()
	s.seq = 0x7ffffffe
	s.mu.Unlock()
	done := make(chan error, 1)
	go func() {
		raw := make([]byte, 22)
		_, err := io.ReadFull(peer, raw)
		if err == nil && binary.BigEndian.Uint32(raw[12:]) != 0x7fffffff {
			err = errors.New("wrong final sequence")
		}
		done <- err
	}()
	if r, err := s.Notify(t.Context(), &pdu.AlertNotification{}); err != nil || r.Sequence != 0x7fffffff {
		t.Fatal(r, err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if r, err := s.Notify(t.Context(), &pdu.AlertNotification{}); err == nil || r.Sequence != 0 || s.State() != Closed {
		t.Fatal(r, err)
	}
}

func TestNotificationWriteFailureAndCancellation(t *testing.T) {
	t.Run("partial", func(t *testing.T) {
		a, b := net.Pipe()
		defer b.Close()
		failure := errors.New("partial notice write")
		s, err := newSession(&faultConn{Conn: a, write: func([]byte) (int, error) { return 7, failure }}, testCodec(t), Config{}, true, Bound, Receiver)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { s.Close(); s.Wait() }()
		r, err := s.Notify(t.Context(), &pdu.AlertNotification{})
		if !errors.Is(err, failure) || r.Outcome != PartlyWritten || r.BytesWritten != 7 {
			t.Fatal(r, err)
		}
	})
	t.Run("queued-and-active", func(t *testing.T) {
		a, b := net.Pipe()
		defer b.Close()
		started := make(chan struct{})
		var once sync.Once
		conn := &faultConn{Conn: a, write: func(raw []byte) (int, error) { once.Do(func() { close(started) }); return a.Write(raw) }}
		s, err := newSession(conn, testCodec(t), Config{NotificationLimit: 2}, true, Bound, Receiver)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { s.Close(); s.Wait() }()
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		first := make(chan Result, 1)
		go func() {
			r, err := s.Notify(ctx, &pdu.AlertNotification{})
			if !errors.Is(err, context.Canceled) {
				t.Error(err)
			}
			first <- r
		}()
		<-started
		short, stop := context.WithTimeout(t.Context(), 20*time.Millisecond)
		defer stop()
		r, err := s.Notify(short, &pdu.AlertNotification{})
		if !errors.Is(err, context.DeadlineExceeded) || r.Outcome != NotWritten || r.Sequence == 0 || s.State() != Bound {
			t.Fatal(r, err)
		}
		// Cancelled queue entries remain bounded until the active writer drains
		// them. Filling that queue must not consume a sequence on rejection.
		short2, stop2 := context.WithTimeout(t.Context(), 20*time.Millisecond)
		defer stop2()
		if r, err := s.Notify(short2, &pdu.AlertNotification{}); !errors.Is(err, context.DeadlineExceeded) || r.Outcome != NotWritten {
			t.Fatal(r, err)
		}
		if r, err := s.Notify(t.Context(), &pdu.AlertNotification{}); !errors.Is(err, ErrOverloaded) || r.Sequence != 0 {
			t.Fatal(r, err)
		}
		cancel() // active write cannot be interrupted safely without closing
		if r := <-first; r.Outcome != NotWritten {
			t.Fatal(r)
		}
		s.Wait()
		if s.State() != Closed || len(s.notifySlots) != 0 || len(s.notifications) != 0 {
			t.Fatal("shutdown did not release notification work")
		}
	})
	t.Run("receive-cancel-retains-queue", func(t *testing.T) {
		s, peer := noticeSession(t, false, Bound, Receiver, Config{})
		raw, _ := hex.DecodeString("00000016000001020000000000000007000000000000")
		if _, err := peer.Write(raw); err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		if _, err := s.ReceiveNotification(ctx); !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
		ctx, cancel = context.WithTimeout(t.Context(), time.Second)
		defer cancel()
		if _, err := s.ReceiveNotification(ctx); err != nil {
			t.Fatal(err)
		}
	})
}
