package smpp

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/skill215/smppkit/codec"
	"github.com/skill215/smppkit/pdu"
	"github.com/skill215/smppkit/tlv"
)

func testCodec(t testing.TB) *codec.Codec {
	t.Helper()
	c, e := codec.New(codec.Config{Version: pdu.V50})
	if e != nil {
		t.Fatal(e)
	}
	return c
}
func sessionPair(t *testing.T, cfg Config) (*Session, *Session) {
	t.Helper()
	a, b := net.Pipe()
	c := testCodec(t)
	client, e := NewSession(a, c, cfg)
	if e != nil {
		t.Fatal(e)
	}
	srv, e := NewServer(ServerConfig{Profiles: []*codec.Codec{c}, Session: cfg, Authenticate: func(context.Context, BindInfo) (BindDecision, error) { return BindDecision{SystemID: "test"}, nil }})
	if e != nil {
		t.Fatal(e)
	}
	ready := make(chan *Session, 1)
	errs := make(chan error, 1)
	go func() { s, e := srv.Accept(t.Context(), b); ready <- s; errs <- e }()
	if _, e = client.Bind(t.Context(), &pdu.BindTransceiver{BindFields: pdu.BindFields{InterfaceVersion: pdu.V50}}); e != nil {
		t.Fatal(e)
	}
	server := <-ready
	if e = <-errs; e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { client.Close(); server.Close(); client.Wait(); server.Wait() })
	return client, server
}
func TestBidirectionalSameSequenceAndHandlerRequest(t *testing.T) {
	client, server := sessionPair(t, Config{Window: 1})
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	// Both next sequence values are equal despite the client bind. This tests the
	// independent request namespaces and a handler making a synchronous request.
	server.mu.Lock()
	server.seq = 1
	server.mu.Unlock()
	errorsCh := make(chan error, 2)
	go func() {
		in, e := client.Receive(ctx)
		if e == nil {
			_, e = client.Request(ctx, &pdu.SubmitSM{})
			if e == nil {
				e = in.Respond(ctx, pdu.OK, &pdu.DeliverSMResp{})
			}
		}
		errorsCh <- e
	}()
	go func() {
		in, e := server.Receive(ctx)
		if e == nil {
			e = in.Respond(ctx, pdu.OK, &pdu.SubmitSMResp{MessageResponseFields: pdu.MessageResponseFields{MessageID: "opaque:ID"}})
		}
		errorsCh <- e
	}()
	result, e := server.Request(ctx, &pdu.DeliverSM{})
	if e != nil || result.Outcome != ResponseReceived {
		t.Fatal(result, e)
	}
	for range 2 {
		if e := <-errorsCh; e != nil {
			t.Fatal(e)
		}
	}
}
func TestConcurrentRequestsAndResponses(t *testing.T) {
	client, server := sessionPair(t, Config{Window: 4})
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	const count = 40
	done := make(chan error, 1)
	go func() {
		for range count {
			in, e := server.Receive(ctx)
			if e != nil {
				done <- e
				return
			}
			if e = in.Respond(ctx, pdu.OK, &pdu.SubmitSMResp{}); e != nil {
				done <- e
				return
			}
		}
		done <- nil
	}()
	var wg sync.WaitGroup
	errs := make(chan error, count)
	for range count {
		wg.Go(func() { _, e := client.Request(ctx, &pdu.SubmitSM{}); errs <- e })
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	if e := <-done; e != nil {
		t.Fatal(e)
	}
}
func TestWrongCommandResponseCloses(t *testing.T) {
	client, server := sessionPair(t, Config{})
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	go func() {
		in, e := server.Receive(ctx)
		if e != nil {
			return
		}
		b, _ := server.codec.Append(nil, pdu.PDU{Sequence: in.PDU.Sequence, Body: &pdu.DeliverSMResp{}})
		_ = server.enqueueResponse(job(b))
	}()
	r, e := client.Request(ctx, &pdu.SubmitSM{})
	if !errors.Is(e, ErrResponse) || r.Outcome == ResponseReceived {
		t.Fatal(r, e)
	}
}
func TestInboundOverloadAndTimeout(t *testing.T) {
	for _, timeout := range []bool{false, true} {
		t.Run(map[bool]string{false: "capacity", true: "timeout"}[timeout], func(t *testing.T) {
			client, server := sessionPair(t, Config{InboundLimit: 1, InboundTimeout: 40 * time.Millisecond})
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			first := make(chan error, 1)
			go func() { _, e := client.Request(ctx, &pdu.SubmitSM{}); first <- e }()
			if _, e := server.Receive(ctx); e != nil {
				t.Fatal(e)
			}
			if !timeout {
				_, _ = client.Request(ctx, &pdu.SubmitSM{})
			}
			select {
			case <-server.Done():
			case <-ctx.Done():
				t.Fatal("reader blocked on full input")
			}
			if !timeout && !errors.Is(server.Err(), ErrOverloaded) {
				t.Fatal(server.Err())
			}
			if <-first == nil {
				t.Fatal("request unexpectedly accepted")
			}
		})
	}
}
func TestTimeoutRetainsUncertainAndDoesNotRetry(t *testing.T) {
	client, server := sessionPair(t, Config{RequestTimeout: 35 * time.Millisecond})
	ctx := t.Context()
	got := make(chan *Inbound, 1)
	go func() { in, _ := server.Receive(ctx); got <- in }()
	r, e := client.Request(ctx, &pdu.SubmitSM{})
	if !errors.Is(e, context.DeadlineExceeded) || r.Outcome != Written {
		t.Fatal(r, e)
	}
	in := <-got
	if in == nil {
		t.Fatal("missing request")
	}
	if e := in.Respond(ctx, pdu.OK, &pdu.SubmitSMResp{}); e != nil {
		t.Fatal(e)
	}
	r, e = client.Request(ctx, &pdu.EnquireLink{})
	if e != nil || r.Outcome != ResponseReceived {
		t.Fatal(r, e)
	}
}
func TestCloseCancelRaces(t *testing.T) {
	for range 20 {
		client, server := sessionPair(t, Config{})
		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan error, 1)
		go func() { _, e := client.Request(ctx, &pdu.SubmitSM{}); done <- e }()
		go cancel()
		go client.Close()
		server.Close()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("Request stuck after close")
		}
		client.Wait()
		server.Wait()
	}
}
func TestUnbindAndSequenceRetirement(t *testing.T) {
	client, _ := sessionPair(t, Config{})
	if e := client.Shutdown(t.Context()); e != nil {
		t.Fatal(e)
	}
	client2, _ := sessionPair(t, Config{})
	client2.mu.Lock()
	client2.seq = 0x7fffffff
	client2.mu.Unlock()
	r, e := client2.Request(t.Context(), &pdu.EnquireLink{})
	if e == nil || r.Outcome != NotWritten || client2.State() != Closed {
		t.Fatal(r, e)
	}
}

// Independent ESME: bytes and expected bytes are authored directly, without the
// Codec. It fragments a bind and submits an empty message over a real TCP socket.
func TestIndependentTCPPeer(t *testing.T) {
	c := testCodec(t)
	srv, _ := NewServer(ServerConfig{Profiles: []*codec.Codec{c}, Authenticate: func(_ context.Context, b BindInfo) (BindDecision, error) {
		if b.Fields.SystemID != "esme" || b.Fields.Password != "pw" {
			return BindDecision{Status: pdu.BindFailed}, nil
		}
		return BindDecision{SystemID: "smsc"}, nil
	}})
	ln, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer ln.Close()
	done := make(chan error, 1)
	go func() {
		conn, e := ln.Accept()
		if e != nil {
			done <- e
			return
		}
		s, e := srv.Accept(t.Context(), conn)
		if e != nil {
			done <- e
			return
		}
		defer func() { s.Close(); s.Wait() }()
		in, e := s.Receive(t.Context())
		if e == nil {
			e = in.Respond(t.Context(), pdu.OK, &pdu.SubmitSMResp{MessageResponseFields: pdu.MessageResponseFields{MessageID: "abc"}})
		}
		done <- e
	}()
	conn, e := net.Dial("tcp", ln.Addr().String())
	if e != nil {
		t.Fatal(e)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(2 * time.Second))
	bind, _ := hex.DecodeString("0000001d00000009000000000000000165736d65007077000050000000")
	for _, b := range bind {
		if _, e := conn.Write([]byte{b}); e != nil {
			t.Fatal(e)
		}
	}
	expected, _ := hex.DecodeString("0000001a800000090000000000000001736d7363000210000150")
	got := make([]byte, len(expected))
	if _, e = io.ReadFull(conn, got); e != nil || !bytes.Equal(got, expected) {
		t.Fatalf("bind response %x %v", got, e)
	}
	submit, _ := hex.DecodeString("000000210000000400000000000000020000000000000000000000000000000000")
	if _, e = conn.Write(submit); e != nil {
		t.Fatal(e)
	}
	expected, _ = hex.DecodeString("0000001480000004000000000000000261626300")
	got = make([]byte, len(expected))
	if _, e = io.ReadFull(conn, got); e != nil || !bytes.Equal(got, expected) {
		t.Fatalf("submit response %x %v", got, e)
	}
	if e = <-done; e != nil {
		t.Fatal(e)
	}
}
func TestTLSClientServer(t *testing.T) {
	key, e := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if e != nil {
		t.Fatal(e)
	}
	now := time.Now()
	template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "localhost"}, NotBefore: now.Add(-time.Minute), NotAfter: now.Add(time.Hour), IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}, KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, e := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if e != nil {
		t.Fatal(e)
	}
	keyDER, _ := x509.MarshalECPrivateKey(key)
	cert, e := tls.X509KeyPair(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}))
	if e != nil {
		t.Fatal(e)
	}
	roots := x509.NewCertPool()
	parsed, _ := x509.ParseCertificate(der)
	roots.AddCert(parsed)
	ln, e := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12})
	if e != nil {
		t.Fatal(e)
	}
	c := testCodec(t)
	srv, _ := NewServer(ServerConfig{Profiles: []*codec.Codec{c}, Authenticate: func(context.Context, BindInfo) (BindDecision, error) { return BindDecision{}, nil }})
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- srv.Serve(ctx, ln, func(ctx context.Context, s *Session) { <-s.Done() }) }()
	client, e := Dial(t.Context(), ln.Addr().String(), ClientConfig{Codec: c, TLS: &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}, Bind: &pdu.BindTransceiver{BindFields: pdu.BindFields{InterfaceVersion: pdu.V50}}})
	if e != nil {
		cancel()
		t.Fatal(e)
	}
	if _, e = client.Request(t.Context(), &pdu.EnquireLink{}); e != nil {
		t.Fatal(e)
	}
	client.Close()
	client.Wait()
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Serve did not join")
	}
}

// faultConn controls write behavior without using the production codec.
type faultConn struct {
	net.Conn
	write func([]byte) (int, error)
}

func (c *faultConn) Write(b []byte) (int, error) { return c.write(b) }
func TestPartialWriteAndQueuedCancellation(t *testing.T) {
	t.Run("partial", func(t *testing.T) {
		a, b := net.Pipe()
		defer b.Close()
		failure := errors.New("injected partial write")
		conn := &faultConn{Conn: a, write: func([]byte) (int, error) { return 7, failure }}
		s, e := newSession(conn, testCodec(t), Config{}, false, Bound, Transceiver)
		if e != nil {
			t.Fatal(e)
		}
		defer func() { s.Close(); s.Wait() }()
		result, e := s.Request(t.Context(), &pdu.SubmitSM{})
		if !errors.Is(e, failure) || result.Outcome != PartlyWritten || result.BytesWritten != 7 {
			t.Fatal(result, e)
		}
	})
	t.Run("queued cancellation is prompt", func(t *testing.T) {
		a, b := net.Pipe()
		defer b.Close()
		s, e := newSession(a, testCodec(t), Config{WriteTimeout: time.Second}, false, Bound, Transceiver)
		if e != nil {
			t.Fatal(e)
		}
		defer func() { s.Close(); s.Wait() }()
		firstDone := make(chan struct{})
		go func() { _, _ = s.Request(t.Context(), &pdu.SubmitSM{}); close(firstDone) }()
		// Wait for the first frame to start writing; the pipe peer never reads.
		deadline := time.Now().Add(time.Second)
		for {
			started := false
			s.mu.Lock()
			for _, p := range s.pending {
				p.job.mu.Lock()
				started = p.job.started
				p.job.mu.Unlock()
			}
			s.mu.Unlock()
			if started {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("writer did not start")
			}
			time.Sleep(time.Millisecond)
		}
		ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
		defer cancel()
		start := time.Now()
		r, e := s.Request(ctx, &pdu.SubmitSM{})
		if !errors.Is(e, context.DeadlineExceeded) || r.Outcome != NotWritten || time.Since(start) > 300*time.Millisecond {
			t.Fatal(r, e)
		}
		s.Close()
		<-firstDone
	})
}
func TestGenericNACKAndRespondOnce(t *testing.T) {
	client, server := sessionPair(t, Config{})
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		in, e := server.Receive(ctx)
		if e != nil {
			done <- e
			return
		}
		if e = in.Respond(ctx, pdu.InvalidCommandID, &pdu.GenericNACK{}); e != nil {
			done <- e
			return
		}
		done <- in.Respond(ctx, pdu.OK, &pdu.SubmitSMResp{})
	}()
	r, e := client.Request(ctx, &pdu.SubmitSM{})
	if _, ok := errors.AsType[*PeerError](e); !ok || r.Response.Body.CommandID() != pdu.GenericNACKID {
		t.Fatal(r, e)
	}
	if e = <-done; !errors.Is(e, ErrResponded) {
		t.Fatal(e)
	}
}
func TestBindAdvertisementAndVersionPolicy(t *testing.T) {
	for _, tc := range []struct {
		name      string
		advert    byte
		missing   bool
		wantError bool
	}{{"absent", 0, true, false}, {"same", 0x50, false, false}, {"unknown", 0x99, false, true}, {"different", 0x34, false, true}} {
		t.Run(tc.name, func(t *testing.T) {
			a, b := net.Pipe()
			s, e := NewSession(a, testCodec(t), Config{})
			if e != nil {
				t.Fatal(e)
			}
			defer func() { s.Close(); s.Wait(); b.Close() }()
			done := make(chan error, 1)
			go func() {
				p, e := s.codec.Read(b)
				if e != nil {
					done <- e
					return
				}
				// A raw peer can advertise an invalid version; don't ask the
				// strict production encoder to manufacture that malformed TLV.
				wire := []byte{0, 0, 0, 17, 0x80, 0, 0, 9, 0, 0, 0, 0, byte(p.Sequence >> 24), byte(p.Sequence >> 16), byte(p.Sequence >> 8), byte(p.Sequence), 0}
				if !tc.missing {
					wire[3] = 22
					wire = append(wire, 2, 0x10, 0, 1, tc.advert)
				}
				_, e = b.Write(wire)
				done <- e
			}()
			_, e = s.Bind(t.Context(), &pdu.BindTransceiver{BindFields: pdu.BindFields{InterfaceVersion: pdu.V50}})
			if (e != nil) != tc.wantError {
				t.Fatal(e)
			}
			if e = <-done; e != nil {
				t.Fatal(e)
			}
			if tc.missing && s.TLVEnabled() {
				t.Fatal("missing advertisement enabled TLVs")
			}
			if tc.missing {
				r, e := s.Request(t.Context(), &pdu.SubmitSM{SMFields: pdu.SMFields{Optional: tlv.Fields{{Tag: 0x1500}}}})
				if !errors.Is(e, ErrState) || r.Outcome != NotWritten {
					t.Fatal(r, e)
				}
			}
		})
	}
}

func TestAllVersionBindRoles(t *testing.T) {
	for _, v := range []pdu.Version{pdu.V33, pdu.V34, pdu.V50} {
		for _, role := range []Role{Transmitter, Receiver, Transceiver} {
			if v == pdu.V33 && role == Transceiver {
				continue
			}
			t.Run(fmt.Sprintf("%x/%d", v, role), func(t *testing.T) {
				c, e := codec.New(codec.Config{Version: v})
				if e != nil {
					t.Fatal(e)
				}
				a, b := net.Pipe()
				client, e := NewSession(a, c, Config{})
				if e != nil {
					t.Fatal(e)
				}
				srv, e := NewServer(ServerConfig{Profiles: []*codec.Codec{c}, Authenticate: func(_ context.Context, info BindInfo) (BindDecision, error) {
					if info.Role != role || info.Version != v {
						t.Error("bootstrap mismatch")
					}
					return BindDecision{}, nil
				}})
				if e != nil {
					t.Fatal(e)
				}
				ready := make(chan *Session, 1)
				errs := make(chan error, 1)
				go func() { s, e := srv.Accept(t.Context(), b); ready <- s; errs <- e }()
				var bind pdu.Body
				fields := pdu.BindFields{InterfaceVersion: v}
				switch role {
				case Transmitter:
					bind = &pdu.BindTransmitter{BindFields: fields}
				case Receiver:
					bind = &pdu.BindReceiver{BindFields: fields}
				case Transceiver:
					bind = &pdu.BindTransceiver{BindFields: fields}
				}
				if _, e := client.Bind(t.Context(), bind); e != nil {
					t.Fatal(e)
				}
				server := <-ready
				if e = <-errs; e != nil {
					t.Fatal(e)
				}
				defer func() { client.Close(); server.Close(); client.Wait(); server.Wait() }()
				if client.TLVEnabled() != (v != pdu.V33) {
					t.Fatal("version negotiation")
				}
				if role == Receiver {
					if _, e := client.Request(t.Context(), &pdu.SubmitSM{}); !errors.Is(e, ErrState) {
						t.Fatal("RX submitted", e)
					}
				} else {
					if _, e := server.Request(t.Context(), &pdu.SubmitSM{}); !errors.Is(e, ErrState) {
						t.Fatal("server submitted", e)
					}
				}
				if e := client.Shutdown(t.Context()); e != nil {
					t.Fatal(e)
				}
			})
		}
	}
}
func TestBindRejectionTimeoutAndUnboundRequest(t *testing.T) {
	t.Run("rejection", func(t *testing.T) {
		a, b := net.Pipe()
		client, _ := NewSession(a, testCodec(t), Config{})
		defer func() { client.Close(); client.Wait() }()
		srv, _ := NewServer(ServerConfig{Profiles: []*codec.Codec{testCodec(t)}, Authenticate: func(context.Context, BindInfo) (BindDecision, error) {
			return BindDecision{Status: pdu.BindFailed}, nil
		}})
		done := make(chan error, 1)
		go func() { _, e := srv.Accept(t.Context(), b); done <- e }()
		r, e := client.Bind(t.Context(), &pdu.BindTransceiver{BindFields: pdu.BindFields{InterfaceVersion: pdu.V50}})
		if _, ok := errors.AsType[*PeerError](e); !ok || r.Outcome != ResponseReceived || r.Response.Status != pdu.BindFailed {
			t.Fatal(r, e)
		}
		e = <-done
		if _, ok := errors.AsType[*PeerError](e); !ok {
			t.Fatal(e)
		}
	})
	t.Run("timeout", func(t *testing.T) {
		a, b := net.Pipe()
		defer a.Close()
		srv, _ := NewServer(ServerConfig{Profiles: []*codec.Codec{testCodec(t)}, BindTimeout: 20 * time.Millisecond, Authenticate: func(context.Context, BindInfo) (BindDecision, error) {
			t.Error("authentication before bind")
			return BindDecision{}, nil
		}})
		if _, e := srv.Accept(t.Context(), b); e == nil {
			t.Fatal("bind timeout absent")
		}
	})
	t.Run("unbound", func(t *testing.T) {
		a, b := net.Pipe()
		defer b.Close()
		s, _ := NewSession(a, testCodec(t), Config{})
		defer func() { s.Close(); s.Wait() }()
		r, e := s.Request(t.Context(), &pdu.SubmitSM{})
		if !errors.Is(e, ErrState) || r.Outcome != NotWritten {
			t.Fatal(r, e)
		}
	})
}
