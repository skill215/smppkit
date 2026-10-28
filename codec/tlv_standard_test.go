// SPDX-AI-Disclosure: ai-generated
// SPDX-AI-Model: GPT-6.0 Astra
// SPDX-AI-Provider: OpenAI
// SPDX-AI-Scope: Test code generated and revised with Codex under maintainer direction.
// SPDX-AI-Date: 2026-10-28

package codec_test

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"testing"

	"github.com/skill215/smppkit/codec"
	"github.com/skill215/smppkit/pdu"
	"github.com/skill215/smppkit/tlv"
)

// These literal tag/length/value bytes are transcribed independently from the
// field definitions, not from standardTLVs or production TLV serialization.
// Command IDs and mandatory zero-value bodies below are separate wire oracles.
var standardVectors = []struct {
	name  string
	id    pdu.CommandID
	since pdu.Version
	hex   string
}{
	{"destination subunit", 0x103, 0x34, "0005000104"},
	{"destination network", 0x103, 0x34, "0006000108"},
	{"destination bearer", 0x103, 0x34, "0007000108"},
	{"destination telematics", 0x103, 0x34, "000800024100"},
	{"source subunit", 0x103, 0x34, "000d000104"},
	{"source network", 0x103, 0x34, "000e000108"},
	{"source bearer", 0x103, 0x34, "000f000108"},
	{"source telematics is ONE octet", 0x103, 0x34, "0010000141"},
	{"TTL", 0x103, 0x34, "0017000400000e10"},
	{"payload type", 0x103, 0x34, "0019000101"},
	{"diagnostic text", 0x80000103, 0x34, "001d00036f6b00"},
	{"receipt ID", 0x103, 0x34, "001e0003696400"},
	{"waiting facilities", 0x103, 0x34, "0030000183"},
	{"privacy", 0x103, 0x34, "0201000103"},
	{"source subaddress", 0x103, 0x34, "02020002a0ff"},
	{"destination subaddress", 0x103, 0x34, "020300028801"},
	{"user reference", 0x103, 0x34, "02040002ffff"},
	{"user response", 0x103, 0x34, "02050001ff"},
	{"source port", 0x103, 0x34, "020a00020b84"},
	{"destination port", 0x103, 0x34, "020b000223f0"},
	{"SAR trio", 0x103, 0x34, "020c00021234 020e000103 020f000102"},
	{"language extension stays opaque", 0x103, 0x34, "020d0001ff"},
	{"interface version", 0x80000009, 0x34, "0210000134"},
	{"callback associations", 0x103, 0x34, "0381000401010131 0302000108 030300020041"},
	{"mailbox count", 0x103, 0x34, "0304000163"},
	{"DPF result", 0x80000103, 0x34, "0420000101"},
	{"set DPF", 0x103, 0x34, "0421000101"},
	{"availability", 0x102, 0x34, "0422000102"},
	{"network error", 0x103, 0x34, "0423000303abcd"},
	{"binary payload", 0x103, 0x34, "0424000300ff00"},
	{"delivery failure", 0x80000103, 0x34, "0425000103"},
	{"more messages", 0x103, 0x34, "0426000101"},
	{"message state", 0x103, 0x34, "0427000108"},
	{"congestion", 0x80000015, 0x50, "0428000164"},
	{"vendor USSD", 4, 0x34, "05010001ff"},
	{"billing", 0x103, 0x50, "060b000280ff"},
	{"source network pair", 0x103, 0x50, "060d000731333130323600 060f0006313233343536"},
	{"destination network pair", 0x103, 0x50, "060e000731333130323600 06100006363534333231"},
	{"portability bundle", 0x103, 0x50, "0611000102 0612000a32313235353531323334 061300020001"},
	{"display", 0x103, 0x34, "1201000102"},
	{"signal", 0x103, 0x34, "12030002ffff"},
	{"validity", 0x103, 0x34, "1204000103"},
	{"empty alert", 0x103, 0x34, "130c0000"},
	{"reply type", 0x103, 0x34, "1380000108"},
	{"session info", 0x103, 0x34, "13830002ffff"},
}

func independentTLVFrame(t *testing.T, id pdu.CommandID, optional []byte) []byte {
	t.Helper()
	var body []byte
	switch id {
	case 4, 5:
		body = make([]byte, 17)
	case 0x103:
		body = make([]byte, 10)
	case 0x102:
		body = make([]byte, 6)
	case 0x21:
		body = unhex(t, "00 000000 01 01000000 00000000000000000000")
	case 0x80000003:
		body = make([]byte, 4)
	case 0x80000021:
		body = make([]byte, 2)
	case 0x80000002, 0x80000001, 0x80000009, 0x80000004, 0x80000005, 0x80000103:
		body = []byte{0}
	case 0x80000006, 0x80000007, 0x80000008, 0x80000015, 0x80000000:
	default:
		t.Fatal("missing independent body", id)
	}
	b := make([]byte, 16, 16+len(body)+len(optional))
	binary.BigEndian.PutUint32(b, uint32(16+len(body)+len(optional)))
	binary.BigEndian.PutUint32(b[4:], uint32(id))
	binary.BigEndian.PutUint32(b[12:], 7)
	if id == 0x80000000 {
		binary.BigEndian.PutUint32(b[8:], 3)
	}
	return append(append(b, body...), optional...)
}
func packetWithTLVs(id pdu.CommandID, fields tlv.Fields) pdu.PDU {
	p := pdu.PDU{Sequence: 7}
	switch id {
	case 4:
		p.Body = &pdu.SubmitSM{SMFields: pdu.SMFields{Optional: fields}}
	case 5:
		p.Body = &pdu.DeliverSM{SMFields: pdu.SMFields{Optional: fields}}
	case 0x21:
		p.Body = &pdu.SubmitMulti{Destinations: []pdu.MultiDestination{{Kind: pdu.SMEAddress}}, Optional: fields}
	case 0x103:
		p.Body = &pdu.DataSM{Optional: fields}
	case 0x102:
		p.Body = &pdu.AlertNotification{Optional: fields}
	case 0x80000001:
		p.Body = &pdu.BindReceiverResp{BindResponseFields: pdu.BindResponseFields{Optional: fields}}
	case 0x80000002:
		p.Body = &pdu.BindTransmitterResp{BindResponseFields: pdu.BindResponseFields{Optional: fields}}
	case 0x80000009:
		p.Body = &pdu.BindTransceiverResp{BindResponseFields: pdu.BindResponseFields{Optional: fields}}
	case 0x80000004:
		p.Body = &pdu.SubmitSMResp{MessageResponseFields: pdu.MessageResponseFields{Optional: fields}}
	case 0x80000005:
		p.Body = &pdu.DeliverSMResp{MessageResponseFields: pdu.MessageResponseFields{Optional: fields}}
	case 0x80000103:
		p.Body = &pdu.DataSMResp{MessageResponseFields: pdu.MessageResponseFields{Optional: fields}}
	case 0x80000021:
		p.Body = &pdu.SubmitMultiResp{Optional: fields}
	case 0x80000003:
		p.Body = &pdu.QuerySMResp{Optional: fields}
	case 0x80000006:
		p.Body = &pdu.UnbindResp{Optional: fields}
	case 0x80000007:
		p.Body = &pdu.ReplaceSMResp{Optional: fields}
	case 0x80000008:
		p.Body = &pdu.CancelSMResp{Optional: fields}
	case 0x80000015:
		p.Body = &pdu.EnquireLinkResp{Optional: fields}
	case 0x80000000:
		p.Body = &pdu.GenericNACK{Optional: fields}
		p.Status = pdu.InvalidCommandID
	}
	return p
}
func literalFields(t *testing.T, raw []byte) tlv.Fields {
	t.Helper()
	var fields tlv.Fields
	for len(raw) > 0 {
		if len(raw) < 4 {
			t.Fatal("bad test fixture")
		}
		n := int(binary.BigEndian.Uint16(raw[2:4]))
		if n > len(raw)-4 {
			t.Fatal("bad fixture length")
		}
		fields = append(fields, tlv.Field{Tag: binary.BigEndian.Uint16(raw), Value: bytes.Clone(raw[4 : 4+n])})
		raw = raw[4+n:]
	}
	return fields
}
func TestStandardTLVIndependentInventory(t *testing.T) {
	covered := map[uint16]bool{}
	for _, tc := range standardVectors {
		t.Run(tc.name, func(t *testing.T) {
			raw := unhex(t, tc.hex)
			fields := literalFields(t, raw)
			for _, f := range fields {
				covered[f.Tag] = true
			}
			for _, version := range []pdu.Version{pdu.V34, pdu.V50} {
				c := newCodec(t, version)
				expected := independentTLVFrame(t, tc.id, raw)
				got, err := c.Append(nil, packetWithTLVs(tc.id, fields))
				if version < tc.since {
					if err == nil {
						t.Fatal("5.0 TLV accepted in 3.4")
					}
					if _, err = c.Decode(expected); err == nil {
						t.Fatal("5.0 TLV decoded in 3.4")
					}
					continue
				}
				if err != nil || !bytes.Equal(got, expected) {
					t.Fatalf("version %x encoded=%x expected=%x err=%v", version, got, expected, err)
				}
				decoded, err := c.Decode(expected)
				if err != nil {
					t.Fatal(version, err)
				}
				encoded, err := c.Append(nil, decoded)
				if err != nil || !bytes.Equal(encoded, expected) {
					t.Fatal("raw value/order lost", err)
				}
			}
		})
	}
	if len(covered) != 53 {
		t.Fatalf("expected 53 independently covered standard SMS tags, got %d", len(covered))
	}
}
func TestStandardTLVInvalidValuesAndDependencies(t *testing.T) {
	cases := []struct {
		version pdu.Version
		id      pdu.CommandID
		hex     string
	}{
		{0x34, 0x103, "020a000100"}, {0x34, 0x103, "0017000100"},
		{0x34, 0x103, "001000020000"}, {0x34, 0x103, "000800020001"},
		{0x34, 0x103, "0005000105"}, {0x34, 0x103, "0006000109"},
		{0x34, 0x103, "001e00026964"}, {0x34, 0x103, "001e0003006100"},
		{0x34, 0x80000103, "001d0002ff00"},
		{0x34, 0x103, "0030000104"}, {0x34, 0x103, "0201000104"},
		{0x34, 0x103, "020200020001"}, {0x34, 0x103, "0304000164"},
		{0x34, 0x103, "042300020300"}, {0x34, 0x103, "04230003040000"},
		{0x50, 0x103, "04230003090000"}, {0x34, 0x103, "0427000100"},
		{0x34, 0x103, "0427000109"}, {0x50, 0x103, "042700010a"},
		{0x50, 0x80000015, "0428000165"}, {0x50, 0x103, "0428000101"},
		{0x34, 0x103, "130c000100"}, {0x50, 0x103, "130c000104"},
		{0x34, 0x103, "1204000404000001"}, {0x50, 0x103, "1204000104"},
		{0x50, 0x103, "1204000404070001"}, {0x50, 0x103, "120400020000"},
		{0x34, 4, "0501000104"}, {0x34, 0x103, "1380000109"},
		{0x34, 0x103, "020a00020001 020a00020001"},
		{0x34, 0x103, "020c00020001 020e000102"},
		{0x34, 0x103, "020c00020001 020e000102 020f000103"},
		{0x34, 0x103, "0302000100"}, {0x34, 0x103, "0303000100"},
		{0x34, 0x103, "0381000401010131 030200010c"},
		{0x34, 0x103, "0381000402010131"},
		{0x34, 0x103, "0381000401010131 030300020b41"}, // reserved alpha-tag DCS
		{0x34, 0x103, "0381000401010131 0381000401010132 030300020041"},
		{0x50, 0x103, "060b000100"}, {0x50, 0x103, "060d000731333130323600"},
		{0x50, 0x103, "060f0006313233343561"}, {0x50, 0x103, "0611000102"},
		{0x50, 0x103, "0611000103"}, {0x50, 0x103, "0600000100"},
		{0x34, 4, "0017000400000001"}, // TTL is data_sm-only in 3.4
		{0x34, 0x21, "02030002a001"}, {0x50, 0x21, "02030002a001"},
		{0x34, 0x21, "0205000101"}, {0x34, 5, "030300020041 0381000401010131"},
		{0x50, 0x80000005, "0420000101"}, // DPF result is not a delivery response TLV
	}
	for i, tc := range cases {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			c := newCodec(t, tc.version)
			raw := unhex(t, tc.hex)
			if _, err := c.Decode(independentTLVFrame(t, tc.id, raw)); err == nil {
				t.Fatal("invalid literal decoded")
			}
			if _, err := c.Append(nil, packetWithTLVs(tc.id, literalFields(t, raw))); err == nil {
				t.Fatal("invalid TLV encoded")
			}
		})
	}
}
func TestStandardTLVVersionDifferencesAndRepeatedCallbacks(t *testing.T) {
	for _, wire := range []string{
		"130c000103", "1204000404060001", "0423000308ffff", "0427000100", "0427000109",
	} {
		raw := independentTLVFrame(t, 0x103, unhex(t, wire))
		if _, err := newCodec(t, 0x50).Decode(raw); err != nil {
			t.Fatal(err)
		}
		if _, err := newCodec(t, 0x34).Decode(raw); err == nil {
			t.Fatal("5.0 value accepted in 3.4")
		}
	}
	// Associations survive arbitrary interleaving; each repeated family's order
	// remains significant. Unknown duplicates between them must also survive.
	values := unhex(t, "0381000401010131 1501000101 030300020041 0381000401010132 030300020042 1501000102 0302000100 0302000108")
	for _, v := range []pdu.Version{0x34, 0x50} {
		raw := independentTLVFrame(t, 0x103, values)
		c := newCodec(t, v)
		got, err := c.Decode(raw)
		if err != nil {
			t.Fatal(err)
		}
		fields := got.Body.(*pdu.DataSM).Optional
		if len(fields.All(tlv.CallbackNum)) != 2 || len(fields.All(0x1501)) != 2 {
			t.Fatal("duplicates lost")
		}
		encoded, err := c.Append(nil, got)
		if err != nil || !bytes.Equal(encoded, raw) {
			t.Fatal("wire order changed", err)
		}
	}
}
func TestCongestionOnEveryImplementedResponse(t *testing.T) {
	for _, id := range []pdu.CommandID{0x80000000, 0x80000001, 0x80000002, 0x80000003, 0x80000004, 0x80000005, 0x80000006, 0x80000007, 0x80000008, 0x80000009, 0x80000015, 0x80000021, 0x80000103} {
		raw := independentTLVFrame(t, id, unhex(t, "0428000150"))
		c := newCodec(t, 0x50)
		got, err := c.Decode(raw)
		if err != nil {
			t.Fatal(id, err)
		}
		encoded, err := c.Append(nil, got)
		if err != nil || !bytes.Equal(encoded, raw) {
			t.Fatal(id, err)
		}
		encoded, err = c.Append(nil, packetWithTLVs(id, tlv.Fields{{Tag: tlv.CongestionState, Value: []byte{80}}}))
		if err != nil || !bytes.Equal(encoded, raw) {
			t.Fatal(id, err)
		}
		if _, err := newCodec(t, 0x34).Decode(raw); err == nil {
			t.Fatal("congestion accepted in 3.4", id)
		}
	}
}

func TestStandardTLVVariableWidthBoundaries(t *testing.T) {
	c, err := codec.New(codec.Config{Version: pdu.V50, MaxFrame: 128 * 1024})
	if err != nil {
		t.Fatal(err)
	}
	// Independent protocol widths, with valid content generated at each bound.
	// Decode gets manually packed tag/length bytes, never Append's output.
	for _, tc := range []struct {
		tag      uint16
		id       pdu.CommandID
		min, max int
		fill     byte
		cString  bool
	}{
		{0x001d, 0x80000103, 1, 256, 'A', true},
		{0x001e, 0x103, 1, 65, 'i', true},
		{0x0202, 0x103, 2, 23, 0xa0, false},
		{0x0381, 0x103, 4, 19, '1', false},
		{0x0424, 0x103, 0, 65535, 0xff, false},
		{0x060b, 0x103, 1, 1024, 0x80, false},
	} {
		for _, n := range []int{tc.min - 1, tc.min, tc.max, tc.max + 1} {
			if n < 0 {
				continue
			}
			value := bytes.Repeat([]byte{tc.fill}, n)
			if tc.cString && n > 0 {
				value[n-1] = 0
			}
			if tc.tag == 0x0381 && n >= 3 {
				copy(value, []byte{1, 1, 1})
			}
			field := tlv.Field{Tag: tc.tag, Value: value}
			_, err := c.Append(nil, packetWithTLVs(tc.id, tlv.Fields{field}))
			valid := n >= tc.min && n <= tc.max
			if (err == nil) != valid {
				t.Fatalf("tag=%x n=%d encode=%v", tc.tag, n, err)
			}
			raw := make([]byte, 4, n+4)
			binary.BigEndian.PutUint16(raw, tc.tag)
			binary.BigEndian.PutUint16(raw[2:], uint16(n))
			raw = append(raw, value...)
			_, err = c.Decode(independentTLVFrame(t, tc.id, raw))
			if (err == nil) != valid {
				t.Fatalf("tag=%x n=%d decode=%v", tc.tag, n, err)
			}
		}
	}
	// A legal 65,535-byte payload still needs room for SMPP/TLV headers. The
	// default 64 KiB frame cap must reject it; raising that cap is explicit.
	packet := packetWithTLVs(0x103, tlv.Fields{{Tag: 0x0424, Value: make([]byte, 65535)}})
	if _, err := newCodec(t, pdu.V50).Append(nil, packet); err == nil {
		t.Fatal("default frame cap bypassed")
	}
}

func FuzzStandardTLVSection(f *testing.F) {
	for _, tc := range standardVectors {
		f.Add(uint32(tc.id), unhex(f, tc.hex))
	}
	f.Add(uint32(0x103), []byte{0x02, 0x0c, 0, 2, 0, 1}) // incomplete SAR
	f.Fuzz(func(t *testing.T, selector uint32, raw []byte) {
		if len(raw) > 4096 {
			t.Skip()
		}
		id := pdu.CommandID(selector)
		switch id {
		case 4, 5, 0x21, 0x103, 0x102, 0x80000103, 0x80000009, 0x80000015:
		default:
			id = 0x103
		}
		frame := independentTLVFrame(t, id, raw)
		for _, v := range []pdu.Version{pdu.V33, pdu.V34, pdu.V50} {
			c := newCodec(t, v)
			packet, err := c.Decode(frame)
			if err != nil {
				continue
			}
			encoded, err := c.Append(nil, packet)
			if err != nil || !bytes.Equal(encoded, frame) {
				t.Fatal("accepted TLVs not preserved", err)
			}
		}
	})
}
