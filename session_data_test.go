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

func dataSessionPair(t *testing.T, v pdu.Version, role Role) (*Session, *Session) {
	t.Helper()
	c, err := codec.New(codec.Config{Version: v})
	if err != nil {
		t.Fatal(err)
	}
	a, b := net.Pipe()
	client, err := NewSession(a, c, Config{Window: 2})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { client.Close(); b.Close(); client.Wait() })
	srv, err := NewServer(ServerConfig{Profiles: []*codec.Codec{c}, Session: Config{Window: 2}, Authenticate: func(context.Context, BindInfo) (BindDecision, error) { return BindDecision{SystemID: "test"}, nil }})
	if err != nil {
		t.Fatal(err)
	}
	type accepted struct {
		s   *Session
		err error
	}
	ready := make(chan accepted, 1)
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	go func() { s, err := srv.Accept(ctx, b); ready <- accepted{s, err} }()
	fields := pdu.BindFields{InterfaceVersion: v}
	var bind pdu.Body = &pdu.BindTransceiver{BindFields: fields}
	if role == Transmitter {
		bind = &pdu.BindTransmitter{BindFields: fields}
	}
	if role == Receiver {
		bind = &pdu.BindReceiver{BindFields: fields}
	}
	if _, err = client.Bind(ctx, bind); err != nil {
		t.Fatal(err)
	}
	peer := <-ready
	if peer.err != nil {
		t.Fatal(peer.err)
	}
	t.Cleanup(func() { peer.s.Close(); peer.s.Wait() })
	return client, peer.s
}

func TestDataSessionDirectionAndRoles(t *testing.T) {
	for _, v := range []pdu.Version{pdu.V34, pdu.V50} {
		for _, role := range []Role{Transmitter, Receiver, Transceiver} {
			t.Run(fmt.Sprintf("%x/%d", v, role), func(t *testing.T) {
				client, server := dataSessionPair(t, v, role)
				ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
				defer cancel()
				// Invalid contextual fields fail before any sequence or write. The
				// receiver-facing error path must not consume a response handle either.
				for _, tc := range []struct {
					s    *Session
					body *pdu.DataSM
				}{
					{client, &pdu.DataSM{Optional: tlv.Fields{{Tag: tlv.MessageState, Value: []byte{2}}}}},
					{server, &pdu.DataSM{Optional: tlv.Fields{{Tag: tlv.SetDPF, Value: []byte{1}}}}},
				} {
					r, err := tc.s.Request(ctx, tc.body)
					if !errors.Is(err, codec.ErrValidation) || r.Outcome != NotWritten || r.BytesWritten != 0 || r.Sequence != 0 {
						t.Fatal(r, err)
					}
				}
				if role == Receiver {
					r, err := client.Request(ctx, &pdu.DataSM{})
					if !errors.Is(err, ErrState) || r.BytesWritten != 0 {
						t.Fatal(r, err)
					}
				}
				if role == Transmitter {
					r, err := server.Request(ctx, &pdu.DataSM{})
					if !errors.Is(err, ErrState) || r.BytesWritten != 0 {
						t.Fatal(r, err)
					}
				}
				for _, endpoint := range []*Session{client, server} {
					endpoint.mu.Lock()
					seq := endpoint.seq
					endpoint.mu.Unlock()
					want := uint32(0)
					if endpoint == client {
						want = 1
					} // bind only
					if seq != want {
						t.Fatal("invalid data_sm consumed sequence", seq, want)
					}
				}
				type flow struct {
					from, to   *Session
					submission bool
				}
				var flows []flow
				if role != Receiver {
					flows = append(flows, flow{client, server, true})
				}
				if role != Transmitter {
					flows = append(flows, flow{server, client, false})
				}
				const count = 12
				errs := make(chan error, len(flows)*(count+1))
				var handlers, requests sync.WaitGroup
				for _, f := range flows {
					handlers.Go(func() {
						for range count {
							in, err := f.to.Receive(ctx)
							if err != nil {
								errs <- err
								return
							}
							if in.PDU.Body.CommandID() != pdu.DataSMID {
								errs <- errors.New("unexpected inbound")
								return
							}
							// Owned packet edits cannot change the response's original direction.
							in.PDU.Body = &pdu.SubmitSM{}
							response := &pdu.DataSMResp{MessageResponseFields: pdu.MessageResponseFields{MessageID: fmt.Sprint(in.sequence), Optional: tlv.Fields{{Tag: tlv.DPFResult, Value: []byte{1}}}}}
							if !f.submission {
								if err := in.Respond(ctx, pdu.OK, response); !errors.Is(err, codec.ErrValidation) {
									errs <- fmt.Errorf("delivery response DPF accepted: %v", err)
									return
								}
								response.Optional = tlv.Fields{{Tag: tlv.AdditionalStatusInfoText, Value: []byte{'o', 'k', 0}}}
							}
							if err := in.Respond(ctx, pdu.OK, response); err != nil {
								errs <- err
								return
							}
						}
					})
					body := &pdu.DataSM{ESMClass: 2, Optional: tlv.Fields{{Tag: tlv.SetDPF, Value: []byte{1}}}}
					if !f.submission {
						body = &pdu.DataSM{ESMClass: 4, Optional: tlv.Fields{{Tag: tlv.MessageState, Value: []byte{2}}}}
					}
					for range count {
						requests.Go(func() {
							r, err := f.from.Request(ctx, body)
							if err == nil {
								response, ok := r.Response.Body.(*pdu.DataSMResp)
								if !ok || response.MessageID != fmt.Sprint(r.Sequence) || r.Outcome != ResponseReceived {
									err = errors.New("incorrect response correlation")
								}
							}
							errs <- err
						})
					}
				}
				requests.Wait()
				handlers.Wait()
				close(errs)
				for err := range errs {
					if err != nil {
						t.Fatal(err)
					}
				}
			})
		}
	}
}

func TestDataInvalidIncomingDirections(t *testing.T) {
	for _, v := range []pdu.Version{pdu.V34, pdu.V50} {
		for _, server := range []bool{false, true} {
			t.Run(fmt.Sprintf("%x/server=%v", v, server), func(t *testing.T) {
				c, err := codec.New(codec.Config{Version: v})
				if err != nil {
					t.Fatal(err)
				}
				a, b := net.Pipe()
				s, err := newSession(a, c, Config{}, server, Bound, Transceiver)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { s.Close(); b.Close(); s.Wait() })
				// Invalid request from the peer: receipt state on submission, or a
				// set_dpf instruction on delivery. The peer never uses Codec.
				rawHex := "0000001f000001030000000000000007000000000000000000000421000101"
				if server {
					rawHex = "0000001f000001030000000000000007000000000000000000000427000102"
				}
				raw, err := hex.DecodeString(rawHex)
				if err != nil {
					t.Fatal(err)
				}
				done := make(chan error, 1)
				go func() { _, err := b.Write(raw); done <- err }()
				select {
				case <-s.Done():
				case <-time.After(time.Second):
					t.Fatal("invalid request not rejected")
				}
				if !errors.Is(s.Err(), codec.ErrValidation) {
					t.Fatal(s.Err())
				}
				select {
				case in := <-s.incoming:
					t.Fatal("invalid data_sm admitted", in)
				default:
				}
				if err := <-done; err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}

func TestDataIndependentResponseDirections(t *testing.T) {
	for _, server := range []bool{false, true} {
		t.Run(fmt.Sprintf("server=%v", server), func(t *testing.T) {
			a, b := net.Pipe()
			s, err := newSession(a, testCodec(t), Config{}, server, Bound, Transceiver)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { s.Close(); b.Close(); s.Wait() })
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			done := make(chan error, 1)
			go func() {
				want, _ := hex.DecodeString("0000001a00000103000000000000000100000000000000020000")
				got := make([]byte, len(want))
				if _, err := io.ReadFull(b, got); err != nil {
					done <- err
					return
				}
				if !bytes.Equal(got, want) {
					done <- fmt.Errorf("request mismatch: %x", got)
					return
				}
				raw, _ := hex.DecodeString("000000178000010300000000000000016d000420000101")
				for _, octet := range raw {
					if _, err := b.Write([]byte{octet}); err != nil {
						done <- err
						return
					}
				}
				done <- nil
			}()
			r, err := s.Request(ctx, &pdu.DataSM{ESMClass: 2})
			if server {
				if !errors.Is(err, codec.ErrValidation) || r.Outcome == ResponseReceived {
					t.Fatal("delivery DPF response accepted", r, err)
				}
			} else if err != nil || r.Outcome != ResponseReceived {
				t.Fatal("submission response incorrectly reversed", r, err)
			}
			if err := <-done; err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestDataNegativeResponseForms(t *testing.T) {
	for _, reverse := range []bool{false, true} {
		for _, nack := range []bool{false, true} {
			client, server := sessionPair(t, Config{})
			from, to := client, server
			if reverse {
				from, to = server, client
			}
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			done := make(chan error, 1)
			go func() {
				in, err := to.Receive(ctx)
				if err != nil {
					done <- err
					return
				}
				var body pdu.Body = &pdu.Omitted{ID: pdu.DataSMRespID}
				if nack {
					body = &pdu.GenericNACK{}
				}
				done <- in.Respond(ctx, pdu.SystemError, body)
			}()
			r, err := from.Request(ctx, &pdu.DataSM{})
			if peer, ok := errors.AsType[*PeerError](err); !ok || r.Outcome != ResponseReceived || peer.Status != pdu.SystemError {
				t.Fatal(r, err)
			}
			if err := <-done; err != nil {
				t.Fatal(err)
			}
			cancel()
		}
	}
}
