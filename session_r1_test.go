package smpp

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/skill215/smppkit/codec"
	"github.com/skill215/smppkit/message"
	"github.com/skill215/smppkit/pdu"
	"github.com/skill215/smppkit/textcodec"
)

func r1Wire(s string) []byte {
	b, err := hex.DecodeString(s)
	if err != nil {
		panic(err)
	}
	return b
}

// RV17: every connection performs a real bind with a byte-oriented peer. Codec
// is used only to prove the response is valid for its version before testing the
// stricter negotiated Session policy; it never manufactures the peer's frames.
func TestR1ResponseTLVPolicy(t *testing.T) {
	for _, v := range []pdu.Version{pdu.V34, pdu.V50} {
		for _, advertised := range []bool{false, true} {
			for _, tc := range []struct {
				name, request, response string
				body                    pdu.Body
				optional, v50           bool
			}{
				{"data-plain", "0000001a00000103000000000000000200000000000000000000", "0000001180000103000000000000000200", &pdu.DataSM{}, false, false},
				{"data-customer", "0000001a00000103000000000000000200000000000000000000", "000000168000010300000000000000020015000001aa", &pdu.DataSM{}, true, false},
				{"data-negative-customer", "0000001a00000103000000000000000200000000000000000000", "000000168000010300000008000000020015000001aa", &pdu.DataSM{}, true, false},
				{"enquire-plain", "00000010000000150000000000000002", "00000010800000150000000000000002", &pdu.EnquireLink{}, false, false},
				{"enquire-standard", "00000010000000150000000000000002", "000000158000001500000000000000020428000101", &pdu.EnquireLink{}, true, true},
				{"enquire-negative", "00000010000000150000000000000002", "000000158000001500000008000000020428000101", &pdu.EnquireLink{}, true, true},
				{"nack-plain", "00000010000000150000000000000002", "00000010800000000000000800000002", &pdu.EnquireLink{}, false, false},
				{"nack-standard", "00000010000000150000000000000002", "000000158000000000000008000000020428000101", &pdu.EnquireLink{}, true, true},
			} {
				if tc.v50 && v != pdu.V50 {
					continue
				}
				t.Run(fmt.Sprintf("%x/advertised=%t/%s", v, advertised, tc.name), func(t *testing.T) {
					c, err := codec.New(codec.Config{Version: v})
					if err != nil {
						t.Fatal(err)
					}
					response, err := c.Decode(r1Wire(tc.response))
					if err != nil {
						t.Fatalf("fixture must be Codec-valid: %v", err)
					}
					a, b := net.Pipe()
					s, err := NewSession(a, c, Config{})
					if err != nil {
						t.Fatal(err)
					}
					defer func() { s.Close(); b.Close(); s.Wait() }()
					ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
					defer cancel()
					_ = b.SetDeadline(time.Now().Add(3 * time.Second))
					peer := make(chan error, 1)
					go func() {
						readExact := func(want []byte) error {
							got := make([]byte, len(want))
							if _, e := io.ReadFull(b, got); e != nil {
								return e
							}
							if !bytes.Equal(got, want) {
								return fmt.Errorf("got %x want %x", got, want)
							}
							return nil
						}
						if e := readExact(r1Wire(fmt.Sprintf("00000017000000020000000000000001000000%02x000000", v))); e != nil {
							peer <- e
							return
						}
						bind := "000000138000000200000000000000016d6300"
						if advertised {
							bind = fmt.Sprintf("000000188000000200000000000000016d630002100001%02x", v)
						}
						if _, e := b.Write(r1Wire(bind)); e != nil {
							peer <- e
							return
						}
						if e := readExact(r1Wire(tc.request)); e != nil {
							peer <- e
							return
						}
						_, e := b.Write(r1Wire(tc.response))
						peer <- e
					}()
					if _, err = s.Bind(ctx, &pdu.BindTransmitter{BindFields: pdu.BindFields{InterfaceVersion: v}}); err != nil {
						t.Fatal(err)
					}
					if s.TLVEnabled() != advertised {
						t.Fatal("bind negotiation mismatch")
					}
					result, err := s.Request(ctx, tc.body)
					if e := <-peer; e != nil {
						t.Fatal(e)
					}
					if tc.optional && !advertised {
						if !errors.Is(err, ErrState) || result.Outcome != Written || result.Response.Body != nil || s.State() != Closed {
							t.Fatalf("unnegotiated response: %+v err=%v state=%v", result, err, s.State())
						}
						return
					}
					peerError, isPeerError := errors.AsType[*PeerError](err)
					if response.Status == pdu.OK && err != nil || response.Status != pdu.OK && (!isPeerError || peerError.Status != response.Status) {
						t.Fatalf("response error: %v", err)
					}
					if result.Outcome != ResponseReceived || result.Response.Status != response.Status || result.Response.Body.CommandID() != response.Body.CommandID() || s.State() != Bound {
						t.Fatalf("lost response or healthy session: %+v", result)
					}
					encoded, err := c.Append(nil, result.Response)
					if err != nil || !bytes.Equal(encoded, r1Wire(tc.response)) {
						t.Fatalf("response metadata lost: %x %v", encoded, err)
					}
				})
			}
		}
	}
}

// The peer may read the frame and respond before Write returns. Model that
// ordering without ignoring transport Close or WriteTimeout. Only the writer
// sets/reads deadline, and tests release the gate through a channel.
type r1WriteGate struct {
	net.Conn
	closed, release chan struct{}
	once            sync.Once
	deadline        time.Time
}

func (c *r1WriteGate) SetWriteDeadline(d time.Time) error {
	c.deadline = d
	return c.Conn.SetWriteDeadline(d)
}
func (c *r1WriteGate) Write(b []byte) (int, error) {
	n, err := c.Conn.Write(b)
	if err != nil {
		return n, err
	}
	timer := time.NewTimer(time.Until(c.deadline))
	defer timer.Stop()
	select {
	case <-c.release:
		return n, nil
	case <-c.closed:
		return n, net.ErrClosed
	case <-timer.C:
		return n, os.ErrDeadlineExceeded
	}
}
func (c *r1WriteGate) Close() error { c.once.Do(func() { close(c.closed) }); return c.Conn.Close() }

// RV18: fake time and Wait establish dispatch-before-cancellation and avoid
// relying on scheduler speed. All transport I/O is through in-memory pipes.
func TestR1ResponseBeforeWriteCompletion(t *testing.T) {
	for _, mode := range []string{"cancel", "request-timeout", "context-deadline", "completed-write", "peer-error", "crossed-unbind"} {
		t.Run(mode, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				a, b := net.Pipe()
				conn := &r1WriteGate{Conn: a, closed: make(chan struct{}), release: make(chan struct{})}
				s, err := newSession(conn, testCodec(t), Config{RequestTimeout: 30 * time.Second, WriteTimeout: time.Hour}, false, Bound, Transmitter)
				if err != nil {
					t.Fatal(err)
				}
				defer func() { s.Close(); b.Close(); s.Wait() }()
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				if mode == "context-deadline" {
					var deadlineCancel context.CancelFunc
					ctx, deadlineCancel = context.WithTimeout(ctx, 10*time.Second)
					defer deadlineCancel()
				}
				var body pdu.Body = &pdu.EnquireLink{}
				request := "00000010000000150000000000000001"
				response := "00000010800000150000000000000001"
				if mode == "peer-error" {
					response = "00000010800000150000000800000001"
				}
				if mode == "crossed-unbind" {
					body = &pdu.Unbind{}
					request, response = "00000010000000060000000000000001", "00000010800000060000000000000001"
				}
				type completion struct {
					result Result
					err    error
				}
				done := make(chan completion, 1)
				go func() { r, e := s.Request(ctx, body); done <- completion{r, e} }()
				wire := make([]byte, 16)
				if _, err := io.ReadFull(b, wire); err != nil || !bytes.Equal(wire, r1Wire(request)) {
					t.Fatalf("request: %x %v", wire, err)
				}
				if mode == "crossed-unbind" {
					if _, err := b.Write(r1Wire("00000010000000060000000000000009")); err != nil {
						t.Fatal(err)
					}
				}
				if _, err := b.Write(r1Wire(response)); err != nil {
					t.Fatal(err)
				}
				synctest.Wait()
				s.mu.Lock()
				pending := len(s.pending)
				s.mu.Unlock()
				if pending != 0 {
					t.Fatal("response was not dispatched")
				}
				switch mode {
				case "request-timeout":
					time.Sleep(31 * time.Second)
				case "context-deadline":
					time.Sleep(11 * time.Second)
				case "completed-write":
					close(conn.release)
				default:
					cancel()
				}
				synctest.Wait()
				var got completion
				select {
				case got = <-done:
				default:
					t.Fatal("Request did not finish after cancellation/deadline or write completion")
				}
				if got.result.Outcome != ResponseReceived || got.result.BytesWritten != 16 || got.result.Response.Sequence != 1 || got.result.Response.Body.CommandID() != body.CommandID()|pdu.ResponseBit {
					t.Fatalf("lost received response or byte count: %+v", got)
				}
				if mode == "peer-error" {
					if peer, ok := errors.AsType[*PeerError](got.err); !ok || peer.Status != 8 {
						t.Fatalf("lost peer error: %+v", got)
					}
				} else if mode == "crossed-unbind" {
					if !errors.Is(got.err, context.Canceled) {
						t.Fatalf("unfinished peer ACK reported success: %+v", got)
					}
				} else if got.err != nil {
					t.Fatalf("response must win request outcome: %+v", got)
				}
				if mode == "completed-write" {
					cancel()
					synctest.Wait()
					if s.State() != Bound {
						t.Fatal("completed write cancellation closed session")
					}
				} else {
					if s.State() != Closed {
						t.Fatal("active writer was not retired")
					}
					s.Wait()
				}
				if len(s.slots) != 0 {
					t.Fatal("request window slot leaked")
				}
			})
		})
	}
}

// Detect any transport write without a peer reader or a wall-clock wait. The
// atomic counter is shared with Session's writer; queue snapshots alone could
// miss an entry already consumed by that goroutine.
type r1PreflightConn struct {
	net.Conn
	writes atomic.Int32
}

func (c *r1PreflightConn) Write([]byte) (int, error) {
	c.writes.Add(1)
	return 0, errors.New("unexpected transport write during preflight")
}

func TestR1V33Latin1SendPreflight(t *testing.T) {
	c, err := codec.New(codec.Config{Version: pdu.V33})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, text string
		policy     message.Policy
	}{
		{"direct", "@", message.Policy{Encoding: textcodec.Latin1}},
		// ÿ is absent from GSM7, so Latin-1 must be selected via fallback.
		{"selected-fallback", "ÿ", message.Policy{Encoding: textcodec.GSM7, Fallback: textcodec.Latin1}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, b := net.Pipe()
			conn := &r1PreflightConn{Conn: a}
			s, err := newSession(conn, c, Config{}, false, Bound, Transmitter)
			if err != nil {
				a.Close()
				b.Close()
				t.Fatal(err)
			}
			defer func() { s.Close(); b.Close(); s.Wait() }()
			r, err := s.SendText(t.Context(), TextMessage{Text: tc.text}, tc.policy)
			if !errors.Is(err, message.ErrMetadata) || len(r.Segments) != 0 {
				t.Fatalf("unexpected preflight: %+v %v", r, err)
			}
			s.mu.Lock()
			if s.seq != 0 || len(s.pending) != 0 || len(s.requests) != 0 || len(s.slots) != 0 || s.state != Bound {
				t.Error("invalid coding reached session admission")
			}
			s.mu.Unlock()
			s.Close()
			s.Wait() // include any delayed writer activity in the observation
			if got := conn.writes.Load(); got != 0 {
				t.Fatalf("preflight performed %d transport writes", got)
			}
		})
	}
}
