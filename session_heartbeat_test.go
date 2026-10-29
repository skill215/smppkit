// SPDX-AI-Disclosure: ai-generated
// SPDX-AI-Model: GPT-6.0 Astra
// SPDX-AI-Provider: OpenAI
// SPDX-AI-Scope: Test code generated and revised with Codex under maintainer direction.
// SPDX-AI-Date: 2026-10-29

package smpp

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/skill215/smppkit/codec"
	"github.com/skill215/smppkit/pdu"
)

// Virtual time makes interval/deadline boundary assertions deterministic. The
// peer below uses independently authored headers, never Codec.Encode/Decode.
func heartbeatConfig() Config {
	return Config{EnquireLinkInterval: time.Second, EnquireLinkTimeout: 3 * time.Second,
		RequestTimeout: time.Hour, ReadTimeout: time.Hour, WriteTimeout: time.Hour}
}
func heartbeatSession(t *testing.T, cfg Config, v pdu.Version, server bool, state State) (*Session, net.Conn) {
	t.Helper()
	c, err := codec.New(codec.Config{Version: v})
	if err != nil {
		t.Fatal(err)
	}
	a, b := net.Pipe()
	s, err := newSession(a, c, cfg, server, state, Transmitter)
	if err != nil {
		a.Close()
		b.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close(); b.Close(); s.Wait() })
	return s, b
}
func heartbeatFrame(t *testing.T, peer net.Conn) []byte {
	t.Helper()
	header := make([]byte, 16)
	if _, err := io.ReadFull(peer, header); err != nil {
		t.Fatal(err)
	}
	n := int(binary.BigEndian.Uint32(header))
	if n < 16 || n > 4096 {
		t.Fatalf("unexpected length %d", n)
	}
	frame := append(header, make([]byte, n-16)...)
	if _, err := io.ReadFull(peer, frame[16:]); err != nil {
		t.Fatal(err)
	}
	return frame
}
func heartbeatExpect(t *testing.T, peer net.Conn, command, status, seq uint32) {
	t.Helper()
	if got := heartbeatFrame(t, peer); !bytes.Equal(got, independentPacket(command, status, seq, "")) {
		t.Fatalf("unexpected frame %x", got)
	}
}
func heartbeatSend(t *testing.T, peer net.Conn, command, status, seq uint32, body string) {
	t.Helper()
	if _, err := peer.Write(independentPacket(command, status, seq, body)); err != nil {
		t.Fatal(err)
	}
}
func heartbeatStats(t *testing.T, s *Session, state State, seq uint32, pendingCount int) {
	t.Helper()
	synctest.Wait()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state != state || s.seq != seq || len(s.pending) != pendingCount {
		t.Fatalf("state=%v seq=%d pending=%d err=%v", s.state, s.seq, len(s.pending), s.err)
	}
}

func TestHeartbeatConfiguration(t *testing.T) {
	cfg, err := (Config{RequestTimeout: 7 * time.Second}).normalized()
	if err != nil || cfg.EnquireLinkInterval != 0 || cfg.EnquireLinkTimeout != 7*time.Second {
		t.Fatal(cfg, err)
	}
	for _, cfg := range []Config{{EnquireLinkInterval: -1}, {EnquireLinkTimeout: -1}} {
		if _, err := cfg.normalized(); err == nil {
			t.Fatal("negative heartbeat duration accepted")
		}
	}
	synctest.Test(t, func(t *testing.T) {
		s, _ := heartbeatSession(t, Config{}, pdu.V50, false, Bound)
		time.Sleep(10 * time.Second)
		heartbeatStats(t, s, Bound, 0, 0)
		if s.heartbeatReady != nil || s.heartbeatStop != nil {
			t.Fatal("disabled scheduler allocated channels")
		}
	})
}

func TestHeartbeatStartsOnlyAfterSuccessfulBind(t *testing.T) {
	for _, v := range []pdu.Version{pdu.V33, pdu.V34, pdu.V50} {
		t.Run(fmt.Sprintf("%x", v), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				s, peer := heartbeatSession(t, heartbeatConfig(), v, false, Connected)
				time.Sleep(5 * time.Second)
				heartbeatStats(t, s, Connected, 0, 0)
				for attempt := uint32(1); attempt <= 2; attempt++ {
					done := make(chan error, 1)
					go func() {
						_, err := s.Bind(context.Background(), &pdu.BindTransmitter{BindFields: pdu.BindFields{InterfaceVersion: v}})
						done <- err
					}()
					frame := heartbeatFrame(t, peer)
					if binary.BigEndian.Uint32(frame[4:]) != 2 || binary.BigEndian.Uint32(frame[12:]) != attempt {
						t.Fatalf("bind %x", frame)
					}
					time.Sleep(2 * time.Second)
					heartbeatStats(t, s, Binding, attempt, 1)
					status, body := uint32(0), "00"
					if attempt == 1 {
						status, body = 0x0d, "00"
					}
					heartbeatSend(t, peer, 0x80000002, status, attempt, body)
					err := <-done
					if attempt == 1 {
						if _, ok := errors.AsType[*PeerError](err); !ok {
							t.Fatal(err)
						}
						time.Sleep(4 * time.Second)
						heartbeatStats(t, s, Connected, 1, 0)
					} else if err != nil {
						t.Fatal(err)
					}
				}
				synctest.Wait()
				time.Sleep(time.Second - time.Nanosecond)
				heartbeatStats(t, s, Bound, 2, 0)
				heartbeatExpect(t, peer, 0x15, 0, 3)
				heartbeatSend(t, peer, 0x80000015, 0, 3, "")
				heartbeatStats(t, s, Bound, 3, 0)
			})
		})
	}
}

func TestHeartbeatBoundEndpointsAndCorrelation(t *testing.T) {
	for _, v := range []pdu.Version{pdu.V33, pdu.V34, pdu.V50} {
		for _, server := range []bool{false, true} {
			t.Run(fmt.Sprintf("%x/server=%t", v, server), func(t *testing.T) {
				synctest.Test(t, func(t *testing.T) {
					cfg := heartbeatConfig()
					cfg.EnquireLinkTimeout = 5 * time.Second
					s, peer := heartbeatSession(t, cfg, v, server, Bound)
					heartbeatExpect(t, peer, 0x15, 0, 1)
					time.Sleep(2 * time.Second)
					heartbeatStats(t, s, Bound, 1, 1)             // no accumulated ticks
					heartbeatSend(t, peer, 0x80000015, 0, 91, "") // unsolicited response cannot satisfy probe
					heartbeatSend(t, peer, 0x15, 0, 1, "")        // same sequence in the independent peer direction
					heartbeatExpect(t, peer, 0x80000015, 0, 1)
					heartbeatStats(t, s, Bound, 1, 1)
					heartbeatSend(t, peer, 0x80000015, 0, 1, "")
					heartbeatStats(t, s, Bound, 1, 0)
					heartbeatSend(t, peer, 0x80000015, 0, 1, "") // duplicate cannot create a new probe
					time.Sleep(time.Second - time.Nanosecond)
					heartbeatStats(t, s, Bound, 1, 0)
					heartbeatExpect(t, peer, 0x15, 0, 2)
					heartbeatSend(t, peer, 0x80000015, 0, 2, "")
					heartbeatStats(t, s, Bound, 2, 0)
				})
			})
		}
	}
}

func TestHeartbeatFullWindowSkipsWithoutSequenceOrFailure(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		cfg := heartbeatConfig()
		cfg.Window = 1
		logger, logs := captureLogger(slog.LevelDebug)
		cfg.Logger = logger
		s, peer := heartbeatSession(t, cfg, pdu.V50, false, Bound)
		done := make(chan error, 1)
		go func() { _, err := s.Request(context.Background(), &pdu.SubmitSM{}); done <- err }()
		frame := heartbeatFrame(t, peer)
		if binary.BigEndian.Uint32(frame[4:]) != 4 {
			t.Fatalf("submit %x", frame)
		}
		time.Sleep(10 * time.Second)
		heartbeatStats(t, s, Bound, 1, 1)
		skipped := 0
		for _, r := range logs.records(t) {
			if r["event"] == "enquire_link_skipped" {
				skipped++
				if r["reason"] != "overloaded" || r["level"] != "DEBUG" {
					t.Fatal(r)
				}
			}
		}
		if skipped != 10 {
			t.Fatalf("skipped %d probes", skipped)
		}
		heartbeatSend(t, peer, 0x80000004, 0, 1, "00")
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		heartbeatExpect(t, peer, 0x15, 0, 2)
		heartbeatSend(t, peer, 0x80000015, 0, 2, "")
		heartbeatStats(t, s, Bound, 2, 0)
	})
}

func TestHeartbeatQueuedExpiryIsLocalPressure(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		cfg := heartbeatConfig()
		cfg.Window = 2
		cfg.EnquireLinkTimeout = 2 * time.Second
		s, peer := heartbeatSession(t, cfg, pdu.V50, false, Bound)
		done := make(chan error, 1)
		go func() { _, err := s.Request(context.Background(), &pdu.SubmitSM{}); done <- err }()
		synctest.Wait() // business frame is blocked on the sole writer
		time.Sleep(3500 * time.Millisecond)
		heartbeatStats(t, s, Bound, 2, 1)
		if len(s.slots) != 1 {
			t.Fatalf("queued probe retained a slot: %d", len(s.slots))
		}
		heartbeatFrame(t, peer)
		heartbeatSend(t, peer, 0x80000004, 0, 1, "00")
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		heartbeatExpect(t, peer, 0x15, 0, 3)
		heartbeatSend(t, peer, 0x80000015, 0, 3, "")
		heartbeatStats(t, s, Bound, 3, 0)
	})
}

func TestHeartbeatSentTimeoutPreservesBusinessUncertainty(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		cfg := heartbeatConfig()
		logger, logs := captureLogger(slog.LevelDebug)
		cfg.Logger = logger
		s, peer := heartbeatSession(t, cfg, pdu.V50, false, Bound)
		type result struct {
			r Result
			e error
		}
		done := make(chan result, 1)
		go func() { r, e := s.Request(context.Background(), &pdu.SubmitSM{}); done <- result{r, e} }()
		heartbeatFrame(t, peer)
		heartbeatExpect(t, peer, 0x15, 0, 2)
		heartbeatSend(t, peer, 0x80000015, 0, 99, "") // wrong sequence is ignored until the real deadline
		<-s.Done()
		s.Wait()
		if !errors.Is(s.Err(), ErrEnquireLinkTimeout) {
			t.Fatal(s.Err())
		}
		got := <-done
		if !errors.Is(got.e, ErrEnquireLinkTimeout) || got.r.Outcome != Written || got.r.BytesWritten <= 16 {
			t.Fatal(got)
		}
		if s.seq != 2 || len(s.slots) != 0 {
			t.Fatal("unexpected retry or leaked admission")
		}
		closed := false
		for _, r := range logs.records(t) {
			if r["event"] == "session_closed" && r["reason"] == "enquire_link_timeout" {
				closed = true
			}
		}
		if !closed {
			t.Fatal("missing safe heartbeat terminal diagnostic")
		}
	})
}

func TestHeartbeatInvalidResponses(t *testing.T) {
	for _, tc := range []struct {
		name            string
		command, status uint32
		peerError       bool
	}{
		{"negative enquiry", 0x80000015, 8, true}, {"generic nack", 0x80000000, 3, true}, {"wrong opcode", 0x80000006, 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				s, peer := heartbeatSession(t, heartbeatConfig(), pdu.V50, false, Bound)
				heartbeatExpect(t, peer, 0x15, 0, 1)
				heartbeatSend(t, peer, tc.command, tc.status, 1, "")
				<-s.Done()
				s.Wait()
				if tc.peerError {
					e, ok := errors.AsType[*PeerError](s.Err())
					if !ok || e.Command != pdu.EnquireLinkID || uint32(e.Status) != tc.status {
						t.Fatal(s.Err())
					}
				} else if !errors.Is(s.Err(), ErrResponse) {
					t.Fatal(s.Err())
				}
			})
		})
	}
}

func TestHeartbeatIndependentDeadlines(t *testing.T) {
	for _, tc := range []struct {
		name        string
		read, write time.Duration
		wantProbe   bool
	}{
		{"active write probe deadline", time.Hour, time.Hour, true},
		{"read deadline earlier", 500 * time.Millisecond, time.Hour, false},
		{"write deadline earlier", time.Hour, 500 * time.Millisecond, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				cfg := heartbeatConfig()
				cfg.ReadTimeout = tc.read
				cfg.WriteTimeout = tc.write
				s, _ := heartbeatSession(t, cfg, pdu.V50, false, Bound) // peer never consumes probe
				<-s.Done()
				s.Wait()
				if tc.wantProbe {
					if !errors.Is(s.Err(), ErrEnquireLinkTimeout) {
						t.Fatal(s.Err())
					}
				} else if e, ok := errors.AsType[net.Error](s.Err()); !ok || !e.Timeout() {
					t.Fatal(s.Err())
				}
				if len(s.slots) != 0 {
					t.Fatal("probe slot retained after Wait")
				}
			})
		})
	}
}

func TestHeartbeatStopsForLocalAndRemoteUnbind(t *testing.T) {
	for _, local := range []bool{false, true} {
		t.Run(fmt.Sprintf("local=%t", local), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				s, peer := heartbeatSession(t, heartbeatConfig(), pdu.V50, false, Bound)
				heartbeatExpect(t, peer, 0x15, 0, 1) // leave response pending
				if local {
					done := make(chan error, 1)
					go func() { done <- s.Shutdown(context.Background()) }()
					heartbeatExpect(t, peer, 6, 0, 2)
					time.Sleep(5 * time.Second)
					heartbeatStats(t, s, Unbinding, 2, 1)
					heartbeatSend(t, peer, 0x80000006, 0, 2, "")
					if err := <-done; err != nil {
						t.Fatal(err)
					}
				} else {
					heartbeatSend(t, peer, 6, 0, 87, "")
					heartbeatExpect(t, peer, 0x80000006, 0, 87)
				}
				<-s.Done()
				s.Wait()
				if !errors.Is(s.Err(), ErrClosed) || len(s.slots) != 0 {
					t.Fatal(s.Err(), len(s.slots))
				}
			})
		})
	}
}

func TestHeartbeatRemoteUnbindDuringBlockedWrite(t *testing.T) {
	for _, queued := range []bool{false, true} {
		t.Run(fmt.Sprintf("queued=%t", queued), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				s, peer := heartbeatSession(t, heartbeatConfig(), pdu.V50, false, Bound)
				businessDone := make(chan struct{})
				if queued {
					go func() { s.Request(context.Background(), &pdu.SubmitSM{}); close(businessDone) }()
				}
				synctest.Wait()
				time.Sleep(time.Second)
				synctest.Wait()
				heartbeatSend(t, peer, 6, 0, 99, "")
				// A probe that already started must finish its frame, even after its timer
				// expires. Unbinding owns retirement now, under the existing write deadline.
				time.Sleep(5 * time.Second)
				if s.State() != Unbinding {
					t.Fatal(s.State(), s.Err())
				}
				if queued {
					f := heartbeatFrame(t, peer)
					if binary.BigEndian.Uint32(f[4:]) != 4 {
						t.Fatalf("expected business frame: %x", f)
					}
				} else {
					heartbeatExpect(t, peer, 0x15, 0, 1)
				}
				heartbeatExpect(t, peer, 0x80000006, 0, 99) // queued probe must not start before ACK or after it
				<-s.Done()
				s.Wait()
				if queued {
					<-businessDone
				}
				if !errors.Is(s.Err(), ErrClosed) || len(s.slots) != 0 {
					t.Fatal(s.Err(), len(s.slots))
				}
			})
		})
	}
}

func TestHeartbeatConcurrentTrafficAndClose(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		cfg := heartbeatConfig()
		client, server := sessionPair(t, cfg)
		var work sync.WaitGroup
		for range 24 {
			work.Go(func() {
				for range 3 {
					if _, err := client.Request(context.Background(), &pdu.EnquireLink{}); err != nil {
						t.Error(err)
						return
					}
					time.Sleep(1100 * time.Millisecond)
				}
			})
		}
		work.Wait()
		synctest.Wait()
		if client.seq <= 73 || server.seq < 2 {
			t.Fatal("automatic probes did not overlap traffic", client.seq, server.seq)
		}
		for range 16 {
			work.Go(func() { client.Close(); server.Close() })
		}
		work.Wait()
		client.Wait()
		server.Wait()
		if len(client.slots) != 0 || len(server.slots) != 0 {
			t.Fatal("automatic requests outlived Wait")
		}
	})
}

func TestHeartbeatProbeTimeoutOverridesRequestTimeout(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		cfg := heartbeatConfig()
		cfg.RequestTimeout = 10 * time.Millisecond
		s, peer := heartbeatSession(t, cfg, pdu.V50, false, Bound)
		heartbeatExpect(t, peer, 0x15, 0, 1)
		time.Sleep(2 * time.Second)
		heartbeatStats(t, s, Bound, 1, 1)
		heartbeatSend(t, peer, 0x80000015, 0, 1, "")
		heartbeatStats(t, s, Bound, 1, 0)
	})
}

func TestHeartbeatCloseBeforeBindJoinsScheduler(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s, _ := heartbeatSession(t, heartbeatConfig(), pdu.V50, false, Connected)
		s.Close()
		s.Wait()
		if s.State() != Closed || len(s.slots) != 0 || s.seq != 0 {
			t.Fatal("pre-bind scheduler survived Close/Wait")
		}
	})
}

// Response correlation can finish before the transport Write returns. The
// probe deadline still bounds that write join, just like an ordinary Request.
func TestHeartbeatResponseBeforeWriteCompletion(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		a, peer := net.Pipe()
		defer peer.Close()
		conn := &faultConn{Conn: a, write: func(b []byte) (int, error) {
			n, err := a.Write(b)
			if err != nil {
				return n, err
			}
			// Model a transport that accepted a complete frame but has not returned
			// yet. This second write blocks until Session closes the connection.
			_, err = a.Write([]byte{0})
			return n, err
		}}
		s, err := newSession(conn, testCodec(t), heartbeatConfig(), false, Bound, Transmitter)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { s.Close(); s.Wait() }()
		heartbeatExpect(t, peer, 0x15, 0, 1)
		heartbeatSend(t, peer, 0x80000015, 0, 1, "")
		<-s.Done()
		s.Wait()
		if !errors.Is(s.Err(), ErrEnquireLinkTimeout) || len(s.slots) != 0 {
			t.Fatal(s.Err(), len(s.slots))
		}
	})
}
