package codec_test

import (
	"bytes"
	"errors"
	"testing"

	"github.com/skill215/smppkit/codec"
	"github.com/skill215/smppkit/pdu"
	"github.com/skill215/smppkit/tlv"
)

// Independent synthetic frames, not produced by the encoder under test.
const alertNotificationHex = "00000021 00000102 00000000 00000007 010131323300 050061707000 0422000100"
const outbindHex = "00000016 0000000b 00000000 00000007 6d6300 707700"

func TestNotificationIndependentFrames(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		body pdu.Body
	}{
		{alertNotificationHex, &pdu.AlertNotification{Source: pdu.Address{TON: 1, NPI: 1, Value: "123"}, ESME: pdu.Address{TON: 5, Value: "app"}, Optional: tlv.Fields{{Tag: tlv.MSAvailabilityStatus, Value: []byte{0}}}}},
		{outbindHex, &pdu.Outbind{SystemID: "mc", Password: "pw"}},
	} {
		for _, v := range []pdu.Version{pdu.V33, pdu.V34, pdu.V50} {
			c := newCodec(t, v)
			raw := unhex(t, tc.raw)
			encoded, err := c.Append(nil, pdu.PDU{Sequence: 7, Body: tc.body})
			if v == pdu.V33 {
				if !errors.Is(err, codec.ErrUnsupported) {
					t.Fatal(err)
				}
				if _, err = c.Decode(raw); !errors.Is(err, codec.ErrUnsupported) {
					t.Fatal(err)
				}
				continue
			}
			if err != nil || !bytes.Equal(encoded, raw) {
				t.Fatal(err, encoded)
			}
			packet, err := c.Decode(raw)
			if err != nil {
				t.Fatal(err)
			}
			if _, ok := c.ResponseCommand(packet.Body.CommandID()); ok {
				t.Fatal("one-way command has response")
			}
			for n := range raw {
				if _, err := c.Decode(raw[:n]); err == nil {
					t.Fatal("truncated frame accepted", n)
				}
			}
			// Encoded bodies are not aliases of the input frame.
			for i := 16; i < len(raw); i++ {
				raw[i] = 0xff
			}
			got, err := c.Append(nil, packet)
			if err != nil || !bytes.Equal(got, encoded) {
				t.Fatal("decoded body aliases wire", err)
			}
		}
	}
}
