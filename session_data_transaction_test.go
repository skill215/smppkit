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

func TestDataTransactionSessionSnapshot(t *testing.T) {
	for _, v := range []pdu.Version{pdu.V34, pdu.V50} {
		t.Run(fmt.Sprintf("%x", v), func(t *testing.T) {
			client, server := dataSessionPair(t, v, Transceiver)
			ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
			defer cancel()
			const count = 24
			errs := make(chan error, count+1)
			var wg sync.WaitGroup
			wg.Go(func() {
				for range count {
					in, err := server.Receive(ctx)
					if err != nil {
						errs <- err
						return
					}
					request := in.PDU.Body.(*pdu.DataSM)
					mode := request.ESMClass & 3
					// Edits to the owned request must not enable/disable response TLVs.
					request.ESMClass = (mode + 1) & 3
					response := &pdu.DataSMResp{MessageResponseFields: pdu.MessageResponseFields{MessageID: fmt.Sprint(mode), Optional: tlv.Fields{{Tag: tlv.DeliveryFailureReason, Value: []byte{0}}}}}
					status := pdu.Status(0xfe)
					if mode == 1 || mode == 3 {
						if err := in.Respond(ctx, status, response); !errors.Is(err, codec.ErrValidation) {
							errs <- fmt.Errorf("nontransaction response accepted: %v", err)
							return
						}
						response.Optional = nil
						status = pdu.OK
					} else if mode == 2 {
						if err := in.Respond(ctx, pdu.OK, response); !errors.Is(err, codec.ErrValidation) {
							errs <- fmt.Errorf("successful failure reason accepted: %v", err)
							return
						}
					}
					if err := in.Respond(ctx, status, response); err != nil {
						errs <- err
						return
					}
				}
			})
			for n := range count {
				wg.Go(func() {
					mode := byte(n % 4)
					r, err := client.Request(ctx, &pdu.DataSM{ESMClass: mode})
					if mode == 0 || mode == 2 {
						if peer, ok := errors.AsType[*PeerError](err); !ok || peer.Status != 0xfe {
							errs <- fmt.Errorf("lost transaction failure: %v", err)
							return
						}
					} else if err != nil {
						errs <- err
						return
					}
					if r.Outcome != ResponseReceived || r.Response.Body.(*pdu.DataSMResp).MessageID != fmt.Sprint(mode) {
						errs <- errors.New("mode context/correlation mismatch")
					}
				})
			}
			wg.Wait()
			close(errs)
			for err := range errs {
				t.Error(err)
			}
		})
	}
}

func TestDataTransactionRawResponses(t *testing.T) {
	// A peer using no Codec reads the exact request, then sends independent
	// response literals. Header statuses and optional fields are not rewritten by
	// production serializers. Each case owns a separate connection.
	for _, v := range []pdu.Version{pdu.V34, pdu.V50} {
		for _, tc := range []struct {
			name             string
			mode             byte
			response         string
			valid, peerError bool
		}{
			{"datagram-dpf", 1, "0000001780000103000000fe000000016d000420000101", false, false},
			{"stored-dpf", 3, "0000001780000103000000fe000000016d000420000101", false, false},
			{"default-dpf", 0, "0000001780000103000000fe000000016d000420000101", true, true},
			{"transaction-failure", 2, "0000001780000103000000fe000000016d000425000100", true, true},
			{"transaction-success-failure", 2, "000000178000010300000000000000016d000425000100", false, false},
			{"transaction-success", 2, "000000128000010300000000000000016d00", true, false},
			{"datagram-diagnostic", 1, "000000198000010300000008000000016d00001d00036f6b00", v == pdu.V34, true},
			{"omitted-error", 2, "00000010800001030000000800000001", true, true},
			{"nack", 3, "00000010800000000000000800000001", true, true},
		} {
			t.Run(fmt.Sprintf("%x/%s", v, tc.name), func(t *testing.T) {
				c, err := codec.New(codec.Config{Version: v})
				if err != nil {
					t.Fatal(err)
				}
				a, b := net.Pipe()
				s, err := newSession(a, c, Config{}, false, Bound, Transceiver)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { s.Close(); b.Close(); s.Wait() })
				ctx, cancel := context.WithTimeout(t.Context(), time.Second)
				defer cancel()
				done := make(chan error, 1)
				go func() {
					want, _ := hex.DecodeString("0000001a00000103000000000000000100000000000000020000")
					want[23] = tc.mode
					got := make([]byte, len(want))
					if _, err := io.ReadFull(b, got); err != nil {
						done <- err
						return
					}
					if !bytes.Equal(got, want) {
						done <- fmt.Errorf("request bytes: %x", got)
						return
					}
					raw, _ := hex.DecodeString(tc.response)
					for _, octet := range raw {
						if _, err := b.Write([]byte{octet}); err != nil {
							done <- err
							return
						}
					}
					done <- nil
				}()
				r, err := s.Request(ctx, &pdu.DataSM{ESMClass: tc.mode})
				if !tc.valid {
					if !errors.Is(err, codec.ErrValidation) || r.Outcome != Written {
						t.Fatal(r, err)
					}
				} else {
					_, isPeerError := errors.AsType[*PeerError](err)
					if r.Outcome != ResponseReceived || tc.peerError != isPeerError || !tc.peerError && err != nil {
						t.Fatal(r, err)
					}
				}
				if err := <-done; err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}

func TestDataDatagramSessionAdmission(t *testing.T) {
	for _, v := range []pdu.Version{pdu.V34, pdu.V50} {
		client, server := dataSessionPair(t, v, Transceiver)
		ctx, cancel := context.WithTimeout(t.Context(), time.Second)
		defer cancel()
		r, err := client.Request(ctx, &pdu.DataSM{ESMClass: 1, RegisteredDelivery: 1})
		if !errors.Is(err, codec.ErrValidation) || r.Outcome != NotWritten || r.Sequence != 0 || r.BytesWritten != 0 {
			t.Fatal(r, err)
		}
		client.mu.Lock()
		seq := client.seq
		client.mu.Unlock()
		if seq != 1 {
			t.Fatal("rejected request consumed sequence", seq)
		}
		// Bypass client validation with an independent invalid incoming request.
		raw, _ := hex.DecodeString("0000001a00000103000000000000000700000000000000010100")
		if _, err := client.conn.Write(raw); err != nil {
			t.Fatal(err)
		}
		select {
		case <-server.Done():
		case <-ctx.Done():
			t.Fatal("not rejected")
		}
		if !errors.Is(server.Err(), codec.ErrValidation) {
			t.Fatal(server.Err())
		}
		select {
		case in := <-server.incoming:
			t.Fatal("invalid request admitted", in)
		default:
		}
	}
}
