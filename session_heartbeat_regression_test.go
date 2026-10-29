// SPDX-AI-Disclosure: ai-generated
// SPDX-AI-Model: GPT-6.0 Astra
// SPDX-AI-Provider: OpenAI
// SPDX-AI-Scope: Regression tests generated and revised with Codex under maintainer direction.
// SPDX-AI-Date: 2026-10-29

package smpp

import (
	"context"
	"errors"
	"fmt"
	"net"
	"runtime"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/skill215/smppkit/codec"
	"github.com/skill215/smppkit/pdu"
)

// A controllable parent deadline allows both completion signals to be ready
// before the request's join select runs. It obeys context.Context's Err contract.
type heartbeatDeadlineContext struct {
	context.Context
	done chan struct{}
}

func (c *heartbeatDeadlineContext) Done() <-chan struct{} { return c.done }
func (c *heartbeatDeadlineContext) Err() error {
	select {
	case <-c.done:
		return context.DeadlineExceeded
	default:
		return nil
	}
}

// RV22: completed response and write win over a simultaneously observable deadline.
func TestHeartbeatCompletedProbeDeadline(t *testing.T) {
	for _, automatic := range []bool{false, true} {
		t.Run(fmt.Sprintf("automatic=%t", automatic), func(t *testing.T) {
			falseClosures := 0
			for range 32 {
				cfg := heartbeatConfig()
				cfg.EnquireLinkInterval = 0 // drive exactly one shared request path
				s, peer := heartbeatSession(t, cfg, pdu.V50, false, Bound)
				parent := &heartbeatDeadlineContext{Context: context.Background(), done: make(chan struct{})}
				type observation struct {
					r   Result
					err error
				}
				done := make(chan observation, 1)
				go func() { r, e := s.request(parent, &pdu.EnquireLink{}, automatic); done <- observation{r, e} }()
				// Capture the request context while the writer cannot complete: the pipe
				// peer has not yet read a byte. Then join the actual completed wire write.
				var pending *pending
				deadline := time.Now().Add(time.Second)
				for pending == nil {
					s.mu.Lock()
					pending = s.pending[1]
					s.mu.Unlock()
					if time.Now().After(deadline) {
						t.Fatal("request not admitted")
					}
					runtime.Gosched()
				}
				pending.job.mu.Lock()
				requestCtx := pending.job.ctx
				pending.job.mu.Unlock()
				heartbeatExpect(t, peer, 0x15, 0, 1)
				<-pending.job.done
				// Reproduce dispatchResponse's publication point under the same mutex.
				// A correlated, independently decoded literal response is already present
				// before deadline expiry; the requester cannot finish cleanup until unlock.
				packet, err := s.codec.Decode([]byte{0, 0, 0, 16, 0x80, 0, 0, 0x15, 0, 0, 0, 0, 0, 0, 0, 1})
				if err != nil {
					t.Fatal(err)
				}
				s.mu.Lock()
				delete(s.pending, 1)
				pending.response <- packet
				close(parent.done)
				for requestCtx.Err() == nil {
					runtime.Gosched()
				}
				s.mu.Unlock()
				got := <-done
				if got.err != nil || got.r.Outcome != ResponseReceived || got.r.BytesWritten != 16 {
					t.Fatal(got)
				}
				if s.Err() != nil {
					if !errors.Is(s.Err(), ErrEnquireLinkTimeout) {
						t.Fatal(s.Err())
					}
					falseClosures++
				}
				s.Close()
				peer.Close()
				s.Wait()
			}
			if falseClosures != 0 {
				t.Fatalf("%d/32 completed, correlated exchanges returned success but retired the Session as enquire_link timeout", falseClosures)
			}
		})
	}
}

// Delay delivery of the first read to the SMPP reader after a real peer write
// begins. This represents a pre-existing inbound frame crossing a local unbind.
type heartbeatGatedRead struct {
	net.Conn
	entered, release chan struct{}
	once             sync.Once
}

func (c *heartbeatGatedRead) Read(p []byte) (int, error) {
	n, e := c.Conn.Read(p)
	if n > 0 {
		c.once.Do(func() { close(c.entered); <-c.release })
	}
	return n, e
}

// RV23: a frame started before local unbind must not abort graceful shutdown.
func TestHeartbeatCrossesUnbind(t *testing.T) {
	for _, v := range []pdu.Version{pdu.V33, pdu.V34, pdu.V50} {
		for _, serverInitiates := range []bool{false, true} {
			t.Run(fmt.Sprintf("%x/serverInitiates=%t", v, serverInitiates), func(t *testing.T) {
				synctest.Test(t, func(t *testing.T) {
					c, e := codec.New(codec.Config{Version: v})
					if e != nil {
						t.Fatal(e)
					}
					a, b := net.Pipe()
					gated := &heartbeatGatedRead{Conn: a, entered: make(chan struct{}), release: make(chan struct{})}
					var release sync.Once
					cfg := heartbeatConfig()
					cfg.EnquireLinkInterval = 0
					initiating, e := newSession(gated, c, cfg, serverInitiates, Bound, Transmitter)
					if e != nil {
						t.Fatal(e)
					}
					peer, e := newSession(b, c, heartbeatConfig(), !serverInitiates, Bound, Transmitter)
					if e != nil {
						t.Fatal(e)
					}
					defer func() {
						release.Do(func() { close(gated.release) })
						initiating.Close()
						peer.Close()
						initiating.Wait()
						peer.Wait()
					}()
					<-gated.entered // peer automatic probe has started its frame before unbind
					done := make(chan error, 1)
					go func() { done <- initiating.Shutdown(context.Background()) }()
					synctest.Wait()
					if initiating.State() != Unbinding || peer.State() != Unbinding {
						t.Fatal("unbind not crossed with active probe", initiating.State(), peer.State())
					}
					release.Do(func() { close(gated.release) })
					err := <-done
					if err != nil {
						t.Fatalf("pre-existing peer probe aborts graceful unbind: %v (initiator=%v, peer=%v)", err, initiating.Err(), peer.Err())
					}
				})
			})
		}
	}
}

// Independent byte peers verify the shutdown policy itself: bound enquiries
// still get replies, but enquiries during unbind produce no writes or admission.
// A following business frame is still rejected; a following unbind response can
// complete shutdown. No shared Codec is used to generate or inspect these bytes.
func TestHeartbeatUnbindingDrainsEnquiries(t *testing.T) {
	for _, v := range []pdu.Version{pdu.V33, pdu.V34, pdu.V50} {
		for _, business := range []bool{false, true} {
			t.Run(fmt.Sprintf("%x/business=%t", v, business), func(t *testing.T) {
				synctest.Test(t, func(t *testing.T) {
					s, peer := heartbeatSession(t, heartbeatConfig(), v, true, Bound)
					heartbeatSend(t, peer, 0x15, 0, 97, "")
					heartbeatExpect(t, peer, 0x80000015, 0, 97)
					var shutdownErr error
					done := make(chan struct{})
					go func() { shutdownErr = s.Shutdown(context.Background()); close(done) }()
					defer func() { s.Close(); peer.Close(); <-done; s.Wait() }()
					heartbeatExpect(t, peer, 6, 0, 1)
					for seq := uint32(98); seq < 101; seq++ {
						heartbeatSend(t, peer, 0x15, 0, seq, "")
					}
					heartbeatStats(t, s, Unbinding, 1, 1)
					if r, err := s.Request(context.Background(), &pdu.EnquireLink{}); !errors.Is(err, ErrState) || r.Outcome != NotWritten {
						t.Fatal("outbound enquiry admitted after unbind", r, err)
					}
					// A short virtual read deadline proves there is no control ACK or
					// restarted automatic probe waiting on the serialized writer.
					if err := peer.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
						t.Fatal(err)
					}
					var buf [1]byte
					n, err := peer.Read(buf[:])
					if e, ok := errors.AsType[net.Error](err); n != 0 || !ok || !e.Timeout() {
						t.Fatal("unexpected write while unbinding", n, err)
					}
					heartbeatStats(t, s, Unbinding, 1, 1)
					if business {
						// Minimal submit_sm: empty C-octets, zero scalar fields and
						// zero sm_length (17 mandatory body octets in all profiles).
						heartbeatSend(t, peer, 4, 0, 101, "0000000000000000000000000000000000")
					} else {
						heartbeatSend(t, peer, 0x80000006, 0, 1, "")
					}
					<-done
					if business {
						if !errors.Is(shutdownErr, ErrState) {
							t.Fatal("business admission reopened", shutdownErr)
						}
					} else if shutdownErr != nil {
						t.Fatal(shutdownErr)
					}
					s.Wait()
					if len(s.slots) != 0 || len(s.inbound) != 0 || len(s.incoming) != 0 {
						t.Fatal("shutdown retained admission")
					}
				})
			})
		}
	}
}
