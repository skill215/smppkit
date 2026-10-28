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
	"github.com/skill215/smppkit/pdu"
)

// This peer constructs headers from protocol integer fields and literal body
// bytes. It never calls Codec, PDU serializers or wire helpers; only the library
// endpoint below uses Codec. This is not evidence of an external SMSC implementation.
func independentPacket(id, status, sequence uint32, body string) []byte {
	raw := lifecycleHex(body)
	frame := make([]byte, 16+len(raw))
	binary.BigEndian.PutUint32(frame, uint32(len(frame)))
	binary.BigEndian.PutUint32(frame[4:], id)
	binary.BigEndian.PutUint32(frame[8:], status)
	binary.BigEndian.PutUint32(frame[12:], sequence)
	copy(frame[16:], raw)
	return frame
}

func TestLifecycleIndependentTCPVersions(t *testing.T) {
	for _, v := range []pdu.Version{pdu.V33, pdu.V34, pdu.V50} {
		t.Run(fmt.Sprintf("%x", v), func(t *testing.T) {
			ln, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer ln.Close()
			ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
			defer cancel()
			timedOut := make(chan struct{})
			controlRead := make(chan struct{})
			done := make(chan error, 1)
			go func() {
				conn, err := ln.Accept()
				if err != nil {
					done <- err
					return
				}
				defer conn.Close()
				if err = conn.SetDeadline(time.Now().Add(2 * time.Second)); err != nil {
					done <- err
					return
				}
				write := func(raw []byte) error {
					for len(raw) > 0 {
						n, e := conn.Write(raw)
						if e != nil {
							return e
						}
						if n == 0 {
							return io.ErrNoProgress
						}
						raw = raw[n:]
					}
					return nil
				}
				expect := func(want []byte) error {
					got := make([]byte, len(want))
					if _, e := io.ReadFull(conn, got); e != nil {
						return e
					}
					if !bytes.Equal(got, want) {
						return fmt.Errorf("independent peer got %x want %x", got, want)
					}
					return nil
				}
				if err = expect(independentPacket(2, 0, 1, fmt.Sprintf("65736d650070770000%02x000000", v))); err != nil {
					done <- err
					return
				}
				bindBody := "6d6300"
				if v != pdu.V33 {
					bindBody += fmt.Sprintf("02100001%02x", v)
				}
				for _, octet := range independentPacket(0x80000002, 0, 1, bindBody) {
					if err = write([]byte{octet}); err != nil {
						done <- err
						return
					}
				}
				// Literal mandatory submit fields plus sm_length=2 and ASCII hi.
				if err = expect(independentPacket(4, 0, 2, "00000000000000 000000 0000 00000000 02 6869")); err != nil {
					done <- err
					return
				}
				submitResponse := independentPacket(0x80000004, 0, 2, "733100")
				if err = write(submitResponse); err != nil {
					done <- err
					return
				}
				if err = expect(independentPacket(3, 0, 3, "733100000000")); err != nil {
					done <- err
					return
				}
				select {
				case <-timedOut:
				case <-ctx.Done():
					done <- ctx.Err()
					return
				}
				if err = expect(independentPacket(0x15, 0, 4, "")); err != nil {
					done <- err
					return
				}
				// Coalesce a late response, a duplicate response, a live response,
				// and an opposite-direction request with that same live sequence.
				coalesced := independentPacket(0x80000003, 0, 3, "733100000100")
				coalesced = append(coalesced, submitResponse...)
				coalesced = append(coalesced, independentPacket(0x80000015, 0, 4, "")...)
				coalesced = append(coalesced, independentPacket(0x15, 0, 4, "")...)
				if err = write(coalesced); err != nil {
					done <- err
					return
				}
				if err = expect(independentPacket(0x80000015, 0, 4, "")); err != nil {
					done <- err
					return
				}
				close(controlRead)
				if err = expect(independentPacket(6, 0, 5, "")); err != nil {
					done <- err
					return
				}
				if err = write(independentPacket(6, 0, 9, "")); err != nil {
					done <- err
					return
				}
				if err = expect(independentPacket(0x80000006, 0, 9, "")); err != nil {
					done <- err
					return
				}
				done <- write(independentPacket(0x80000006, 0, 5, ""))
			}()
			c, err := codec.New(codec.Config{Version: v})
			if err != nil {
				t.Fatal(err)
			}
			s, err := Dial(ctx, ln.Addr().String(), ClientConfig{Codec: c, Bind: &pdu.BindTransmitter{BindFields: pdu.BindFields{SystemID: "esme", Password: "pw", InterfaceVersion: v}}})
			if err != nil {
				t.Fatal(err)
			}
			defer func() { s.Close(); s.Wait() }()
			r, err := s.Request(ctx, &pdu.SubmitSM{SMFields: pdu.SMFields{ShortMessage: []byte("hi")}})
			if err != nil || r.Response.Body.(*pdu.SubmitSMResp).MessageID != "s1" {
				t.Fatal(r, err)
			}
			short, stop := context.WithTimeout(ctx, 30*time.Millisecond)
			r, err = s.Request(short, &pdu.QuerySM{MessageID: "s1"})
			stop()
			close(timedOut)
			if !errors.Is(err, context.DeadlineExceeded) || r.Outcome != Written {
				t.Fatal(r, err)
			}
			r, err = s.Request(ctx, &pdu.EnquireLink{})
			if err != nil || r.Sequence != 4 || r.Outcome != ResponseReceived {
				t.Fatal(r, err)
			}
			select {
			case <-controlRead:
			case err := <-done:
				t.Fatal(err)
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			if err = s.Shutdown(ctx); err != nil {
				t.Fatal(err)
			}
			if err = <-done; err != nil {
				t.Fatal(err)
			}
			s.Wait()
		})
	}
}
