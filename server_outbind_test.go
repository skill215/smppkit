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
	"testing"
	"time"

	"github.com/skill215/smppkit/codec"
	"github.com/skill215/smppkit/pdu"
)

func outbindServer(t *testing.T, v pdu.Version, timeout time.Duration, auth func(context.Context, BindInfo) (BindDecision, error)) *Server {
	t.Helper()
	c, err := codec.New(codec.Config{Version: v})
	if err != nil {
		t.Fatal(err)
	}
	if auth == nil {
		auth = func(context.Context, BindInfo) (BindDecision, error) { return BindDecision{SystemID: "mc"}, nil }
	}
	srv, err := NewServer(ServerConfig{Profiles: []*codec.Codec{c}, BindTimeout: timeout, Authenticate: auth})
	if err != nil {
		t.Fatal(err)
	}
	return srv
}

func TestOutbindPublicFlow(t *testing.T) {
	for _, v := range []pdu.Version{pdu.V34, pdu.V50} {
		t.Run(fmt.Sprintf("%x", v), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			srv := outbindServer(t, v, 0, func(_ context.Context, info BindInfo) (BindDecision, error) {
				if info.Role != Receiver || info.Version != v || info.Fields.SystemID != "esme" || info.Fields.Password != "esme-pw" {
					return BindDecision{}, errors.New("wrong bind identity")
				}
				return BindDecision{SystemID: "mc"}, nil
			})
			a, b := net.Pipe()
			defer a.Close()
			defer b.Close()
			client, err := NewSession(a, srv.profiles[v], Config{})
			if err != nil {
				t.Fatal(err)
			}
			defer func() { client.Close(); client.Wait() }()
			type accepted struct {
				s   *Session
				err error
			}
			ready := make(chan accepted, 1)
			go func() {
				s, err := srv.Outbind(ctx, b, v, &pdu.Outbind{SystemID: "mc", Password: "mc-pw"})
				ready <- accepted{s, err}
			}()
			packet, err := client.ReceiveNotification(ctx)
			if err != nil {
				t.Fatal(err)
			}
			out, ok := packet.Body.(*pdu.Outbind)
			if !ok || out.SystemID != "mc" || out.Password != "mc-pw" || client.State() != Connected {
				t.Fatal("invalid outbind")
			}
			// Rejection is local and leaves RX bind available; no sequence consumed.
			if r, err := client.Bind(ctx, &pdu.BindTransceiver{BindFields: pdu.BindFields{InterfaceVersion: v}}); !errors.Is(err, ErrState) || r.Sequence != 0 {
				t.Fatal(r, err)
			}
			if _, err := client.Bind(ctx, &pdu.BindReceiver{BindFields: pdu.BindFields{SystemID: "esme", Password: "esme-pw", InterfaceVersion: v}}); err != nil {
				t.Fatal(err)
			}
			acceptedPeer := <-ready
			if acceptedPeer.err != nil {
				t.Fatal(acceptedPeer.err)
			}
			server := acceptedPeer.s
			defer func() { server.Close(); server.Wait() }()
			r, err := server.Notify(ctx, &pdu.AlertNotification{})
			if err != nil || r.Sequence != 2 || r.Outcome != Written {
				t.Fatal(r, err)
			}
			if packet, err = client.ReceiveNotification(ctx); err != nil || packet.Body.CommandID() != pdu.AlertNotificationID {
				t.Fatal(packet, err)
			}
			if err := client.Shutdown(ctx); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestOutbindIndependentESME(t *testing.T) {
	for _, v := range []pdu.Version{pdu.V34, pdu.V50} {
		t.Run(fmt.Sprintf("%x", v), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			srv := outbindServer(t, v, 0, nil)
			a, b := net.Pipe()
			defer a.Close()
			defer b.Close()
			done := make(chan error, 1)
			go func() {
				readExact := func(h string) error {
					want, _ := hex.DecodeString(h)
					got := make([]byte, len(want))
					if _, err := io.ReadFull(b, got); err != nil {
						return err
					}
					if !bytes.Equal(got, want) {
						return fmt.Errorf("wire mismatch %x", got)
					}
					return nil
				}
				if err := readExact("000000160000000b00000000000000016d6300707700"); err != nil {
					done <- err
					return
				}
				// Peer bind uses its own sequence space and never echoes outbind's
				// sequence as an invented response. All bytes are independently set.
				raw, _ := hex.DecodeString(fmt.Sprintf("0000001d00000001000000000000000965736d650070770000%02x000000", v))
				for _, octet := range raw {
					if _, err := b.Write([]byte{octet}); err != nil {
						done <- err
						return
					}
				}
				if err := readExact(fmt.Sprintf("000000188000000100000000000000096d630002100001%02x", v)); err != nil {
					done <- err
					return
				}
				// A late NACK to the bootstrap's sequence cannot complete any live request.
				nack, _ := hex.DecodeString("00000010800000000000000800000001")
				if _, err := b.Write(nack); err != nil {
					done <- err
					return
				}
				if err := readExact("00000016000001020000000000000002000000000000"); err != nil {
					done <- err
					return
				}
				if err := readExact("00000010000000150000000000000003"); err != nil {
					done <- err
					return
				}
				response, _ := hex.DecodeString("00000010800000150000000000000003")
				_, err := b.Write(response)
				done <- err
			}()
			s, err := srv.Outbind(ctx, a, v, &pdu.Outbind{SystemID: "mc", Password: "pw"})
			if err != nil {
				t.Fatal(err)
			}
			defer func() { s.Close(); s.Wait() }()
			if r, err := s.Notify(ctx, &pdu.AlertNotification{}); err != nil || r.Sequence != 2 {
				t.Fatal(r, err)
			}
			if r, err := s.Request(ctx, &pdu.EnquireLink{}); err != nil || r.Sequence != 3 || r.Outcome != ResponseReceived {
				t.Fatal(r, err)
			}
			if err := <-done; err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestOutbindFailureBoundaries(t *testing.T) {
	for _, test := range []string{"write-timeout", "read-timeout", "wrong-role", "wrong-version", "refused", "cancel"} {
		t.Run(test, func(t *testing.T) {
			called := false
			srv := outbindServer(t, pdu.V50, 30*time.Millisecond, func(context.Context, BindInfo) (BindDecision, error) { called = true; return BindDecision{}, nil })
			a, b := net.Pipe()
			defer a.Close()
			defer b.Close()
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			done := make(chan error, 1)
			go func() {
				s, err := srv.Outbind(ctx, a, pdu.V50, &pdu.Outbind{SystemID: "mc", Password: "pw"})
				if s != nil {
					s.Close()
					s.Wait()
				}
				done <- err
			}()
			if test != "write-timeout" {
				if _, err := io.ReadFull(b, make([]byte, 22)); err != nil {
					t.Fatal(err)
				}
			}
			switch test {
			case "wrong-role", "wrong-version":
				raw, _ := hex.DecodeString("0000001700000001000000000000000100000050000000")
				if test == "wrong-role" {
					raw[7] = 2
				} else {
					raw[19] = 0x34
				}
				if _, err := b.Write(raw); err != nil {
					t.Fatal(err)
				}
			case "refused":
				b.Close()
			case "cancel":
				cancel()
			}
			select {
			case err := <-done:
				if err == nil {
					t.Fatal("failed outbind accepted")
				}
			case <-time.After(time.Second):
				t.Fatal("bootstrap did not terminate")
			}
			if called {
				t.Fatal("invalid bootstrap reached authentication")
			}
		})
	}
	for _, v := range []pdu.Version{pdu.V33, 0x51} {
		srv := outbindServer(t, pdu.V50, 0, nil)
		a, b := net.Pipe()
		if _, err := srv.Outbind(t.Context(), a, v, &pdu.Outbind{}); !errors.Is(err, ErrState) {
			t.Fatal(err)
		}
		if _, err := b.Read(make([]byte, 1)); err == nil {
			t.Fatal("unsupported bootstrap left transport open")
		}
		b.Close()
	}
}
