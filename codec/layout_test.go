package codec_test

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"testing"

	"github.com/skill215/smppkit/pdu"
)

// Unlike the ordinary mandatory-layout table, generic_nack requires a nonzero
// status and permits sequence zero. Keep literal headers for every profile,
// including 3.3, rather than relying on the 5.0 congestion-response sweep.
func TestIndependentGenericNACKProfiles(t *testing.T) {
	for _, version := range []pdu.Version{pdu.V33, pdu.V34, pdu.V50} {
		t.Run(fmt.Sprintf("%x", version), func(t *testing.T) {
			c := newCodec(t, version)
			for _, raw := range [][]byte{
				unhex(t, "00000010 80000000 00000008 00000000"),
				unhex(t, "00000010 80000000 00000008 7fffffff"),
			} {
				packet, err := c.Decode(raw)
				if err != nil {
					t.Fatal(err)
				}
				if _, ok := packet.Body.(*pdu.GenericNACK); !ok || packet.Status != pdu.SystemError || packet.Sequence != binary.BigEndian.Uint32(raw[12:]) {
					t.Fatalf("wrong NACK identity: %+v", packet)
				}
				encoded, err := c.Append(nil, packet)
				if err != nil || !bytes.Equal(encoded, raw) {
					t.Fatalf("NACK bytes=%x err=%v", encoded, err)
				}
			}
			for _, raw := range []string{
				"00000010 80000000 00000000 00000001", // a successful NACK is invalid
				"00000010 80000000 00000008 80000000", // sequence exceeds signed range
				"0000000f 80000000 00000008 000000",   // truncated header
			} {
				if _, err := c.Decode(unhex(t, raw)); err == nil {
					t.Fatalf("accepted malformed NACK: %s", raw)
				}
			}
			// Congestion is legal on every 5.0 response; 3.3/3.4 NACK has no
			// optional section. The frame is independent of the encoder.
			raw := unhex(t, "00000015 80000000 00000008 00000001 0428000101")
			_, err := c.Decode(raw)
			if (err == nil) != (version == pdu.V50) {
				t.Fatalf("NACK TLV profile %x: %v", version, err)
			}
		})
	}
}

// Mandatory zero-value body vectors are authored by counting ordered fields
// in S33 §6.3, S34 §4, S50 §4. These do not call a production encoder to build
// expected bytes; every command/response listed as an implemented layout is
// covered. Nonzero field/width/order cases live in codec_test.go.
func TestIndependentMandatoryLayouts(t *testing.T) {
	for _, v := range []pdu.Version{pdu.V33, pdu.V34, pdu.V50} {
		c := newCodec(t, v)
		bind := pdu.BindFields{InterfaceVersion: v}
		cases := []struct {
			body      pdu.Body
			mandatory []byte
		}{
			{&pdu.BindReceiver{BindFields: bind}, []byte{0, 0, 0, byte(v), 0, 0, 0}},
			{&pdu.BindTransmitter{BindFields: bind}, []byte{0, 0, 0, byte(v), 0, 0, 0}},
			{&pdu.BindReceiverResp{}, []byte{0}},
			{&pdu.BindTransmitterResp{}, []byte{0}},
			{&pdu.SubmitSM{}, make([]byte, 17)}, {&pdu.DeliverSM{}, make([]byte, 17)},
			{&pdu.SubmitSMResp{}, []byte{0}}, {&pdu.DeliverSMResp{}, []byte{0}},
			{&pdu.SubmitMulti{Destinations: []pdu.MultiDestination{{Kind: pdu.SMEAddress}}}, []byte{0, 0, 0, 0, 1, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}},
			{&pdu.SubmitMultiResp{}, []byte{0, 0}},
			{&pdu.QuerySM{}, make([]byte, 4)}, {&pdu.QuerySMResp{MessageState: 1}, []byte{0, 0, 1, 0}},
			{&pdu.ReplaceSM{}, make([]byte, 9)}, {&pdu.ReplaceSMResp{}, nil},
			{&pdu.CancelSM{}, make([]byte, 8)}, {&pdu.CancelSMResp{}, nil},
			{&pdu.Unbind{}, nil}, {&pdu.UnbindResp{}, nil},
			{&pdu.EnquireLink{}, nil}, {&pdu.EnquireLinkResp{}, nil},
		}
		if v != pdu.V33 {
			cases = append(cases, struct {
				body      pdu.Body
				mandatory []byte
			}{&pdu.BindTransceiver{BindFields: bind}, []byte{0, 0, 0, byte(v), 0, 0, 0}}, struct {
				body      pdu.Body
				mandatory []byte
			}{&pdu.BindTransceiverResp{}, []byte{0}}, struct {
				body      pdu.Body
				mandatory []byte
			}{&pdu.DataSM{}, make([]byte, 10)}, struct {
				body      pdu.Body
				mandatory []byte
			}{&pdu.DataSMResp{}, []byte{0}}, struct {
				body      pdu.Body
				mandatory []byte
			}{&pdu.Outbind{}, []byte{0, 0}}, struct {
				body      pdu.Body
				mandatory []byte
			}{&pdu.AlertNotification{}, make([]byte, 6)})
		}
		for _, tc := range cases {
			expected := make([]byte, 16)
			binary.BigEndian.PutUint32(expected, uint32(16+len(tc.mandatory)))
			binary.BigEndian.PutUint32(expected[4:], uint32(tc.body.CommandID()))
			binary.BigEndian.PutUint32(expected[12:], 123)
			expected = append(expected, tc.mandatory...)
			got, e := c.Append(nil, pdu.PDU{Sequence: 123, Body: tc.body})
			if e != nil || !bytes.Equal(got, expected) {
				t.Fatalf("version=%x command=%08x got=%x expected=%x error=%v", v, tc.body.CommandID(), got, expected, e)
			}
			if _, e = c.Decode(expected); e != nil {
				t.Fatalf("version=%x command=%08x: %v", v, tc.body.CommandID(), e)
			}
		}
	}
}
