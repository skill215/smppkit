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
	"fmt"
	"io"
	"net"
	"runtime/pprof"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/skill215/smppkit/pdu"
)

func lifecycleHex(s string) []byte {
	b, err := hex.DecodeString(strings.Join(strings.Fields(s), ""))
	if err != nil {
		panic(err)
	}
	return b
}

func TestLifecycleCrossedUnbind(t *testing.T) {
	for _, responseFirst := range []bool{false, true} {
		name := "ack-first"
		if responseFirst {
			name = "response-first"
		}
		t.Run(name, func(t *testing.T) {
			s, peer := noticeSession(t, false, Bound, Transceiver, Config{})
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			done := make(chan error, 1)
			go func() {
				got := make([]byte, 16)
				if _, err := io.ReadFull(peer, got); err != nil {
					done <- err
					return
				}
				if !bytes.Equal(got, lifecycleHex("00000010000000060000000000000001")) {
					done <- errors.New("wrong local unbind")
					return
				}
				if _, err := peer.Write(lifecycleHex("00000010000000060000000000000009")); err != nil {
					done <- err
					return
				}
				response := lifecycleHex("00000010800000060000000000000001")
				if responseFirst {
					if _, err := peer.Write(response); err != nil {
						done <- err
						return
					}
				}
				if _, err := io.ReadFull(peer, got); err != nil {
					done <- err
					return
				}
				if !bytes.Equal(got, lifecycleHex("00000010800000060000000000000009")) {
					done <- errors.New("missing peer unbind ACK")
					return
				}
				if !responseFirst {
					if _, err := peer.Write(response); err != nil {
						done <- err
						return
					}
				}
				done <- nil
			}()
			err := s.Shutdown(ctx)
			peerErr := <-done
			if err != nil || peerErr != nil {
				t.Fatalf("crossed unbind: shutdown=%v peer=%v", err, peerErr)
			}
			s.Wait()
			if s.State() != Closed {
				t.Fatal("unbind left session active")
			}
		})
	}
}

func TestLifecycleRequestUnbindIsTerminal(t *testing.T) {
	s, peer := noticeSession(t, false, Bound, Transceiver, Config{})
	done := make(chan error, 1)
	go func() {
		got := make([]byte, 16)
		_, err := io.ReadFull(peer, got)
		if err == nil {
			_, err = peer.Write(lifecycleHex("00000010800000060000000000000001"))
		}
		done <- err
	}()
	r, err := s.Request(t.Context(), &pdu.Unbind{})
	if err != nil || r.Outcome != ResponseReceived {
		t.Fatal(r, err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if s.State() != Closed {
		t.Fatal("correlated unbind left state", s.State())
	}
}

func TestLifecycleCloseReleasesUndeliveredInput(t *testing.T) {
	client, server := sessionPair(t, Config{})
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	sent := make(chan struct{})
	go func() {
		_, _ = client.Request(ctx, &pdu.SubmitSM{SMFields: pdu.SMFields{ShortMessage: bytes.Repeat([]byte{'x'}, 140)}})
		close(sent)
	}()
	// A following synchronous control response proves the reader has admitted
	// the preceding request, without consuming its application-owned input slot.
	if _, err := client.Request(ctx, &pdu.EnquireLink{}); err != nil {
		t.Fatal(err)
	}
	// The requests can queue in either order; wait for the admission under mu.
	deadline := time.Now().Add(time.Second)
	for {
		server.mu.Lock()
		n := len(server.inbound)
		server.mu.Unlock()
		if n == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("no admission")
		}
		time.Sleep(time.Millisecond)
	}
	server.Close()
	server.Wait()
	client.Close()
	client.Wait()
	<-sent
	server.mu.Lock()
	inbound, pending := len(server.inbound), len(server.pending)
	server.mu.Unlock()
	if inbound != 0 || pending != 0 || len(server.incoming) != 0 || len(server.notices) != 0 {
		t.Fatalf("closed session retains input: inbound=%d pending=%d queue=%d", inbound, pending, len(server.incoming))
	}
}

// Fragment transport calls without changing their deadline or cancellation
// behavior. Counters expose whether a frame budget is incorrectly reset for
// every fragment, while net.Pipe supplies real deadline-triggered errors.
type lifecycleFragments struct {
	net.Conn
	readDeadlines, writeDeadlines atomic.Int32
	readStarted                   chan struct{}
	chunk                         int
}

func (c *lifecycleFragments) SetReadDeadline(d time.Time) error {
	if c.readDeadlines.Add(1) == 1 && c.readStarted != nil {
		close(c.readStarted)
	}
	return c.Conn.SetReadDeadline(d)
}
func (c *lifecycleFragments) SetWriteDeadline(d time.Time) error {
	c.writeDeadlines.Add(1)
	return c.Conn.SetWriteDeadline(d)
}
func (c *lifecycleFragments) Write(b []byte) (int, error) {
	if c.chunk > 0 && len(b) > c.chunk {
		b = b[:c.chunk]
	}
	return c.Conn.Write(b)
}

func TestLifecycleFragmentedDeadlines(t *testing.T) {
	for _, prefix := range []int{1, 10, 28} {
		t.Run(fmt.Sprintf("read-prefix-%d", prefix), func(t *testing.T) {
			a, b := net.Pipe()
			defer b.Close()
			conn := &lifecycleFragments{Conn: a, readStarted: make(chan struct{})}
			s, err := newSession(conn, testCodec(t), Config{ReadTimeout: 80 * time.Millisecond}, true, Bound, Transceiver)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { s.Close(); s.Wait() }()
			<-conn.readStarted
			raw := lifecycleHex("0000001f000001030000000000000007000000000000000000000421000101")
			// Length, header and TLV truncations must each expire under the one
			// frame budget, without ever being published to the application.
			for _, v := range raw[:prefix] {
				if _, err := b.Write([]byte{v}); err != nil {
					t.Fatal(err)
				}
			}
			select {
			case <-s.Done():
			case <-time.After(time.Second):
				t.Fatal("partial frame did not expire")
			}
			if timeout, ok := errors.AsType[net.Error](s.Err()); !ok || !timeout.Timeout() {
				t.Fatal(s.Err())
			}
			if conn.readDeadlines.Load() != 1 || len(s.incoming) != 0 {
				t.Fatal("frame budget reset or partial input admitted")
			}
		})
	}
	t.Run("write-fragments", func(t *testing.T) {
		a, b := net.Pipe()
		defer b.Close()
		conn := &lifecycleFragments{Conn: a, chunk: 3}
		s, err := newSession(conn, testCodec(t), Config{WriteTimeout: 80 * time.Millisecond}, true, Bound, Receiver)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { s.Close(); s.Wait() }()
		done := make(chan error, 1)
		go func() { buf := make([]byte, 9); _, err := io.ReadFull(b, buf); done <- err }()
		r, err := s.Notify(t.Context(), &pdu.AlertNotification{})
		if e := <-done; e != nil {
			t.Fatal(e)
		}
		if timeout, ok := errors.AsType[net.Error](err); !ok || !timeout.Timeout() || r.Outcome != PartlyWritten || r.BytesWritten != 9 {
			t.Fatal(r, err)
		}
		if conn.writeDeadlines.Load() != 1 {
			t.Fatal("write budget reset per fragment")
		}
		s.Wait()
	})
}

func TestLifecycleCrossedUnbindMissingHalf(t *testing.T) {
	for _, missingResponse := range []bool{false, true} {
		name := "blocked-peer-ack"
		if missingResponse {
			name = "missing-local-response"
		}
		t.Run(name, func(t *testing.T) {
			s, peer := noticeSession(t, false, Bound, Transceiver, Config{RequestTimeout: 60 * time.Millisecond})
			done := make(chan error, 1)
			go func() {
				got := make([]byte, 16)
				if _, err := io.ReadFull(peer, got); err != nil {
					done <- err
					return
				}
				if _, err := peer.Write(lifecycleHex("00000010000000060000000000000009")); err != nil {
					done <- err
					return
				}
				if missingResponse {
					_, err := io.ReadFull(peer, got)
					done <- err
				} else {
					_, err := peer.Write(lifecycleHex("00000010800000060000000000000001"))
					done <- err
				}
			}()
			r, err := s.Request(t.Context(), &pdu.Unbind{})
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatal(r, err)
			}
			want := Written
			if !missingResponse {
				want = ResponseReceived
			}
			if r.Outcome != want || s.State() != Closed {
				t.Fatal(r, s.State())
			}
			if err := <-done; err != nil {
				t.Fatal(err)
			}
			s.Wait()
		})
	}
}

func TestLifecycleRetiredHandleAndRepeatedConnections(t *testing.T) {
	ownedLoops := func() int {
		var b bytes.Buffer
		if err := pprof.Lookup("goroutine").WriteTo(&b, 2); err != nil {
			t.Fatal(err)
		}
		stacks := b.String()
		return strings.Count(stacks, "smppkit.(*Session).readLoop(") + strings.Count(stacks, "smppkit.(*Session).writeLoop(") + strings.Count(stacks, "smppkit.(*Session).expiryLoop(")
	}
	before := ownedLoops()
	for cycle := range 40 {
		client, server := sessionPair(t, Config{})
		ctx, cancel := context.WithTimeout(t.Context(), time.Second)
		requestDone := make(chan error, 1)
		go func() { _, err := client.Request(ctx, &pdu.SubmitSM{}); requestDone <- err }()
		in, err := server.Receive(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if cycle%2 == 0 {
			if err = in.Respond(ctx, pdu.OK, &pdu.SubmitSMResp{}); err != nil {
				t.Fatal(err)
			}
			if err = <-requestDone; err != nil {
				t.Fatal(err)
			}
			if err = client.Shutdown(ctx); err != nil {
				t.Fatal(err)
			}
		} else {
			server.Close()
			client.Close()
			if err = <-requestDone; err == nil {
				t.Fatal("aborted request succeeded")
			}
			if err = in.Respond(ctx, pdu.OK, &pdu.SubmitSMResp{}); err == nil {
				t.Fatal("retired handle wrote response")
			}
		}
		client.Close()
		server.Close()
		client.Wait()
		server.Wait()
		cancel()
		for _, s := range []*Session{client, server} {
			s.mu.Lock()
			reservations := len(s.pending) + len(s.inbound)
			s.mu.Unlock()
			if reservations+len(s.requests)+len(s.responses)+len(s.notifications)+len(s.incoming)+len(s.notices)+len(s.slots)+len(s.notifySlots) != 0 {
				t.Fatal("retained lifecycle resources", cycle)
			}
		}
	}
	if after := ownedLoops(); after != before {
		t.Fatalf("Session goroutines before=%d after=%d", before, after)
	}
}

func TestLifecycleFinishedJobsReleaseCapturedData(t *testing.T) {
	s, peer := noticeSession(t, true, Bound, Receiver, Config{})
	j := job(lifecycleHex("00000010800000150000000000000009"))
	j.ctx = t.Context()
	j.after = func() {}
	if err := s.enqueueResponse(j); err != nil {
		t.Fatal(err)
	}
	if _, err := io.ReadFull(peer, make([]byte, 16)); err != nil {
		t.Fatal(err)
	}
	<-j.done
	if j.data != nil || j.ctx != nil || j.after != nil {
		t.Fatal("idle writer retains completed frame/captures")
	}
	queued := job(make([]byte, 4096))
	queued.ctx = t.Context()
	queued.after = func() {}
	if !queued.cancelQueued(context.Canceled) || queued.data != nil || queued.after != nil || queued.ctx != nil {
		t.Fatal("cancelled entry retains frame/captures")
	}
}

type lifecycleUnbindGate struct {
	net.Conn
	arrived chan<- struct{}
	release <-chan struct{}
}

func (c *lifecycleUnbindGate) Write(b []byte) (int, error) {
	if len(b) == 16 && bytes.Equal(b[4:8], []byte{0, 0, 0, 6}) {
		c.arrived <- struct{}{}
		<-c.release
	}
	return c.Conn.Write(b)
}

func TestLifecycleSimultaneousShutdown(t *testing.T) {
	a, b := net.Pipe()
	arrived := make(chan struct{}, 2)
	release := make(chan struct{})
	left, err := newSession(&lifecycleUnbindGate{a, arrived, release}, testCodec(t), Config{}, false, Bound, Transceiver)
	if err != nil {
		t.Fatal(err)
	}
	right, err := newSession(&lifecycleUnbindGate{b, arrived, release}, testCodec(t), Config{}, true, Bound, Transceiver)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { left.Close(); right.Close(); left.Wait(); right.Wait() }()
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	done := make(chan error, 2)
	go func() { done <- left.Shutdown(ctx) }()
	go func() { done <- right.Shutdown(ctx) }()
	for range 2 {
		select {
		case <-arrived:
		case <-ctx.Done():
			close(release)
			t.Fatal("both unbinds not queued")
		}
	}
	close(release)
	for range 2 {
		if err := <-done; err != nil {
			t.Error(err)
		}
	}
}

// Drive the exact reader-before-writer completion order without depending on
// the scheduler. Full transport tests above exercise both ACK/response orders.
func TestLifecycleEOFBeforeACKCompletion(t *testing.T) {
	for _, crossed := range []bool{false, true} {
		t.Run(fmt.Sprintf("crossed-%t", crossed), func(t *testing.T) {
			a, b := net.Pipe()
			defer a.Close()
			defer b.Close()
			s := &Session{conn: a, state: Unbinding, done: make(chan struct{}),
				peerUnbind: true, localUnbind: crossed, localUnbindDone: crossed}
			for _, err := range []error{io.EOF, io.ErrClosedPipe, net.ErrClosed} {
				if !s.finishUnbindOnWriter(err) || s.unbindCompleteLocked() {
					t.Fatal("reader must leave the unfinished ACK to the writer", err)
				}
			}
			for _, err := range []error{io.ErrUnexpectedEOF, context.DeadlineExceeded, ErrState} {
				if s.finishUnbindOnWriter(err) {
					t.Fatal("non-graceful failure hidden", err)
				}
			}
			if crossed {
				s.localUnbindDone = false
				if s.finishUnbindOnWriter(io.EOF) {
					t.Fatal("missing response cannot be repaired by ACK completion")
				}
				s.localUnbindDone = true
			}
			s.finishPeerUnbind()
			if s.State() != Closed || !s.unbindCompleteLocked() || s.Err() != ErrClosed {
				t.Fatal("writer completion did not retire the connection")
			}
			if s.finishUnbindOnWriter(io.EOF) {
				t.Fatal("closed connection cannot defer further failures")
			}
		})
	}
}
