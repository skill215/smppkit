// SPDX-AI-Disclosure: ai-generated
// SPDX-AI-Model: GPT-6.0 Astra
// SPDX-AI-Provider: OpenAI
// SPDX-AI-Scope: Test code generated and revised with Codex under maintainer direction.
// SPDX-AI-Date: 2026-10-29

package smpp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/skill215/smppkit/codec"
	"github.com/skill215/smppkit/pdu"
	"github.com/skill215/smppkit/tlv"
)

type logCapture struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (c *logCapture) Write(b []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.b.Write(b)
}

func (c *logCapture) records(t *testing.T) []map[string]any {
	t.Helper()
	c.mu.Lock()
	defer c.mu.Unlock()
	var records []map[string]any
	d := json.NewDecoder(bytes.NewReader(c.b.Bytes()))
	for {
		var record map[string]any
		if err := d.Decode(&record); err == io.EOF {
			return records
		} else if err != nil {
			t.Fatal(err)
		}
		records = append(records, record)
	}
}

func captureLogger(level slog.Level) (*slog.Logger, *logCapture) {
	c := new(logCapture)
	return slog.New(slog.NewJSONHandler(c, &slog.HandlerOptions{Level: level})), c
}

func TestLoggingDefaultDiscardsWithoutUsingGlobalLogger(t *testing.T) {
	global, captured := captureLogger(slog.LevelDebug)
	previous := slog.Default()
	slog.SetDefault(global)
	defer slog.SetDefault(previous)
	client, server := sessionPair(t, Config{})
	if client.cfg.Logger.Enabled(t.Context(), slog.LevelError) {
		t.Fatal("nil configuration did not normalize to discard")
	}
	if _, err := client.Request(t.Context(), &pdu.EnquireLink{}); err != nil {
		t.Fatal(err)
	}
	client.Close()
	server.Close()
	client.Wait()
	server.Wait()
	if got := captured.records(t); len(got) != 0 {
		t.Fatalf("library used global logger: %v", got)
	}
}

// Reuse the independent byte peer rather than checking two logging-enabled
// copies of the same codec against each other. No automatic scheduler is added.
func TestLoggingIndependentEnquireLinkVersions(t *testing.T) {
	for _, v := range []pdu.Version{pdu.V33, pdu.V34, pdu.V50} {
		t.Run(fmt.Sprintf("%x", v), func(t *testing.T) {
			logger, captured := captureLogger(slog.LevelDebug)
			logger = logger.With("application", "test")
			c, err := codec.New(codec.Config{Version: v})
			if err != nil {
				t.Fatal(err)
			}
			a, peer := net.Pipe()
			s, err := newSession(a, c, Config{Logger: logger}, false, Bound, Transmitter)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { s.Close(); peer.Close(); s.Wait() }()
			_ = peer.SetDeadline(time.Now().Add(3 * time.Second))
			done := make(chan error, 1)
			go func() {
				got := make([]byte, 16)
				_, err := io.ReadFull(peer, got)
				if err == nil && !bytes.Equal(got, independentPacket(0x15, 0, 1, "")) {
					err = fmt.Errorf("unexpected enquiry: %x", got)
				}
				if err == nil {
					_, err = peer.Write(independentPacket(0x80000015, 0, 1, ""))
				}
				done <- err
			}()
			r, err := s.Request(t.Context(), &pdu.EnquireLink{})
			if err != nil || r.Outcome != ResponseReceived {
				t.Fatal(r, err)
			}
			if err := <-done; err != nil {
				t.Fatal(err)
			}
			s.Close()
			s.Wait()
			count := 0
			for _, record := range captured.records(t) {
				if record["application"] != "test" || record["version"] != float64(v) || record["session_id"] != s.namespace {
					t.Fatalf("lost attributes: %v", record)
				}
				if record["event"] == "request_completed" {
					count++
					if record["command_id"] != float64(0x15) || record["sequence"] != float64(1) || record["outcome"] != float64(ResponseReceived) || record["bytes_written"] != float64(16) || record["reason"] != "ok" {
						t.Fatalf("incorrect result metadata: %v", record)
					}
				}
			}
			if count != 1 {
				t.Fatalf("request records = %d", count)
			}
		})
	}
}

// Error() would disclose application data. No logging path may call it.
type privateLogError struct{}

func (privateLogError) Error() string { panic("must not format application errors") }

type diagnosticTrapError struct{ privateLogError }

func (diagnosticTrapError) Is(error) bool { panic("disabled diagnostics must not inspect errors") }

type loggingFailedListener struct{}

func (loggingFailedListener) Accept() (net.Conn, error) { return nil, privateLogError{} }
func (loggingFailedListener) Close() error              { return nil }
func (loggingFailedListener) Addr() net.Addr            { return nil }

func TestLoggingListenerFailurePreservesCallerContext(t *testing.T) {
	var calls int
	logger := slog.New(callbackLogHandler{
		enabled: func(context.Context, slog.Level) bool { return true },
		handle: func(ctx context.Context, r slog.Record) error {
			if ctx.Err() != nil {
				t.Error("internal Serve cleanup canceled diagnostic context")
			}
			r.Attrs(func(a slog.Attr) bool {
				if a.Key == "stage" && a.Value.String() == "listen_accept" {
					calls++
				}
				return true
			})
			return nil
		},
	})
	srv, err := NewServer(ServerConfig{
		Profiles: []*codec.Codec{testCodec(t)}, Session: Config{Logger: logger},
		Authenticate: func(context.Context, BindInfo) (BindDecision, error) { return BindDecision{}, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	err = srv.Serve(t.Context(), loggingFailedListener{}, func(context.Context, *Session) {})
	if _, ok := err.(privateLogError); !ok || calls != 1 {
		t.Fatal("listener failure was not returned and logged once")
	}
}

func TestLoggingDefaultDoesNotInspectAuthenticationErrors(t *testing.T) {
	srv, err := NewServer(ServerConfig{
		Profiles:     []*codec.Codec{testCodec(t)},
		Authenticate: func(context.Context, BindInfo) (BindDecision, error) { return BindDecision{}, diagnosticTrapError{} },
	})
	if err != nil {
		t.Fatal(err)
	}
	a, peer := net.Pipe()
	defer peer.Close()
	done := make(chan error, 1)
	go func() { _, err := srv.Accept(t.Context(), a); done <- err }()
	_ = peer.SetDeadline(time.Now().Add(3 * time.Second))
	_, writeErr := peer.Write(independentPacket(9, 0, 1, "75736572007077736563726574000050000000"))
	if writeErr != nil {
		peer.Close()
		<-done
		t.Fatal(writeErr)
	}
	if _, ok := (<-done).(diagnosticTrapError); !ok {
		t.Fatal("authentication error identity changed")
	}
}

func TestLoggingServeAuthenticationFailure(t *testing.T) {
	logger, captured := captureLogger(slog.LevelWarn)
	srv, err := NewServer(ServerConfig{
		Profiles: []*codec.Codec{testCodec(t)}, Session: Config{Logger: logger},
		Authenticate: func(context.Context, BindInfo) (BindDecision, error) { return BindDecision{}, privateLogError{} },
	})
	if err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	var handled atomic.Bool
	go func() { done <- srv.Serve(ctx, ln, func(context.Context, *Session) { handled.Store(true) }) }()
	conn, err := net.DialTimeout("tcp", ln.Addr().String(), time.Second)
	if err != nil {
		cancel()
		<-done
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
	// Independently authored bind_transceiver: user, pwsecret, empty system_type,
	// interface 5.0, TON/NPI zero and empty address_range.
	_, err = conn.Write(independentPacket(9, 0, 1, "75736572007077736563726574000050000000"))
	if err != nil {
		cancel()
		<-done
		t.Fatal(err)
	}
	_, err = conn.Read(make([]byte, 1))
	if err != io.EOF {
		cancel()
		<-done
		t.Fatalf("authentication did not close transport: %v", err)
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	records := captured.records(t)
	if handled.Load() || len(records) != 1 || records[0]["stage"] != "authenticate" || records[0]["reason"] != "other" || records[0]["level"] != "WARN" {
		t.Fatalf("missing/incorrect setup failure: %v", records)
	}
	encoded, _ := json.Marshal(records)
	if bytes.Contains(encoded, []byte("pwsecret")) || bytes.Contains(encoded, []byte("user")) {
		t.Fatalf("credential leak: %s", encoded)
	}
}

func TestLoggingOverloadAndPayloadPrivacy(t *testing.T) {
	logger, captured := captureLogger(slog.LevelWarn)
	s, peer := noticeSession(t, true, Bound, Transceiver, Config{Logger: logger, InboundLimit: 1})
	_ = peer.SetDeadline(time.Now().Add(3 * time.Second))
	for seq := uint32(1); seq <= 2; seq++ {
		data, err := s.codec.Append(nil, pdu.PDU{Sequence: seq, Body: &pdu.SubmitSM{SMFields: pdu.SMFields{ShortMessage: []byte("private message")}}})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := peer.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	s.Wait()
	if !errors.Is(s.Err(), ErrOverloaded) {
		t.Fatal(s.Err())
	}
	records := captured.records(t)
	if len(records) != 1 || records[0]["event"] != "session_closed" || records[0]["reason"] != "overloaded" {
		t.Fatalf("unexpected warning records: %v", records)
	}
	encoded, _ := json.Marshal(records)
	if bytes.Contains(encoded, []byte("private message")) {
		t.Fatal("payload leaked")
	}
}

type callbackLogHandler struct {
	slog.Handler
	enabled func(context.Context, slog.Level) bool
	handle  func(context.Context, slog.Record) error
}

func (h callbackLogHandler) Enabled(ctx context.Context, l slog.Level) bool  { return h.enabled(ctx, l) }
func (h callbackLogHandler) Handle(ctx context.Context, r slog.Record) error { return h.handle(ctx, r) }

func TestLoggingCallbacksOutsideLocksAndConcurrentRequests(t *testing.T) {
	var current atomic.Pointer[Session]
	var calls atomic.Int32
	var canceled atomic.Bool
	type contextKey struct{}
	logger := slog.New(callbackLogHandler{
		enabled: func(context.Context, slog.Level) bool {
			if s := current.Load(); s != nil {
				_ = s.State()
				_ = s.Err()
			}
			return true
		},
		handle: func(ctx context.Context, r slog.Record) error {
			if s := current.Load(); s != nil {
				_ = s.State()
				_ = s.Err()
			}
			r.Attrs(func(a slog.Attr) bool {
				if a.Key == "event" && a.Value.String() == "request_completed" && ctx.Value(contextKey{}) == "request" {
					calls.Add(1)
					if ctx.Err() != nil {
						canceled.Store(true)
					}
				}
				return true
			})
			return errors.New("handler output failure must not alter SMPP results")
		},
	})
	client, _ := sessionPair(t, Config{Logger: logger})
	current.Store(client)
	ctx, cancel := context.WithTimeout(context.WithValue(t.Context(), contextKey{}, "request"), 3*time.Second)
	defer cancel()
	var wg sync.WaitGroup
	for range 24 {
		wg.Go(func() {
			if r, err := client.Request(ctx, &pdu.EnquireLink{}); err != nil || r.Outcome != ResponseReceived {
				t.Errorf("request: %+v %v", r, err)
			}
		})
	}
	wg.Wait()
	if calls.Load() != 24 {
		t.Fatalf("context forwarding/call count: %d", calls.Load())
	}
	if canceled.Load() {
		t.Fatal("internal timeout cleanup replaced the caller context")
	}
}

func TestLoggingCloseRemainsIndependentOfHandler(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		entered, release := make(chan struct{}), make(chan struct{})
		var count atomic.Int32
		logger := slog.New(callbackLogHandler{
			enabled: func(context.Context, slog.Level) bool { return true },
			handle: func(_ context.Context, r slog.Record) error {
				r.Attrs(func(a slog.Attr) bool {
					if a.Key == "event" && a.Value.String() == "session_closed" {
						count.Add(1)
						close(entered)
						<-release
					}
					return true
				})
				return nil
			},
		})
		s, peer := noticeSession(t, false, Bound, Transceiver, Config{Logger: logger})
		defer peer.Close()
		s.Close() // Must return even while the terminal handler is blocked.
		<-entered
		if s.State() != Closed || !errors.Is(s.Err(), ErrClosed) {
			t.Fatal("terminal state unavailable")
		}
		waited := make(chan struct{})
		go func() { s.Wait(); close(waited) }()
		synctest.Wait()
		select {
		case <-waited:
			t.Fatal("Wait did not join the logging work")
		default:
		}
		close(release)
		<-waited
		s.Close()
		if count.Load() != 1 {
			t.Fatal("duplicate terminal event")
		}
	})
}

func TestLoggingDialFailureAndNoAddressDisclosure(t *testing.T) {
	logger, captured := captureLogger(slog.LevelWarn)
	_, err := Dial(t.Context(), "private-address:invalid-port", ClientConfig{Codec: testCodec(t), Bind: &pdu.BindTransceiver{}, Session: Config{Logger: logger}})
	if err == nil {
		t.Fatal("invalid address accepted")
	}
	records := captured.records(t)
	if len(records) != 1 || records[0]["stage"] != "connect" {
		t.Fatal(records)
	}
	encoded, _ := json.Marshal(records)
	if strings.Contains(string(encoded), "private-address") {
		t.Fatal("address leaked")
	}
}

func TestLoggingRequestResultPrivacy(t *testing.T) {
	logger, captured := captureLogger(slog.LevelDebug)
	client, server := sessionPair(t, Config{Logger: logger})
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	for _, status := range []pdu.Status{pdu.OK, pdu.Status(8)} {
		done := make(chan error, 1)
		go func() {
			in, err := server.Receive(ctx)
			if err == nil {
				var body pdu.Body = &pdu.SubmitSMResp{MessageResponseFields: pdu.MessageResponseFields{MessageID: "secret-id"}}
				if status != pdu.OK {
					body = &pdu.Omitted{ID: pdu.SubmitSMRespID}
				}
				err = in.Respond(ctx, status, body)
			}
			done <- err
		}()
		r, err := client.Request(ctx, &pdu.SubmitSM{SMFields: pdu.SMFields{
			Source: pdu.Address{Value: "secret-address"}, ShortMessage: []byte("secret-text"),
			Optional: tlv.Fields{{Tag: 0x1500, Value: []byte("secret-tlv")}},
		}})
		if r.Outcome != ResponseReceived || (err == nil) != (status == pdu.OK) {
			t.Fatal(r, err)
		}
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	client.Close()
	server.Close()
	client.Wait()
	server.Wait()
	records := captured.records(t)
	encoded, _ := json.Marshal(records)
	if bytes.Contains(encoded, []byte("secret-")) {
		t.Fatalf("payload/identity leaked: %s", encoded)
	}
	var rejected bool
	for _, r := range records {
		if r["event"] == "request_completed" && r["reason"] == "peer_rejected" {
			rejected = r["level"] == "WARN" && r["status"] == float64(8) && r["outcome"] == float64(ResponseReceived)
		}
	}
	if !rejected {
		t.Fatalf("missing peer rejection metadata: %s", encoded)
	}
}

func TestLoggingMalformedFrameAndInboundTimeout(t *testing.T) {
	for _, malformed := range []bool{true, false} {
		t.Run(fmt.Sprintf("malformed=%t", malformed), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				logger, captured := captureLogger(slog.LevelWarn)
				s, peer := noticeSession(t, true, Bound, Transceiver, Config{Logger: logger, InboundTimeout: time.Second})
				// A missing submit_sm body is invalid. An enquiry gets an automatic
				// response whose blocked write still has a separate write timeout;
				// use a valid empty-message submission to test the application ACK timer.
				body := ""
				if !malformed {
					body = "0000000000000000000000000000000000"
				}
				if _, err := peer.Write(independentPacket(4, 0, 7, body)); err != nil {
					t.Fatal(err)
				}
				<-s.Done()
				s.Wait()
				want := "inbound_timeout"
				if malformed {
					want = "invalid_pdu"
				}
				records := captured.records(t)
				if len(records) != 1 || records[0]["reason"] != want {
					t.Fatalf("want %s: %v", want, records)
				}
			})
		})
	}
}
