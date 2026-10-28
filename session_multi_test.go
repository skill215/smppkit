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
	"sync"
	"testing"
	"time"

	"github.com/skill215/smppkit/codec"
	"github.com/skill215/smppkit/pdu"
	"github.com/skill215/smppkit/tlv"
)

// This peer never calls Codec. It checks the complete request and fragments a
// literal partial-failure response, including a full-width per-SME status.
func TestSubmitMultiIndependentSessionPeer(t *testing.T) {
	for _, v := range []pdu.Version{pdu.V33, pdu.V34, pdu.V50} {
		t.Run(fmt.Sprintf("%x", v), func(t *testing.T) {
			c, err := codec.New(codec.Config{Version: v})
			if err != nil {
				t.Fatal(err)
			}
			a, b := net.Pipe()
			s, err := newSession(a, c, Config{}, false, Bound, Transmitter)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { s.Close(); b.Close(); s.Wait() })
			ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
			defer cancel()
			_ = b.SetDeadline(time.Now().Add(2 * time.Second))
			done := make(chan error, 1)
			go func() {
				want, _ := hex.DecodeString("00000035000000210000000000000001434d540005007372630002010101313233000267726f757000000001000001000300026869")
				got := make([]byte, len(want))
				if _, err := io.ReadFull(b, got); err != nil {
					done <- err
					return
				}
				if !bytes.Equal(got, want) {
					done <- fmt.Errorf("unexpected request: %x", got)
					return
				}
				response, _ := hex.DecodeString("0000002080000021000000000000000169643a37000101013132330012345678")
				for _, octet := range response {
					if _, err := b.Write([]byte{octet}); err != nil {
						done <- err
						return
					}
				}
				done <- nil
			}()
			result, err := s.Request(ctx, &pdu.SubmitMulti{ServiceType: "CMT", Source: pdu.Address{TON: 5, Value: "src"},
				Destinations: []pdu.MultiDestination{{Kind: pdu.SMEAddress, Address: pdu.Address{TON: 1, NPI: 1, Value: "123"}}, {Kind: pdu.DistributionList, ListName: "group"}},
				PriorityFlag: 1, RegisteredDelivery: 1, DataCoding: 3, ShortMessage: []byte("hi")})
			if err != nil || result.Outcome != ResponseReceived || result.BytesWritten != 53 || result.Response.Status != pdu.OK {
				t.Fatal(result, err)
			}
			body, ok := result.Response.Body.(*pdu.SubmitMultiResp)
			if !ok || body.MessageID != "id:7" || len(body.Unsuccessful) != 1 || body.Unsuccessful[0].Status != 0x12345678 || body.Unsuccessful[0].Address.Value != "123" {
				t.Fatalf("partial failure lost: %+v", body)
			}
			if err := <-done; err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestSubmitMultiBoundRolesAndConcurrentRequests(t *testing.T) {
	for _, v := range []pdu.Version{pdu.V33, pdu.V34, pdu.V50} {
		for _, role := range []Role{Transmitter, Receiver, Transceiver} {
			if v == pdu.V33 && role == Transceiver {
				continue
			}
			t.Run(fmt.Sprintf("%x/%d", v, role), func(t *testing.T) {
				ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
				defer cancel()
				c, err := codec.New(codec.Config{Version: v})
				if err != nil {
					t.Fatal(err)
				}
				a, b := net.Pipe()
				client, err := NewSession(a, c, Config{Window: 4})
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { client.Close(); b.Close(); client.Wait() })
				srv, err := NewServer(ServerConfig{Profiles: []*codec.Codec{c}, Authenticate: func(context.Context, BindInfo) (BindDecision, error) { return BindDecision{SystemID: "test"}, nil }})
				if err != nil {
					t.Fatal(err)
				}
				type accepted struct {
					s   *Session
					err error
				}
				ready := make(chan accepted, 1)
				go func() { s, err := srv.Accept(ctx, b); ready <- accepted{s, err} }()
				fields := pdu.BindFields{InterfaceVersion: v}
				var bind pdu.Body = &pdu.BindTransmitter{BindFields: fields}
				if role == Receiver {
					bind = &pdu.BindReceiver{BindFields: fields}
				}
				if role == Transceiver {
					bind = &pdu.BindTransceiver{BindFields: fields}
				}
				if _, err := client.Bind(ctx, bind); err != nil {
					t.Fatal(err)
				}
				peer := <-ready
				if peer.err != nil {
					t.Fatal(peer.err)
				}
				server := peer.s
				t.Cleanup(func() { server.Close(); server.Wait() })
				request := &pdu.SubmitMulti{Destinations: []pdu.MultiDestination{{Kind: pdu.SMEAddress, Address: pdu.Address{Value: "123"}}}}
				if v != pdu.V33 {
					request.Optional = tlv.Fields{{Tag: tlv.PrivacyIndicator, Value: []byte{1}}}
				}
				// A server cannot submit in the reverse direction, regardless of role.
				result, err := server.Request(ctx, request)
				if !errors.Is(err, ErrState) || result.BytesWritten != 0 {
					t.Fatal(result, err)
				}
				if role == Receiver {
					result, err = client.Request(ctx, request)
					if !errors.Is(err, ErrState) || result.BytesWritten != 0 {
						t.Fatal(result, err)
					}
					return
				}
				const count = 24
				done := make(chan error, 1)
				go func() {
					for range count {
						in, err := server.Receive(ctx)
						if err != nil {
							done <- err
							return
						}
						body, ok := in.PDU.Body.(*pdu.SubmitMulti)
						if !ok || len(body.Destinations) != 1 {
							done <- fmt.Errorf("wrong request body")
							return
						}
						response := &pdu.SubmitMultiResp{MessageID: fmt.Sprint(in.PDU.Sequence), Unsuccessful: []pdu.UnsuccessfulSME{{Address: body.Destinations[0].Address, Status: pdu.Throttled}}}
						if v == pdu.V50 {
							response.Optional = tlv.Fields{{Tag: tlv.CongestionState, Value: []byte{80}}}
						}
						if err := in.Respond(ctx, pdu.OK, response); err != nil {
							done <- err
							return
						}
					}
					done <- nil
				}()
				var wg sync.WaitGroup
				errs := make(chan error, count)
				for range count {
					wg.Go(func() {
						r, err := client.Request(ctx, request) // shared immutable input and Codec
						if err == nil {
							body, ok := r.Response.Body.(*pdu.SubmitMultiResp)
							if !ok || body.MessageID != fmt.Sprint(r.Sequence) || len(body.Unsuccessful) != 1 || (v == pdu.V50 && len(body.Optional) != 1) {
								err = fmt.Errorf("response correlation or failure lost")
							}
						}
						errs <- err
					})
				}
				wg.Wait()
				close(errs)
				for err := range errs {
					if err != nil {
						t.Fatal(err)
					}
				}
				if err := <-done; err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}

func TestSubmitMultiOptionalPolicyAndNegativeResponse(t *testing.T) {
	client, server := sessionPair(t, Config{})
	// Model the established no-advertisement policy (handshake variants are
	// separately tested). Both new body types must respect this Session policy.
	for _, s := range []*Session{client, server} {
		s.mu.Lock()
		s.tlvAllowed = false
		s.mu.Unlock()
	}
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	request := &pdu.SubmitMulti{Destinations: []pdu.MultiDestination{{Kind: pdu.SMEAddress}}, Optional: tlv.Fields{{Tag: tlv.PrivacyIndicator, Value: []byte{1}}}}
	result, err := client.Request(ctx, request)
	if !errors.Is(err, ErrState) || result.BytesWritten != 0 {
		t.Fatal(result, err)
	}
	request.Optional = nil
	done := make(chan error, 1)
	go func() {
		in, err := server.Receive(ctx)
		if err != nil {
			done <- err
			return
		}
		err = in.Respond(ctx, pdu.OK, &pdu.SubmitMultiResp{Optional: tlv.Fields{{Tag: tlv.CongestionState, Value: []byte{90}}}})
		if !errors.Is(err, ErrState) {
			done <- fmt.Errorf("response TLV policy bypassed: %v", err)
			return
		}
		// Rejected local validation must leave the response handle usable.
		done <- in.Respond(ctx, pdu.Throttled, &pdu.Omitted{ID: pdu.SubmitMultiRespID})
	}()
	result, err = client.Request(ctx, request)
	if peerError, ok := errors.AsType[*PeerError](err); !ok || peerError.Status != pdu.Throttled || result.Outcome != ResponseReceived || result.Response.Status != pdu.Throttled {
		t.Fatal(result, err)
	}
	if _, ok := result.Response.Body.(*pdu.Omitted); !ok {
		t.Fatal("negative omission not preserved")
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
