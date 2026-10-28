package codec_test

import (
	"bytes"
	"errors"
	"fmt"
	"testing"

	"github.com/skill215/smppkit/codec"
	"github.com/skill215/smppkit/pdu"
	"github.com/skill215/smppkit/tlv"
)

// Independent frames: no production encoder constructs expected bytes.
// Request: transaction mode, empty service/addresses, no receipt request or TLVs.
const dataTransactionHex = "0000001a 00000103 00000000 00000007 00 000000 000000 02 00 00"

// Response: transaction delivery failure, ID m, failure reason 0, GSM error 1,
// delivery pending flag set. All header/body lengths are literal.
const dataFailureHex = "00000023 80000103 000000fe 00000007 6d00 0425000100 04230003030001 0420000101"

func TestDataTransactionIndependentFrames(t *testing.T) {
	for _, v := range []pdu.Version{pdu.V34, pdu.V50} {
		c := newCodec(t, v)
		request, err := c.Decode(unhex(t, dataTransactionHex))
		if err != nil {
			t.Fatal(err)
		}
		response, err := c.Decode(unhex(t, dataFailureHex))
		if err != nil {
			t.Fatal(err)
		}
		b := request.Body.(*pdu.DataSM)
		if err := c.ValidateDataDirection(b, codec.DataSubmission); err != nil {
			t.Fatal(err)
		}
		if err := c.ValidateDataResponse(response, b.ESMClass, codec.DataSubmission); err != nil {
			t.Fatal(err)
		}
		want := &pdu.DataSMResp{MessageResponseFields: pdu.MessageResponseFields{MessageID: "m", Optional: tlv.Fields{
			{Tag: tlv.DeliveryFailureReason, Value: []byte{0}},
			{Tag: tlv.NetworkErrorCode, Value: []byte{3, 0, 1}},
			{Tag: tlv.DPFResult, Value: []byte{1}},
		}}}
		got, err := c.Append(nil, pdu.PDU{Sequence: 7, Status: pdu.Status(0xfe), Body: want})
		if err != nil || !bytes.Equal(got, unhex(t, dataFailureHex)) {
			t.Fatal(err, fmt.Sprintf("%x", got))
		}
		for _, esm := range []byte{1, 3, 0x41, 0x83} {
			if err := c.ValidateDataResponse(response, esm, codec.DataSubmission); !errors.Is(err, codec.ErrValidation) {
				t.Fatal(esm, err)
			}
		}
		// Default mode is unknown. Do not guess the MC's configured default.
		if err := c.ValidateDataResponse(response, 0, codec.DataSubmission); err != nil {
			t.Fatal(err)
		}
		response.Status = pdu.OK
		if err := c.ValidateDataResponse(response, 2, codec.DataSubmission); !errors.Is(err, codec.ErrValidation) {
			t.Fatal("failure reason on successful transaction", err)
		}
		// A response without request context is still a valid wire structure.
		if _, err := c.Append(nil, response); err != nil {
			t.Fatal(err)
		}
	}
}

func TestDataTransactionResponseMatrix(t *testing.T) {
	// Literal TLVs independently cover 3.4's footnoted subset vs 5.0's whole
	// submission-response table, universal congestion and opaque customer tags.
	cases := []struct {
		wire           string
		only34, only50 bool
	}{
		{"0420000100", true, true},
		{"0425000103", true, true},
		{"04230003030000", true, true},
		{"001d000462616400", false, true},
		{"15010001ff 1501000100", false, false},
		{"0428000164", false, false},
	}
	for _, v := range []pdu.Version{pdu.V34, pdu.V50} {
		c := newCodec(t, v)
		for _, tc := range cases {
			if v == pdu.V34 && tc.wire == "0428000164" {
				continue
			}
			raw := independentTLVFrame(t, pdu.DataSMRespID, unhex(t, tc.wire))
			raw[11] = 0xfe // delivery failure; not synthesized by Codec
			packet, err := c.Decode(raw)
			if err != nil {
				t.Fatal(err)
			}
			for esm := range 256 {
				typ := esm & 0x3c
				validESM := typ == 0 || typ == 8 || typ == 16 || v == pdu.V50 && (typ == 4 || typ == 24 || typ == 32)
				transactionOnly := tc.only34
				if v == pdu.V50 {
					transactionOnly = tc.only50
				}
				want := validESM && (!transactionOnly || esm&3 == 0 || esm&3 == 2)
				err := c.ValidateDataResponse(packet, byte(esm), codec.DataSubmission)
				if (err == nil) != want {
					t.Fatalf("version=%x esm=%02x tlv=%s: %v", v, esm, tc.wire, err)
				}
			}
			got, err := c.Append(nil, packet)
			if err != nil || !bytes.Equal(got, raw) {
				t.Fatal("context mutated wire", err)
			}
		}
		// Delivery request mode bits cannot recover the original submission mode.
		for _, esm := range []byte{0, 1, 2, 3} {
			for _, wire := range []string{"0425000101", "04230003030000", "001d000462616400"} {
				packet, err := c.Decode(independentTLVFrame(t, pdu.DataSMRespID, unhex(t, wire)))
				if err != nil {
					t.Fatal(err)
				}
				packet.Status = pdu.SystemError
				if err := c.ValidateDataResponse(packet, esm, codec.DataDelivery); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
}

func TestDataDatagramRegisteredDelivery(t *testing.T) {
	for _, v := range []pdu.Version{pdu.V34, pdu.V50} {
		c := newCodec(t, v)
		for mode := range byte(4) {
			for registered := range 256 {
				raw := unhex(t, dataTransactionHex)
				raw[23], raw[24] = mode, byte(registered)
				packet, err := c.Decode(raw)
				valid := registered&0xe0 == 0 && (v == pdu.V50 || registered&3 != 3)
				if (err == nil) != valid {
					t.Fatal(v, registered, err)
				}
				if !valid {
					continue
				}
				for _, direction := range []codec.DataDirection{codec.DataSubmission, codec.DataDelivery} {
					want := direction == codec.DataDelivery || mode != 1 || registered == 0
					err := c.ValidateDataDirection(packet.Body, direction)
					if (err == nil) != want {
						t.Fatal(v, mode, registered, direction, err)
					}
				}
			}
		}
	}
}

func TestDataResponseContextContract(t *testing.T) {
	c := newCodec(t, pdu.V50)
	for _, body := range []pdu.Body{nil, (*pdu.DataSMResp)(nil), (*pdu.GenericNACK)(nil), &pdu.DataSM{}, &pdu.SubmitSMResp{}, &pdu.Omitted{ID: pdu.SubmitSMRespID}} {
		if err := c.ValidateDataResponse(pdu.PDU{Body: body}, 2, codec.DataSubmission); !errors.Is(err, codec.ErrValidation) {
			t.Fatal(body, err)
		}
	}
	for _, direction := range []codec.DataDirection{0, 3, 255} {
		if c.ValidateDataResponse(pdu.PDU{Body: &pdu.DataSMResp{}}, 2, direction) == nil {
			t.Fatal("invalid direction")
		}
	}
	if err := newCodec(t, pdu.V33).ValidateDataResponse(pdu.PDU{Body: &pdu.DataSMResp{}}, 2, codec.DataSubmission); !errors.Is(err, codec.ErrUnsupported) {
		t.Fatal(err)
	}
	for _, body := range []pdu.Body{&pdu.Omitted{ID: pdu.DataSMRespID}, &pdu.GenericNACK{}} {
		for _, mode := range []byte{0, 1, 2, 3} {
			packet := pdu.PDU{Sequence: 1, Status: pdu.SystemError, Body: body}
			if err := c.ValidateDataResponse(packet, mode, codec.DataSubmission); err != nil {
				t.Fatal(err)
			}
			if _, err := c.Append(nil, packet); err != nil {
				t.Fatal(err)
			}
			packet.Status = pdu.OK
			if _, err := c.Append(nil, packet); err == nil {
				t.Fatal("ordinary status validation bypassed")
			}
		}
	}
	var calls int
	c, err := codec.New(codec.Config{Version: pdu.V50, TLVs: []codec.TLVDefinition{{Tag: 0x1501, MinLength: 1, MaxLength: 1, Versions: []pdu.Version{pdu.V50}, Commands: []pdu.CommandID{pdu.DataSMRespID}, Validate: func([]byte) error { calls++; return nil }}}})
	if err != nil {
		t.Fatal(err)
	}
	packet := pdu.PDU{Sequence: 1, Body: &pdu.DataSMResp{MessageResponseFields: pdu.MessageResponseFields{Optional: tlv.Fields{{Tag: 0x1501, Value: []byte{1}}}}}}
	if _, err := c.Append(nil, packet); err != nil {
		t.Fatal(err)
	}
	if err := c.ValidateDataResponse(packet, 3, codec.DataSubmission); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatal("context called customer validator", calls)
	}
}

func FuzzDataResponse(f *testing.F) {
	for _, raw := range []string{dataFailureHex, dataDPFResponseHex, "000000198000010300000008000000076d00001d00036f6b00"} {
		f.Add(byte(0), byte(2), unhex(f, raw))
	}
	f.Fuzz(func(t *testing.T, profile, esm byte, raw []byte) {
		if len(raw) > 65536 {
			t.Skip()
		}
		c := newCodec(t, []pdu.Version{pdu.V34, pdu.V50}[int(profile)%2])
		packet, err := c.Decode(raw)
		if err != nil {
			return
		}
		for _, direction := range []codec.DataDirection{codec.DataSubmission, codec.DataDelivery} {
			err := c.ValidateDataResponse(packet, esm, direction)
			if (c.ValidateDataResponse(packet, esm, direction) == nil) != (err == nil) {
				t.Fatal("non-deterministic context")
			}
			encoded, err := c.Append(nil, packet)
			if err != nil || !bytes.Equal(encoded, raw) {
				t.Fatal("context mutated decoded frame", err)
			}
		}
	})
}
