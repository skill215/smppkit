package codec_test

import (
	"bytes"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/skill215/smppkit/codec"
	"github.com/skill215/smppkit/pdu"
	"github.com/skill215/smppkit/tlv"
)

// Literal data_sm/data_sm_resp frames; expected bytes never come from Codec.
const dataReceiptHex = "00000026 00000103 00000000 00000007 00000000000000 040000 001e0003696400 0427000102"
const dataTTLHex = "00000022 00000103 00000000 00000007 00000000000000 020000 001700040000003c"
const dataDPFResponseHex = "00000017 80000103 00000000 00000007 6d00 0420000101"

func TestDataDirectionIndependentFrames(t *testing.T) {
	for _, v := range []pdu.Version{pdu.V34, pdu.V50} {
		c := newCodec(t, v)
		for _, tc := range []struct {
			wire                 string
			body                 pdu.Body
			submission, delivery bool
		}{
			{dataReceiptHex, &pdu.DataSM{ESMClass: 4, Optional: tlv.Fields{{Tag: tlv.ReceiptedMessageID, Value: []byte{'i', 'd', 0}}, {Tag: tlv.MessageState, Value: []byte{2}}}}, false, true},
			{dataTTLHex, &pdu.DataSM{ESMClass: 2, Optional: tlv.Fields{{Tag: tlv.QOSTimeToLive, Value: []byte{0, 0, 0, 60}}}}, true, v == pdu.V34},
			{dataDPFResponseHex, &pdu.DataSMResp{MessageResponseFields: pdu.MessageResponseFields{MessageID: "m", Optional: tlv.Fields{{Tag: tlv.DPFResult, Value: []byte{1}}}}}, true, false},
		} {
			raw := unhex(t, tc.wire)
			encoded, err := c.Append(nil, pdu.PDU{Sequence: 7, Body: tc.body})
			if err != nil || !bytes.Equal(encoded, raw) {
				t.Fatal(v, err, fmt.Sprintf("%x", encoded))
			}
			packet, err := c.Decode(raw)
			if err != nil {
				t.Fatal(err)
			}
			for _, direction := range []codec.DataDirection{codec.DataSubmission, codec.DataDelivery} {
				want := tc.submission
				if direction == codec.DataDelivery {
					want = tc.delivery
				}
				for _, body := range []pdu.Body{tc.body, packet.Body} {
					err := c.ValidateDataDirection(body, direction)
					if (err == nil) != want {
						t.Fatal(v, direction, tc.wire, err)
					}
					if err != nil && !errors.Is(err, codec.ErrValidation) {
						t.Fatal(err)
					}
				}
			}
			got, err := c.Append(nil, packet)
			if err != nil || !bytes.Equal(got, raw) {
				t.Fatal("context check mutated packet", err)
			}
		}
	}
}

func TestDataDirectionESMOctets(t *testing.T) {
	for _, v := range []pdu.Version{pdu.V34, pdu.V50} {
		c := newCodec(t, v)
		for n := range 256 {
			for _, d := range []codec.DataDirection{codec.DataSubmission, codec.DataDelivery} {
				typ := n & 0x3c
				valid := typ == 0 || typ == 4 || typ == 8 || typ == 16 || typ == 24 || typ == 32
				if v == pdu.V34 && d == codec.DataSubmission {
					valid = typ == 0 || typ == 8 || typ == 16
				}
				body := &pdu.DataSM{ESMClass: byte(n)}
				err := c.ValidateDataDirection(body, d)
				if (err == nil) != valid {
					t.Fatalf("v=%x direction=%d esm=%02x: %v", v, d, n, err)
				}
				wireBody := make([]byte, 10)
				wireBody[7] = byte(n)
				packet, decodeErr := c.Decode(semanticFrame(0x103, 0, wireBody))
				if valid && decodeErr != nil {
					t.Fatal(decodeErr)
				}
				if decodeErr == nil && (c.ValidateDataDirection(packet.Body, d) == nil) != valid {
					t.Fatal("decoded ESM differs")
				}
			}
		}
	}
}

func TestDataDirectionTLVTables(t *testing.T) {
	// The 5.0 submission-only and delivery-only cases come from its separate
	// tables. 3.4 shares a table and retains TTL/network/callback metadata in
	// both directions, with explicit receipt/DPF/more-message restrictions.
	cases := []struct {
		wire                       string
		response                   bool
		sub34, del34, sub50, del50 bool
	}{
		{"001e00026900", false, false, true, false, true},
		{"0427000102", false, false, true, false, true},
		{"04230003030001", false, false, true, false, true},
		{"0421000101", false, true, false, true, false},
		{"0426000100", false, true, false, true, false},
		{"0017000400000001", false, true, true, true, false},
		{"0006000101", false, true, true, true, false},
		{"0007000101", false, true, true, true, false},
		{"000800020000", false, true, true, true, false},
		{"000e000101", false, true, true, true, false},
		{"000f000101", false, true, true, true, false},
		{"0010000100", false, true, true, true, false},
		{"0030000100", false, true, true, true, false},
		{"0304000101", false, true, true, true, false},
		{"1201000100", false, true, true, true, false},
		{"120300020000", false, true, true, true, false},
		{"1204000100", false, true, true, true, false},
		{"130c0000", false, true, true, true, false},
		{"0381000401010131 0302000100 030300020041", false, true, true, true, true},
		{"020a00021234 020b00025678", false, true, true, true, true},
		{"0005000101 000d000101", false, true, true, true, true},
		{"0420000101", false, false, false, false, true}, // introduced on requests in 5.0
		{"060b000180", false, false, false, true, false},
		{"001d00036f6b00", true, true, true, true, true},
		{"04230003030001", true, true, true, true, true},
		{"0425000101", true, true, true, true, true},
		{"0420000101", true, true, false, true, false},
		{"0428000164", true, false, false, true, true},
		{"15010001ff 1501000100", false, true, true, true, true},
		{"15010001ff 1501000100", true, true, true, true, true},
	}
	for _, v := range []pdu.Version{pdu.V34, pdu.V50} {
		c := newCodec(t, v)
		for _, tc := range cases {
			id := pdu.DataSMID
			if tc.response {
				id = pdu.DataSMRespID
			}
			raw := independentTLVFrame(t, id, unhex(t, tc.wire))
			packet, err := c.Decode(raw)
			sub, del := tc.sub34, tc.del34
			if v == pdu.V50 {
				sub, del = tc.sub50, tc.del50
			}
			if !sub && !del && v == pdu.V34 {
				if err == nil {
					t.Fatal("version-only TLV accepted")
				}
				continue
			}
			if err != nil {
				t.Fatal(tc.wire, v, err)
			}
			for _, d := range []codec.DataDirection{codec.DataSubmission, codec.DataDelivery} {
				want := sub
				if d == codec.DataDelivery {
					want = del
				}
				if err := c.ValidateDataDirection(packet.Body, d); (err == nil) != want {
					t.Fatal(v, d, tc.wire, err)
				}
			}
			got, err := c.Append(nil, packet)
			if err != nil || !bytes.Equal(got, raw) {
				t.Fatal("raw/order changed", err)
			}
		}
	}
}

func TestDataDirectionAPIContract(t *testing.T) {
	c := newCodec(t, pdu.V50)
	for _, body := range []pdu.Body{nil, (*pdu.DataSM)(nil), (*pdu.DataSMResp)(nil), &pdu.SubmitSM{}, &pdu.Omitted{ID: pdu.SubmitSMRespID}} {
		if err := c.ValidateDataDirection(body, codec.DataSubmission); !errors.Is(err, codec.ErrValidation) {
			t.Fatal(body, err)
		}
	}
	for _, d := range []codec.DataDirection{0, 3, 255} {
		if c.ValidateDataDirection(&pdu.DataSM{}, d) == nil {
			t.Fatal("invalid direction")
		}
	}
	if err := newCodec(t, pdu.V33).ValidateDataDirection(&pdu.DataSM{}, codec.DataSubmission); !errors.Is(err, codec.ErrUnsupported) {
		t.Fatal(err)
	}
	for _, d := range []codec.DataDirection{codec.DataSubmission, codec.DataDelivery} {
		body := &pdu.Omitted{ID: pdu.DataSMRespID}
		if err := c.ValidateDataDirection(body, d); err != nil {
			t.Fatal(err)
		}
		if _, err := c.Append(nil, pdu.PDU{Sequence: 1, Body: body}); err == nil {
			t.Fatal("direction check bypassed success body rule")
		}
	}
	// Unknown/custom values stay opaque here. Normal validation still enforces
	// widths and dependencies, and customer callbacks run exactly there.
	var calls int
	c, err := codec.New(codec.Config{Version: pdu.V50, TLVs: []codec.TLVDefinition{{Tag: 0x1501, MinLength: 1, MaxLength: 1, Versions: []pdu.Version{pdu.V50}, Commands: []pdu.CommandID{pdu.DataSMID}, Validate: func([]byte) error { calls++; return nil }}}})
	if err != nil {
		t.Fatal(err)
	}
	b := &pdu.DataSM{Optional: tlv.Fields{{Tag: 0x1501, Value: []byte{255}}}}
	if _, err = c.Append(nil, pdu.PDU{Sequence: 7, Body: b}); err != nil {
		t.Fatal(err)
	}
	for _, d := range []codec.DataDirection{codec.DataSubmission, codec.DataDelivery} {
		if err = c.ValidateDataDirection(b, d); err != nil {
			t.Fatal(err)
		}
	}
	if calls != 1 {
		t.Fatal("context check invoked callback", calls)
	}
	b.Optional = tlv.Fields{{Tag: tlv.SourcePort, Value: []byte{1}}}
	if _, err = c.Append(nil, pdu.PDU{Sequence: 7, Body: b}); err == nil {
		t.Fatal("width validation bypass")
	}
	// A valid union can be invalid in BOTH concrete directions.
	b.Optional = tlv.Fields{{Tag: tlv.SetDPF, Value: []byte{1}}, {Tag: tlv.MessageState, Value: []byte{2}}}
	if _, err = c.Append(nil, pdu.PDU{Sequence: 7, Body: b}); err != nil {
		t.Fatal(err)
	}
	for _, d := range []codec.DataDirection{codec.DataSubmission, codec.DataDelivery} {
		if c.ValidateDataDirection(b, d) == nil {
			t.Fatal("mixed direction parameters accepted")
		}
	}
	b.Optional = make(tlv.Fields, c.MaxFrame()/4+1)
	if c.ValidateDataDirection(b, codec.DataSubmission) == nil {
		t.Fatal("unbounded contextual scan")
	}
}

func TestDataDirectionConcurrentCodec(t *testing.T) {
	c := newCodec(t, pdu.V50)
	packet, err := c.Decode(unhex(t, dataReceiptHex))
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 32 {
		wg.Go(func() {
			for range 30 {
				if c.ValidateDataDirection(packet.Body, codec.DataDelivery) != nil || c.ValidateDataDirection(packet.Body, codec.DataSubmission) == nil {
					t.Error("shared direction state leaked")
				}
			}
		})
	}
	wg.Wait()
}

func FuzzDataDirection(f *testing.F) {
	for _, raw := range []string{dataReceiptHex, dataTTLHex, dataDPFResponseHex} {
		f.Add(byte(0), unhex(f, raw))
	}
	f.Fuzz(func(t *testing.T, profile byte, raw []byte) {
		if len(raw) > 65536 {
			t.Skip()
		}
		v := []pdu.Version{pdu.V34, pdu.V50}[int(profile)%2]
		c := newCodec(t, v)
		p, err := c.Decode(raw)
		if err != nil {
			return
		}
		id := p.Body.CommandID()
		if id != pdu.DataSMID && id != pdu.DataSMRespID {
			return
		}
		for _, d := range []codec.DataDirection{codec.DataSubmission, codec.DataDelivery} {
			err := c.ValidateDataDirection(p.Body, d)
			if (c.ValidateDataDirection(p.Body, d) == nil) != (err == nil) {
				t.Fatal("nondeterministic validation")
			}
			got, e := c.Append(nil, p)
			if e != nil || !bytes.Equal(got, raw) {
				t.Fatal("context mutated accepted frame", e)
			}
		}
	})
}
