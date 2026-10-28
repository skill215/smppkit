// SPDX-AI-Disclosure: ai-generated
// SPDX-AI-Model: GPT-6.0 Astra
// SPDX-AI-Provider: OpenAI
// SPDX-AI-Scope: Test code generated and revised with Codex under maintainer direction.
// SPDX-AI-Date: 2026-10-28

package codec_test

import (
	"bytes"
	"encoding/hex"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/skill215/smppkit/codec"
	"github.com/skill215/smppkit/pdu"
	"github.com/skill215/smppkit/tlv"
)

func newCodec(t testing.TB, v pdu.Version) *codec.Codec {
	t.Helper()
	c, e := codec.New(codec.Config{Version: v})
	if e != nil {
		t.Fatal(e)
	}
	return c
}
func unhex(t testing.TB, s string) []byte {
	t.Helper()
	b, e := hex.DecodeString(strings.ReplaceAll(s, " ", ""))
	if e != nil {
		t.Fatal(e)
	}
	return b
}

// Fixtures are hand-constructed from SMPP 3.4 §4.1/4.4/4.6 and 5.0 §3.2,
// not captured traffic and not produced by another smppkit encoder.
func TestIndependentWireVectors(t *testing.T) {
	cases := []struct {
		name, hex string
		v         pdu.Version
		p         pdu.PDU
	}{
		{"enquire", "00000010 00000015 00000000 0000002a", pdu.V34, pdu.PDU{Sequence: 42, Body: &pdu.EnquireLink{}}},
		{"bind50", "0000001d 00000009 00000000 00000001 65736d6500 707700 00 50 00 00 00", pdu.V50, pdu.PDU{Sequence: 1, Body: &pdu.BindTransceiver{BindFields: pdu.BindFields{SystemID: "esme", Password: "pw", InterfaceVersion: pdu.V50}}}},
		{"submit", "00000029 00000004 00000000 00000007 00 01 01 31323300 01 01 34353600 00 00 00 00 00 01 00 00 00 02 4869", pdu.V34, pdu.PDU{Sequence: 7, Body: &pdu.SubmitSM{SMFields: pdu.SMFields{Source: pdu.Address{TON: 1, NPI: 1, Value: "123"}, Destination: pdu.Address{TON: 1, NPI: 1, Value: "456"}, RegisteredDelivery: 1, ShortMessage: []byte("Hi")}}}},
		{"query response byte widths", "00000016 80000003 00000000 00000007 616200 00 02 ff", pdu.V34, pdu.PDU{Sequence: 7, Body: &pdu.QuerySMResp{MessageID: "ab", MessageState: 2, ErrorCode: 255}}},
		{"omitted error", "00000010 80000004 00000058 00000007", pdu.V50, pdu.PDU{Sequence: 7, Status: pdu.Throttled, Body: &pdu.Omitted{ID: pdu.SubmitSMRespID}}},
		{"present empty success", "00000011 80000004 00000000 00000007 00", pdu.V50, pdu.PDU{Sequence: 7, Body: &pdu.SubmitSMResp{}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := newCodec(t, tc.v)
			want := unhex(t, tc.hex)
			got, e := c.Append(nil, tc.p)
			if e != nil {
				t.Fatal(e)
			}
			if !bytes.Equal(got, want) {
				t.Fatalf("got %x want %x", got, want)
			}
			decoded, e := c.Decode(want)
			if e != nil {
				t.Fatal(e)
			}
			again, e := c.Append(nil, decoded)
			if e != nil || !bytes.Equal(want, again) {
				t.Fatalf("decode: %#v %v", decoded, e)
			}
		})
	}
}
func TestBoundariesAndOwnership(t *testing.T) {
	c := newCodec(t, pdu.V34)
	p := pdu.PDU{Sequence: 1, Body: &pdu.SubmitSM{SMFields: pdu.SMFields{ShortMessage: []byte("A"), Optional: tlv.Fields{{Tag: 0x1500, Value: []byte{1}}, {Tag: 0x1500, Value: []byte{2}}}}}}
	original := p.Body.(*pdu.SubmitSM).SMFields.Clone()
	b, e := c.Append(nil, p)
	if e != nil {
		t.Fatal(e)
	}
	again, _ := c.Append(nil, p)
	if !bytes.Equal(b, again) || !reflect.DeepEqual(original, p.Body.(*pdu.SubmitSM).SMFields) {
		t.Fatal("encoder mutated input")
	}
	out, e := c.Decode(b)
	if e != nil {
		t.Fatal(e)
	}
	clear(b)
	if !bytes.Equal(out.Body.(*pdu.SubmitSM).ShortMessage, []byte("A")) {
		t.Fatal("borrowed decode")
	}
	for _, body := range []pdu.Body{&pdu.SubmitSM{SMFields: pdu.SMFields{ShortMessage: make([]byte, 255)}}, &pdu.BindTransmitter{BindFields: pdu.BindFields{SystemID: "secret\x00bad", InterfaceVersion: pdu.V34}}, &pdu.SubmitSM{SMFields: pdu.SMFields{Optional: tlv.Fields{{Tag: tlv.SARMsgRefNum, Value: []byte{1}}}}}, &pdu.Opaque{ID: pdu.SubmitSMID}} {
		dst := []byte("prefix")
		got, e := c.Append(dst, pdu.PDU{Sequence: 1, Body: body})
		if e == nil || string(got) != "prefix" {
			t.Fatalf("invalid accepted: %T %v", body, e)
		}
		if strings.Contains(e.Error(), "secret") {
			t.Fatal("secret in error")
		}
	}
	for n := range again {
		if _, e := c.Decode(again[:n]); e == nil {
			t.Fatalf("truncation %d accepted", n)
		}
	}
	malformed := unhex(t, "00000010 80000004 00000000 00000001")
	if _, e := c.Decode(malformed); e == nil {
		t.Fatal("missing success body")
	}
	// Upper length rejection must not wait for the rest of even the header.
	r := bytes.NewReader([]byte{0x7f, 0xff, 0xff, 0xff})
	if _, e := c.Read(r); e == nil || errors.Is(e, io.EOF) {
		t.Fatal(e)
	}
}
func TestVersionRules(t *testing.T) {
	for _, v := range []pdu.Version{pdu.V33, pdu.V34, pdu.V50} {
		c := newCodec(t, v)
		for _, n := range []int{160, 161, 254, 255} {
			_, e := c.Append(nil, pdu.PDU{Sequence: 1, Body: &pdu.SubmitSM{SMFields: pdu.SMFields{ShortMessage: make([]byte, n)}}})
			want := v == pdu.V50 && n <= 255 || v == pdu.V34 && n <= 254 || v == pdu.V33 && n <= 160
			if (e == nil) != want {
				t.Fatalf("%x length %d: %v", v, n, e)
			}
		}
		_, e := c.Append(nil, pdu.PDU{Sequence: 1, Body: &pdu.SubmitSM{SMFields: pdu.SMFields{Optional: tlv.Fields{{Tag: 0x1500}}}}})
		if (e != nil) != (v == pdu.V33) {
			t.Fatal(v, e)
		}
		_, e = c.Append(nil, pdu.PDU{Sequence: 1, Body: &pdu.BindTransceiver{BindFields: pdu.BindFields{InterfaceVersion: v}}})
		if (e != nil) != (v == pdu.V33) {
			t.Fatal(v, e)
		}
	}
	if _, e := codec.New(codec.Config{Version: 0x99}); e == nil {
		t.Fatal("unknown version")
	}
	c := newCodec(t, pdu.V33)
	_, e := c.Append(nil, pdu.PDU{Sequence: 1, Body: &pdu.SubmitSMResp{MessageResponseFields: pdu.MessageResponseFields{MessageID: "123456789"}}})
	if e == nil {
		t.Fatal("3.3 ID bound")
	}
	for _, id := range []pdu.CommandID{0xa, 0xc, 0xd, pdu.BroadcastSMID} {
		_, e = c.Decode(frame(id))
		if !errors.Is(e, codec.ErrUnsupported) {
			t.Fatal(id, e)
		}
	}
}
func frame(id pdu.CommandID) []byte {
	return []byte{0, 0, 0, 16, byte(id >> 24), byte(id >> 16), byte(id >> 8), byte(id), 0, 0, 0, 0, 0, 0, 0, 1}
}
func FuzzDecode(f *testing.F) {
	for _, tc := range rv20Frames {
		f.Add(unhex(f, tc.hex))
	}
	f.Add(frame(pdu.EnquireLinkID))
	f.Add([]byte{0, 0, 0, 4})
	f.Add(unhex(f, "00000011 80000004 00000000 00000001 00"))
	f.Add(unhex(f, multiRequestHex))
	f.Add(unhex(f, multiResponseHex))
	f.Add(unhex(f, scheduledSubmitHex))
	f.Add(unhex(f, finalQueryHex))
	f.Add(unhex(f, localQueryHex))
	f.Add(unhex(f, dataReceiptHex))
	f.Add(unhex(f, dataTTLHex))
	f.Add(unhex(f, dataDPFResponseHex))
	f.Add(unhex(f, dataTransactionHex))
	f.Add(unhex(f, dataFailureHex))
	f.Add(unhex(f, alertNotificationHex))
	f.Add(unhex(f, outbindHex))
	// Independent lifecycle-peer frames, including coalesced input which Decode
	// must reject as one PDU (Read is responsible for stream frame boundaries).
	f.Add(unhex(f, "00000010 00000006 00000000 00000009"))
	f.Add(unhex(f, "00000010 80000006 00000000 00000001"))
	f.Add(unhex(f, "00000016 80000003 00000000 00000003 733100000100"))
	f.Add(unhex(f, "00000010 80000015 00000000 00000004 00000010 00000015 00000000 00000004"))
	for _, v := range []pdu.Version{pdu.V33, pdu.V34, pdu.V50} {
		_ = newCodec(f, v)
	}
	f.Fuzz(func(t *testing.T, b []byte) {
		if len(b) > 65536 {
			return
		}
		for _, v := range []pdu.Version{pdu.V33, pdu.V34, pdu.V50} {
			c := newCodec(t, v)
			p, e := c.Decode(b)
			if e == nil {
				got, e := c.Append(nil, p)
				if e != nil || !bytes.Equal(got, b) {
					t.Fatalf("accepted but not preserved: %v", e)
				}
				assertRV20Isolation(t, p, b)
			}
		}
	})
}
func BenchmarkSubmitCodec(b *testing.B) {
	c := newCodec(b, pdu.V50)
	p := pdu.PDU{Sequence: 1, Body: &pdu.SubmitSM{SMFields: pdu.SMFields{ShortMessage: bytes.Repeat([]byte{'A'}, 140)}}}
	wire, _ := c.Append(nil, p)
	b.ReportAllocs()
	b.SetBytes(int64(len(wire)))
	for b.Loop() {
		if _, e := c.Decode(wire); e != nil {
			b.Fatal(e)
		}
	}
}

func FuzzEncodeSM(f *testing.F) {
	f.Add("source", "Hi", []byte{0x15, 0, 1})
	f.Add("bad\x00address", "", []byte{})
	f.Fuzz(func(t *testing.T, address, text string, raw []byte) {
		if len(address)+len(text)+len(raw) > 65536 {
			return
		}
		body := &pdu.SubmitSM{SMFields: pdu.SMFields{Source: pdu.Address{Value: address}, ShortMessage: []byte(text), Optional: tlv.Fields{{Tag: 0x1500, Value: append([]byte(nil), raw...)}}}}
		original := body.SMFields.Clone()
		c := newCodec(t, pdu.V50)
		dst := []byte("prefix")
		out, e := c.Append(dst, pdu.PDU{Sequence: 1, Body: body})
		if !reflect.DeepEqual(body.SMFields, original) {
			t.Fatal("input mutation")
		}
		if e != nil {
			if string(out) != "prefix" {
				t.Fatal("invalid prefix committed")
			}
			return
		}
		if _, e = c.Decode(out[len(dst):]); e != nil {
			t.Fatal("encoder emitted undecodable frame", e)
		}
	})
}
