package smpp

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"testing"
	"time"

	"github.com/skill215/smppkit/codec"
	"github.com/skill215/smppkit/message"
	"github.com/skill215/smppkit/pdu"
)

const rv21Data = "0000002000000103000000000000000100000000000000020000042400024869"
const rv21Submit = "0000002300000004000000000000000100000000000000020000000000000000024869"

// Only the receiver uses Codec. Peer requests and response header expectations
// are independent bytes. No delivery/consumer worker runs during admission.
func rv21Session(t *testing.T, v pdu.Version, server bool) (*Session, net.Conn, context.Context) {
	t.Helper()
	c, err := codec.New(codec.Config{Version: v})
	if err != nil {
		t.Fatal(err)
	}
	a, b := net.Pipe()
	s, err := newSession(a, c, Config{}, server, Bound, Transceiver)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	if err = b.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cancel(); s.Close(); b.Close(); s.Wait() })
	return s, b, ctx
}

func rv21Inbound(t *testing.T, s *Session, peer net.Conn, ctx context.Context, raw []byte) *Inbound {
	t.Helper()
	done := make(chan error, 1)
	go func() { _, err := peer.Write(raw); done <- err }()
	in, err := s.Receive(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = <-done; err != nil {
		t.Fatal(err)
	}
	return in
}

// Checks the wire header directly; a bounded read prevents a malformed length
// from turning a test failure into a hang or an oversized allocation.
func rv21ReadResponse(peer net.Conn, id pdu.CommandID, status pdu.Status) error {
	var header [16]byte
	if _, err := io.ReadFull(peer, header[:]); err != nil {
		return err
	}
	n := binary.BigEndian.Uint32(header[:4])
	if n < 16 || n > 1024 {
		return fmt.Errorf("invalid response length %d", n)
	}
	if _, err := io.ReadFull(peer, make([]byte, n-16)); err != nil {
		return err
	}
	if binary.BigEndian.Uint32(header[4:8]) != uint32(id) || binary.BigEndian.Uint32(header[8:12]) != uint32(status) || binary.BigEndian.Uint32(header[12:]) != 1 {
		return fmt.Errorf("unexpected response header %x", header)
	}
	return nil
}

func TestRV21BufferedACKPolicy(t *testing.T) {
	for _, v := range []pdu.Version{pdu.V33, pdu.V34, pdu.V50} {
		for _, kind := range []string{"submit", "data", "deliver", "data-delivery"} {
			if v == pdu.V33 && (kind == "data" || kind == "data-delivery") {
				continue
			}
			for _, mode := range []byte{0, 1, 2, 3, 0x12} {
				if v == pdu.V33 && kind == "submit" && mode != 0 {
					continue
				}
				t.Run(fmt.Sprintf("%x/%s/esm=%02x", v, kind, mode), func(t *testing.T) {
					server := kind == "submit" || kind == "data"
					raw := r1Wire(rv21Submit)
					if kind == "data" || kind == "data-delivery" {
						raw = r1Wire(rv21Data)
					}
					if kind == "deliver" {
						raw[7] = 5
					}
					raw[23] = mode
					s, peer, ctx := rv21Session(t, v, server)
					in := rv21Inbound(t, s, peer, ctx, raw)
					receiver, err := message.NewReceiver(message.ReceiveConfig{AckPolicy: message.AckOnBuffered})
					if err != nil {
						t.Fatal(err)
					}
					reject := server && v != pdu.V33 && mode&3 == 2
					response := pdu.CommandID(binary.BigEndian.Uint32(raw[4:8])) | pdu.ResponseBit
					ack := make(chan error, 1)
					status := pdu.OK
					if reject {
						status = pdu.SystemError
					}
					go func() { ack <- rv21ReadResponse(peer, response, status) }()
					err = AdmitMessage(ctx, receiver, in, "")
					if reject {
						if !errors.Is(err, ErrState) {
							t.Fatalf("want ErrState, got %v", err)
						}
						if receiver.Stats() != (message.Stats{}) {
							t.Fatalf("rejected submission retained state: %+v", receiver.Stats())
						}
						// The first peer-visible response must be the application's explicit
						// rejection. An earlier automatic OK or a claimed handle fails this check.
						if err = in.Respond(ctx, pdu.SystemError, &pdu.Omitted{ID: response}); err != nil {
							t.Fatal(err)
						}
					} else {
						if err != nil {
							t.Fatal(err)
						}
						if receiver.Stats().Queued != 1 {
							t.Fatal("allowed message was not queued")
						}
						msg, err := receiver.Receive(ctx)
						if err != nil || !bytes.Equal(msg.Payload, []byte("Hi")) {
							t.Fatalf("payload=%x err=%v", msg.Payload, err)
						}
					}
					if err = <-ack; err != nil {
						t.Fatal(err)
					}
				})
			}
		}
	}
}

func TestRV21OriginalTransactionSurvivesEdits(t *testing.T) {
	for _, raw := range []string{rv21Data, rv21Submit} {
		t.Run(raw[8:16], func(t *testing.T) {
			s, peer, ctx := rv21Session(t, pdu.V50, true)
			in := rv21Inbound(t, s, peer, ctx, r1Wire(raw))
			// Replacing the body must not erase the original submission mode/identity.
			in.PDU = pdu.PDU{Body: &pdu.DeliverSM{SMFields: pdu.SMFields{ShortMessage: []byte("Hi")}}}
			receiver, _ := message.NewReceiver(message.ReceiveConfig{AckPolicy: message.AckOnBuffered})
			if err := AdmitMessage(ctx, receiver, in, ""); !errors.Is(err, ErrState) {
				t.Fatalf("got %v", err)
			}
			if receiver.Stats() != (message.Stats{}) || in.responded.Load() {
				t.Fatal("modified transaction was admitted/claimed")
			}
		})
	}
}

func TestRV21RunReceiverClosesWithoutACK(t *testing.T) {
	s, peer, ctx := rv21Session(t, pdu.V50, true)
	receiver, _ := message.NewReceiver(message.ReceiveConfig{AckPolicy: message.AckOnBuffered})
	done := make(chan error, 1)
	go func() { done <- s.RunReceiver(ctx, receiver, "") }()
	if _, err := peer.Write(r1Wire(rv21Data)); err != nil {
		t.Fatal(err)
	}
	var b [1]byte
	n, err := peer.Read(b[:])
	if n != 0 || err == nil {
		t.Fatalf("unexpected ACK bytes: n=%d err=%v", n, err)
	}
	if err = <-done; !errors.Is(err, ErrState) {
		t.Fatalf("RunReceiver: %v", err)
	}
	s.Wait()
	if s.State() != Closed || receiver.Stats() != (message.Stats{}) {
		t.Fatal("transaction retained resources")
	}
}
