// SPDX-AI-Disclosure: ai-generated
// SPDX-AI-Model: GPT-6.0 Astra
// SPDX-AI-Provider: OpenAI
// SPDX-AI-Scope: Test code generated and revised with Codex under maintainer direction.
// SPDX-AI-Date: 2026-10-28

package codec_test

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/skill215/smppkit/codec"
	"github.com/skill215/smppkit/pdu"
	"github.com/skill215/smppkit/tlv"
)

// Independently counted field bytes: two destination variants, nonzero flags,
// binary status width, and an opaque message ID. No production encoder builds
// the expected frames (S33 §6.3.4, S34 §4.5, S50 §4.2.3).
const multiRequestHex = "00000035 00000021 00000000 01020304 434d5400 050073726300 02 01010131323300 0267726f757000 00000100000100030002 6869"
const multiResponseHex = "00000020 80000021 00000000 01020304 69643a3700 01 010131323300 00000058"

func sampleMulti() *pdu.SubmitMulti {
	return &pdu.SubmitMulti{ServiceType: "CMT", Source: pdu.Address{TON: 5, Value: "src"}, Destinations: []pdu.MultiDestination{
		{Kind: pdu.SMEAddress, Address: pdu.Address{TON: 1, NPI: 1, Value: "123"}}, {Kind: pdu.DistributionList, ListName: "group"},
	}, PriorityFlag: 1, RegisteredDelivery: 1, DataCoding: 3, ShortMessage: []byte("hi")}
}
func TestSubmitMultiIndependentVectors(t *testing.T) {
	for _, v := range []pdu.Version{pdu.V33, pdu.V34, pdu.V50} {
		c := newCodec(t, v)
		cases := []struct {
			hex  string
			body pdu.Body
		}{
			{multiRequestHex, sampleMulti()},
			{multiResponseHex, &pdu.SubmitMultiResp{MessageID: "id:7", Unsuccessful: []pdu.UnsuccessfulSME{{Address: pdu.Address{TON: 1, NPI: 1, Value: "123"}, Status: pdu.Throttled}}}},
		}
		for _, tc := range cases {
			want := pdu.PDU{Sequence: 0x01020304, Body: tc.body}
			raw := unhex(t, tc.hex)
			got, err := c.Append(nil, want)
			if err != nil || !bytes.Equal(got, raw) {
				t.Fatalf("v=%x got=%x want=%x err=%v", v, got, raw, err)
			}
			decoded, err := c.Decode(raw)
			if err != nil || !reflect.DeepEqual(decoded, want) {
				t.Fatalf("v=%x decoded=%+v err=%v", v, decoded, err)
			}
			for n := 16; n < len(raw); n++ {
				cut := bytes.Clone(raw[:n])
				binary.BigEndian.PutUint32(cut, uint32(n))
				if _, err := c.Decode(cut); err == nil {
					t.Fatalf("v=%x accepted prefix %d", v, n)
				}
			}
		}
	}
}
func TestSubmitMultiVersionBoundsAndMetadata(t *testing.T) {
	for _, v := range []pdu.Version{pdu.V33, pdu.V34, pdu.V50} {
		c := newCodec(t, v)
		for _, n := range []int{0, 1, 254, 255, 256} {
			b := sampleMulti()
			b.Destinations = make([]pdu.MultiDestination, n)
			for i := range b.Destinations {
				b.Destinations[i].Kind = pdu.SMEAddress
			}
			raw, err := c.Append(nil, pdu.PDU{Sequence: 1, Body: b})
			valid := n >= 1 && n <= 255 && (v != pdu.V34 || n <= 254)
			if (err == nil) != valid {
				t.Fatalf("v=%x dests=%d err=%v", v, n, err)
			}
			if valid {
				if _, err = c.Decode(raw); err != nil {
					t.Fatal(err)
				}
			}
		}
		for _, n := range []int{160, 161, 254, 255, 256} {
			b := sampleMulti()
			b.ShortMessage = make([]byte, n)
			_, err := c.Append(nil, pdu.PDU{Sequence: 1, Body: b})
			valid := n <= 255 && (v != pdu.V33 || n <= 160) && (v != pdu.V34 || n <= 254)
			if (err == nil) != valid {
				t.Fatalf("v=%x message=%d err=%v", v, n, err)
			}
		}
		invalid := []*pdu.SubmitMulti{sampleMulti(), sampleMulti(), sampleMulti(), sampleMulti(), sampleMulti()}
		invalid[0].Destinations[0].Kind = 3
		invalid[1].Destinations[0].ListName = "ignored"
		invalid[2].Destinations[1].Address.Value = "ignored"
		invalid[3].Destinations[1].ListName = strings.Repeat("x", 21)
		invalid[4].Destinations[0].Address.Value = strings.Repeat("x", 21)
		for _, b := range invalid {
			if _, err := c.Append(nil, pdu.PDU{Sequence: 1, Body: b}); err == nil {
				t.Fatalf("v=%x invalid destination accepted", v)
			}
		}
		for _, offset := range []int{26, 27, 34} {
			raw := unhex(t, multiRequestHex)
			raw[offset] = 0
			if _, err := c.Decode(raw); err == nil {
				t.Fatal("invalid count/flag accepted", offset)
			}
		}
		b := sampleMulti()
		b.ReplaceIfPresent = 1
		_, err := c.Append(nil, pdu.PDU{Sequence: 1, Body: b})
		if (err == nil) != (v == pdu.V50) {
			t.Fatalf("v=%x multi replacement: %v", v, err)
		}
		b.Destinations = b.Destinations[:1]
		_, err = c.Append(nil, pdu.PDU{Sequence: 1, Body: b})
		if (err == nil) != (v != pdu.V34) {
			t.Fatalf("v=%x single replacement: %v", v, err)
		}
	}
}
func TestSubmitMultiOptionalAndNegativeBodies(t *testing.T) {
	for _, v := range []pdu.Version{pdu.V33, pdu.V34, pdu.V50} {
		c := newCodec(t, v)
		b := sampleMulti()
		b.ShortMessage = nil
		b.Optional = tlv.Fields{{Tag: tlv.MessagePayload, Value: []byte{0, 0xff}}}
		raw, err := c.Append(nil, pdu.PDU{Sequence: 1, Body: b})
		if (err == nil) != (v != pdu.V33) {
			t.Fatal(v, err)
		}
		if err == nil {
			if _, err = c.Decode(raw); err != nil {
				t.Fatal(err)
			}
		}
		b.ShortMessage = []byte{1}
		if _, err = c.Append(nil, pdu.PDU{Sequence: 1, Body: b}); err == nil {
			t.Fatal("two payloads accepted")
		}
		_, err = c.Append(nil, pdu.PDU{Sequence: 1, Status: pdu.Throttled, Body: &pdu.Omitted{ID: pdu.SubmitMultiRespID}})
		if (err == nil) != (v != pdu.V33) {
			t.Fatal(v, err)
		}
		raw = unhex(t, "00000010 80000021 00000058 00000001")
		if _, err = c.Decode(raw); (err == nil) != (v != pdu.V33) {
			t.Fatal(v, err)
		}
		raw[11] = 0
		if _, err = c.Decode(raw); err == nil {
			t.Fatal("header-only success accepted")
		}
		resp := &pdu.SubmitMultiResp{Optional: tlv.Fields{{Tag: tlv.CongestionState, Value: []byte{50}}}}
		_, err = c.Append(nil, pdu.PDU{Sequence: 1, Body: resp})
		if (err == nil) != (v == pdu.V50) {
			t.Fatal(v, err)
		}
		resp.Optional = nil
		resp.Unsuccessful = make([]pdu.UnsuccessfulSME, 256)
		if _, err = c.Append(nil, pdu.PDU{Sequence: 1, Body: resp}); !errors.Is(err, codec.ErrValidation) {
			t.Fatal(err)
		}
	}
}
func TestSubmitMultiOwnership(t *testing.T) {
	original := sampleMulti()
	original.Optional = tlv.Fields{{Tag: 0x1500, Value: []byte{9}}}
	copy := original.Clone()
	copy.Destinations[0].Address.Value = "changed"
	copy.ShortMessage[0] = 0
	copy.Optional[0].Value[0] = 0
	if original.Destinations[0].Address.Value != "123" || string(original.ShortMessage) != "hi" || original.Optional[0].Value[0] != 9 {
		t.Fatal("clone aliases input")
	}
	raw := unhex(t, multiRequestHex)
	decoded, err := newCodec(t, pdu.V50).Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	clear(raw)
	if string(decoded.Body.(*pdu.SubmitMulti).ShortMessage) != "hi" {
		t.Fatal("decoded payload aliases frame")
	}
}
func FuzzSubmitMulti(f *testing.F) {
	f.Add(byte(1), byte(2), []byte("hello"))
	f.Add(byte(0), byte(255), []byte{0xff, 0})
	f.Fuzz(func(t *testing.T, profile, count byte, payload []byte) {
		if len(payload) > 300 {
			t.Skip()
		}
		v := []pdu.Version{pdu.V33, pdu.V34, pdu.V50}[int(profile)%3]
		c := newCodec(t, v)
		body := sampleMulti()
		body.ShortMessage = payload
		body.Destinations = make([]pdu.MultiDestination, int(count))
		for i := range body.Destinations {
			body.Destinations[i] = pdu.MultiDestination{Kind: pdu.SMEAddress, Address: pdu.Address{Value: "123"}}
		}
		raw, err := c.Append(nil, pdu.PDU{Sequence: 1, Body: body})
		if err != nil {
			return
		}
		got, err := c.Decode(raw)
		if err != nil {
			t.Fatal(err)
		}
		encoded, err := c.Append(nil, got)
		if err != nil || !bytes.Equal(encoded, raw) {
			t.Fatal("unstable encoding", err)
		}
	})
}

func BenchmarkSubmitMultiCodec(b *testing.B) {
	for _, count := range []int{2, 255} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			c := newCodec(b, pdu.V50)
			request := sampleMulti()
			request.Destinations = make([]pdu.MultiDestination, count)
			for i := range request.Destinations {
				request.Destinations[i] = pdu.MultiDestination{Kind: pdu.SMEAddress, Address: pdu.Address{TON: 1, NPI: 1, Value: "123456789"}}
			}
			request.Optional = tlv.Fields{{Tag: tlv.PrivacyIndicator, Value: []byte{1}}, {Tag: tlv.CallbackNum, Value: []byte{1, 1, 1, '1'}}}
			raw, err := c.Append(nil, pdu.PDU{Sequence: 1, Body: request})
			if err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.SetBytes(int64(len(raw)))
			for b.Loop() {
				if _, err := c.Decode(raw); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
