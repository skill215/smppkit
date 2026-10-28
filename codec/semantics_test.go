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
	"reflect"
	"strings"
	"testing"

	"github.com/skill215/smppkit/codec"
	"github.com/skill215/smppkit/pdu"
	"github.com/skill215/smppkit/tlv"
)

// Header arithmetic and all body bytes in this file are independent of Codec.
// Do not create the decoder's negative fixtures by encoding the invalid body:
// the encoder is required to reject that same body before exposing any bytes.
func semanticFrame(id pdu.CommandID, status pdu.Status, body []byte) []byte {
	b := make([]byte, 16, 16+len(body))
	binary.BigEndian.PutUint32(b, uint32(16+len(body)))
	binary.BigEndian.PutUint32(b[4:], uint32(id))
	binary.BigEndian.PutUint32(b[8:], uint32(status))
	binary.BigEndian.PutUint32(b[12:], 7)
	return append(b, body...)
}

func checkSemantic(t *testing.T, v pdu.Version, p pdu.PDU, wire []byte, valid bool) {
	t.Helper()
	c := newCodec(t, v)
	p.Sequence = 7
	prefix := []byte{0xca, 0xfe}
	encoded, err := c.Append(prefix, p)
	if (err == nil) != valid {
		t.Fatalf("v=%x command=%x encode err=%v, valid=%v", v, p.Body.CommandID(), err, valid)
	}
	if !valid && !bytes.Equal(encoded, prefix) {
		t.Fatal("invalid PDU changed append destination")
	}
	if valid && !bytes.Equal(encoded[2:], wire) {
		t.Fatalf("encoded=%x expected=%x", encoded[2:], wire)
	}
	decoded, err := c.Decode(wire)
	if (err == nil) != valid {
		t.Fatalf("v=%x command=%x decode err=%v, valid=%v wire=%x", v, p.Body.CommandID(), err, valid, wire)
	}
	if valid {
		reencoded, err := c.Append(nil, decoded)
		if err != nil || !bytes.Equal(reencoded, wire) {
			t.Fatal("accepted fields were normalized or lost", err)
		}
	}
}

const scheduledSubmitHex = "0000004c 00000004 00000000 00000007 434d5400 050073726300 010131323300 030003 3234303232393233353935393934382b00 3030303030313030303030303030305200 1e01080002 0041"
const finalQueryHex = "00000027 80000003 00000000 00000007 6d3a3700 3234313233313233353935393030302d00 0200"
const localQueryHex = "00000023 80000003 00000000 00000007 6d3a3700 32343132333132333539353900 0200"

func TestIndependentSemanticVectors(t *testing.T) {
	fields := pdu.SMFields{ServiceType: "CMT", Source: pdu.Address{TON: 5, Value: "src"}, Destination: pdu.Address{TON: 1, NPI: 1, Value: "123"},
		ESMClass: 3, PriorityFlag: 3, ScheduleDeliveryTime: "240229235959948+", ValidityPeriod: "000001000000000R",
		RegisteredDelivery: 0x1e, ReplaceIfPresent: 1, DataCoding: 8, ShortMessage: []byte{0, 'A'}}
	for _, v := range []pdu.Version{pdu.V33, pdu.V34, pdu.V50} {
		checkSemantic(t, v, pdu.PDU{Body: &pdu.SubmitSM{SMFields: fields}}, unhex(t, scheduledSubmitHex), v != pdu.V33)
		// In 5.0 deliver_sm may carry forwarding fields. 3.4 still requires
		// empty schedule/validity, replacement and default-message fields.
		raw := unhex(t, scheduledSubmitHex)
		raw[7] = 5
		checkSemantic(t, v, pdu.PDU{Body: &pdu.DeliverSM{SMFields: fields}}, raw, v == pdu.V50)
		for _, tc := range []struct{ date, hex string }{{"241231235959000-", finalQueryHex}, {"241231235959", localQueryHex}} {
			p := pdu.PDU{Sequence: 7, Body: &pdu.QuerySMResp{MessageID: "m:7", FinalDate: tc.date, MessageState: 2}}
			valid := len(tc.date) == 16 || v != pdu.V50
			checkSemantic(t, v, p, unhex(t, tc.hex), valid)
			if valid {
				got, err := newCodec(t, v).Decode(unhex(t, tc.hex))
				if err != nil || !reflect.DeepEqual(got, p) {
					t.Fatal(got, err)
				}
			}
		}
	}
}

func TestTimeBoundaries(t *testing.T) {
	cases := []struct {
		value    string
		valid    bool
		relative bool
	}{
		{"", true, false}, {"240229235959948+", true, false}, {"000229000000000-", true, false},
		{"991231235959000-", true, false}, {"230229000000000+", false, false}, {"240230000000000+", false, false},
		{"240431000000000+", false, false}, {"240001000000000+", false, false}, {"241301000000000+", false, false},
		{"240100000000000+", false, false}, {"240132000000000+", false, false}, {"240101240000000+", false, false},
		{"240101006000000+", false, false}, {"240101000060000+", false, false}, {"240101000000049+", false, false},
		{"240101000000000Z", false, false}, {"24010100000000+", false, false}, {"2401010000000000+", false, false},
		{"240101000000a00+", false, false}, {"2401010000000x0+", false, false}, {"240101000000", false, false},
		{"000000010000000R", true, true}, {"000000000000000R", true, true}, {"991231235959999R", true, true},
		{"001300000000000R", false, true}, {"000032000000000R", false, true}, {"000000240000000R", false, true},
	}
	for _, v := range []pdu.Version{pdu.V33, pdu.V34, pdu.V50} {
		for _, tc := range cases {
			t.Run(fmt.Sprintf("%x/%q", v, tc.value), func(t *testing.T) {
				valid := tc.valid && (!tc.relative || v != pdu.V33)
				for _, expiry := range []bool{false, true} {
					b := &pdu.SubmitSM{}
					body := make([]byte, 10) // empty service/addresses, then ESM/PID/priority
					if expiry {
						b.ValidityPeriod = tc.value
						body = append(body, 0)
					} else {
						b.ScheduleDeliveryTime = tc.value
					}
					body = append(body, []byte(tc.value)...)
					body = append(body, 0)
					if !expiry {
						body = append(body, 0)
					}
					body = append(body, 0, 0, 0, 0, 0)
					checkSemantic(t, v, pdu.PDU{Body: b}, semanticFrame(4, 0, body), valid)
					// replace_sm has the same time grammar but a different field order.
					r := &pdu.ReplaceSM{ScheduleDeliveryTime: b.ScheduleDeliveryTime, ValidityPeriod: b.ValidityPeriod}
					replacement := append(make([]byte, 4), body[10:len(body)-5]...)
					replacement = append(replacement, 0, 0, 0)
					checkSemantic(t, v, pdu.PDU{Body: r}, semanticFrame(7, 0, replacement), valid)
				}
			})
		}
	}
}

func TestMessageOctetSemantics(t *testing.T) {
	// Explicit allowed sets from the version tables, swept over all 256 values.
	// Body offsets below are independent of the production suffix helpers.
	for _, v := range []pdu.Version{pdu.V33, pdu.V34, pdu.V50} {
		for _, id := range []pdu.CommandID{4, 5, 0x21, 0x103} {
			if v == pdu.V33 && id == 0x103 {
				continue
			}
			for field := range 6 {
				if id == 0x103 && field >= 3 {
					continue
				}
				t.Run(fmt.Sprintf("%x/%x/%d", v, id, field), func(t *testing.T) {
					for n := range 256 {
						value := byte(n)
						var fields pdu.SMFields
						valid := false
						switch field {
						case 0:
							fields.ESMClass = value
							kind := n & 0x3c
							valid = kind == 0 || kind == 4 || kind == 8 || kind == 16 || kind == 24 || kind == 32
							if v == pdu.V34 && (id == 4 || id == 0x21) {
								valid = kind == 0 || kind == 8 || kind == 16
							}
							if v == pdu.V33 {
								valid = id == 5 || n == 0
							}
						case 1:
							fields.RegisteredDelivery = value
							valid = n < 32 && (v == pdu.V50 || n%4 != 3)
							if v == pdu.V33 {
								valid = n == 0 || n == 1
							}
						case 2:
							fields.DataCoding = value
							valid = v == pdu.V33 || n <= 10 || n == 13 || n == 14 || n >= 192 && n <= 223 || n >= 240
						case 3:
							fields.PriorityFlag = value
							valid = n < 4
							if v == pdu.V33 {
								valid = n < 2
							}
						case 4:
							fields.ReplaceIfPresent = value
							valid = n < 2
							if v == pdu.V34 && (id == 5 || id == 0x21) {
								valid = n == 0
							}
						case 5:
							fields.DefaultMessageID = value
							valid = true
							if v == pdu.V33 {
								valid = n <= 100
							}
							if v == pdu.V34 {
								valid = n < 255 && (id != 5 || n == 0)
							}
						}
						body := make([]byte, 17)
						offsets := []int{7, 12, 14, 9, 13, 15}
						var typed pdu.Body = &pdu.SubmitSM{SMFields: fields}
						switch id {
						case 5:
							typed = &pdu.DeliverSM{SMFields: fields}
						case 0x21:
							body = make([]byte, 19)
							body[4] = 1
							body[5] = 1
							offsets = []int{9, 14, 16, 11, 15, 17}
							typed = &pdu.SubmitMulti{Destinations: []pdu.MultiDestination{{Kind: pdu.SMEAddress}}, ESMClass: fields.ESMClass, RegisteredDelivery: fields.RegisteredDelivery, DataCoding: fields.DataCoding, PriorityFlag: fields.PriorityFlag, ReplaceIfPresent: fields.ReplaceIfPresent, DefaultMessageID: fields.DefaultMessageID}
						case 0x103:
							body = make([]byte, 10)
							offsets = []int{7, 8, 9}
							typed = &pdu.DataSM{ESMClass: fields.ESMClass, RegisteredDelivery: fields.RegisteredDelivery, DataCoding: fields.DataCoding}
						}
						body[offsets[field]] = value
						checkSemantic(t, v, pdu.PDU{Body: typed}, semanticFrame(id, 0, body), valid)
					}
				})
			}
		}
	}
}

func TestQueryAndDeliveryResponseSemantics(t *testing.T) {
	for _, v := range []pdu.Version{pdu.V33, pdu.V34, pdu.V50} {
		for n := range 256 {
			for _, status := range []pdu.Status{pdu.OK, pdu.SystemError} {
				b := &pdu.QuerySMResp{MessageState: byte(n), ErrorCode: 255}
				valid := status != pdu.OK || v == pdu.V33 || (v == pdu.V34 && n >= 1 && n <= 8) || (v == pdu.V50 && n <= 9)
				checkSemantic(t, v, pdu.PDU{Status: status, Body: b}, semanticFrame(0x80000003, status, []byte{0, 0, byte(n), 255}), valid)
			}
		}
		for _, date := range []string{"000000010000000R", "241231235959000+", "241231235959", "241332235959000+"} {
			for _, state := range []byte{0, 1, 2} {
				b := &pdu.QuerySMResp{FinalDate: date, MessageState: state}
				raw := append([]byte{0}, []byte(date)...)
				raw = append(raw, 0, state, 0)
				valid := date == "241231235959000+" || date == "241231235959" && v != pdu.V50
				if v != pdu.V33 && state <= 1 {
					valid = false
				}
				checkSemantic(t, v, pdu.PDU{Body: b}, semanticFrame(0x80000003, 0, raw), valid)
			}
		}
		checkSemantic(t, v, pdu.PDU{Body: &pdu.DeliverSMResp{MessageResponseFields: pdu.MessageResponseFields{MessageID: "id"}}}, semanticFrame(0x80000005, 0, []byte{'i', 'd', 0}), v == pdu.V33)
	}
}

func TestCStringsAreASCIIAndPayloadsRemainBinary(t *testing.T) {
	for _, v := range []pdu.Version{pdu.V33, pdu.V34, pdu.V50} {
		for _, value := range []byte{1, 127, 128, 255} {
			b := &pdu.BindTransmitter{BindFields: pdu.BindFields{Password: string([]byte{value}), InterfaceVersion: v}}
			raw := semanticFrame(2, 0, []byte{0, value, 0, 0, byte(v), 0, 0, 0})
			checkSemantic(t, v, pdu.PDU{Body: b}, raw, value < 128)
		}
		// Do not apply ASCII restrictions to binary payloads or protocol IDs.
		b := &pdu.SubmitSM{SMFields: pdu.SMFields{ProtocolID: 255, ShortMessage: []byte{0, 128, 255}}}
		raw := make([]byte, 17)
		raw[8] = 255
		raw[16] = 3
		raw = append(raw, 0, 128, 255)
		checkSemantic(t, v, pdu.PDU{Body: b}, semanticFrame(4, 0, raw), true)
	}
	secret := "priv\xff"
	_, err := newCodec(t, pdu.V50).Append(nil, pdu.PDU{Sequence: 7, Body: &pdu.Outbind{Password: secret}})
	if err == nil || strings.Contains(err.Error(), "priv") {
		t.Fatal("credential echoed or accepted", err)
	}
	for _, raw := range []string{"001e0002ff00", "001e00028000"} {
		c := newCodec(t, pdu.V50)
		if _, err := c.Decode(independentTLVFrame(t, 0x103, unhex(t, raw))); err == nil {
			t.Fatal("non-ASCII receipt ID")
		}
	}
	checkSemantic(t, pdu.V50, pdu.PDU{Body: &pdu.DataSM{Optional: tlv.Fields{{Tag: 0x1500, Value: []byte{255}}}}}, independentTLVFrame(t, 0x103, unhex(t, "15000001ff")), true)
}

func TestAddressTypesOnEveryAddressLayout(t *testing.T) {
	// Each case selects an actual address slot (including bind metadata and
	// per-SME response addresses), rather than testing only a private predicate.
	cases := []struct {
		id       pdu.CommandID
		body     []byte
		offset   int
		makeBody func(byte, byte, pdu.Version) pdu.Body
	}{
		{2, []byte{0, 0, 0, 0, 0, 0, 0}, 4, func(ton, npi byte, v pdu.Version) pdu.Body {
			return &pdu.BindTransmitter{BindFields: pdu.BindFields{InterfaceVersion: v, AddrTON: ton, AddrNPI: npi}}
		}},
		{4, make([]byte, 17), 1, func(ton, npi byte, _ pdu.Version) pdu.Body {
			return &pdu.SubmitSM{SMFields: pdu.SMFields{Source: pdu.Address{TON: ton, NPI: npi}}}
		}},
		{5, make([]byte, 17), 4, func(ton, npi byte, _ pdu.Version) pdu.Body {
			return &pdu.DeliverSM{SMFields: pdu.SMFields{Destination: pdu.Address{TON: ton, NPI: npi}}}
		}},
		{3, make([]byte, 4), 1, func(ton, npi byte, _ pdu.Version) pdu.Body {
			return &pdu.QuerySM{Source: pdu.Address{TON: ton, NPI: npi}}
		}},
		{7, make([]byte, 9), 1, func(ton, npi byte, _ pdu.Version) pdu.Body {
			return &pdu.ReplaceSM{Source: pdu.Address{TON: ton, NPI: npi}}
		}},
		{8, make([]byte, 8), 5, func(ton, npi byte, _ pdu.Version) pdu.Body {
			return &pdu.CancelSM{Destination: pdu.Address{TON: ton, NPI: npi}}
		}},
		{0x21, []byte{0, 0, 0, 0, 1, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}, 6, func(ton, npi byte, _ pdu.Version) pdu.Body {
			return &pdu.SubmitMulti{Destinations: []pdu.MultiDestination{{Kind: pdu.SMEAddress, Address: pdu.Address{TON: ton, NPI: npi}}}}
		}},
		{0x80000021, []byte{0, 1, 0, 0, 0, 0, 0, 0, 0x58}, 2, func(ton, npi byte, _ pdu.Version) pdu.Body {
			return &pdu.SubmitMultiResp{Unsuccessful: []pdu.UnsuccessfulSME{{Address: pdu.Address{TON: ton, NPI: npi}, Status: pdu.Throttled}}}
		}},
		{0x103, make([]byte, 10), 4, func(ton, npi byte, _ pdu.Version) pdu.Body {
			return &pdu.DataSM{Destination: pdu.Address{TON: ton, NPI: npi}}
		}},
		{0x102, make([]byte, 6), 3, func(ton, npi byte, _ pdu.Version) pdu.Body {
			return &pdu.AlertNotification{ESME: pdu.Address{TON: ton, NPI: npi}}
		}},
	}
	for _, v := range []pdu.Version{pdu.V33, pdu.V34, pdu.V50} {
		for _, tc := range cases {
			if v == pdu.V33 && (tc.id == 0x102 || tc.id == 0x103) {
				continue
			}
			for _, ton := range []byte{0, 1, 5, 6, 7, 128, 255} {
				for _, npi := range []byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 14, 15, 16, 18, 19, 255} {
					valid := ton <= 6 && (npi == 0 || npi == 1 || npi == 3 || npi == 4 || npi == 6 || npi == 8 || npi == 9 || npi == 10 || npi == 14 || npi == 18)
					if v == pdu.V33 {
						valid = ton <= 6 && npi <= 15
					}
					body := bytes.Clone(tc.body)
					body[tc.offset] = ton
					body[tc.offset+1] = npi
					if tc.id == 2 {
						body[3] = byte(v)
					}
					raw := semanticFrame(tc.id, 0, body)
					checkSemantic(t, v, pdu.PDU{Body: tc.makeBody(ton, npi, v)}, raw, valid)
					if tc.id == 2 {
						_, _, err := codec.ReadBind(bytes.NewReader(raw), 1024)
						if (err == nil) != valid {
							t.Fatal("bootstrap bypassed bind semantics", err)
						}
					}
				}
			}
		}
	}
}

func TestReplacementFlagsAndLegacyDeliveryFields(t *testing.T) {
	for _, v := range []pdu.Version{pdu.V33, pdu.V34, pdu.V50} {
		for _, n := range []byte{0, 1, 2, 3, 16, 31, 32, 100, 101, 254, 255} {
			regValid := n < 32 && (v == pdu.V50 || n%4 != 3)
			if v == pdu.V33 {
				regValid = n < 2
			}
			b := &pdu.ReplaceSM{RegisteredDelivery: n}
			raw := make([]byte, 9)
			raw[6] = n
			checkSemantic(t, v, pdu.PDU{Body: b}, semanticFrame(7, 0, raw), regValid)
			idValid := v == pdu.V50 || v == pdu.V34 && n < 255 || v == pdu.V33 && n <= 100
			b.RegisteredDelivery = 0
			b.DefaultMessageID = n
			raw[6] = 0
			raw[7] = n
			checkSemantic(t, v, pdu.PDU{Body: b}, semanticFrame(7, 0, raw), idValid)
		}
		// Unlike 3.4, 3.3 reuses the submit layout, including absolute time.
		b := &pdu.DeliverSM{SMFields: pdu.SMFields{ScheduleDeliveryTime: "240101000000000+"}}
		raw := append(make([]byte, 10), []byte(b.ScheduleDeliveryTime)...)
		raw = append(raw, make([]byte, 7)...)
		checkSemantic(t, v, pdu.PDU{Body: b}, semanticFrame(5, 0, raw), v != pdu.V34)
	}
}

func FuzzSemanticTimes(f *testing.F) {
	for _, s := range []string{"", "240229235959948+", "000000010000000R", "241231235959", "230229000000000+"} {
		f.Add(byte(1), s)
	}
	f.Fuzz(func(t *testing.T, profile byte, s string) {
		if len(s) > 32 {
			t.Skip()
		}
		v := []pdu.Version{pdu.V33, pdu.V34, pdu.V50}[int(profile)%3]
		c := newCodec(t, v)
		// The header/body below is built independently, keeping framing valid as
		// the fuzzer mutates digits, embedded NULs, suffixes and Unicode bytes.
		body := append(make([]byte, 10), []byte(s)...)
		body = append(body, make([]byte, 7)...)
		raw := semanticFrame(4, 0, body)
		packet, err := c.Decode(raw)
		if err == nil {
			got, err := c.Append(nil, packet)
			if err != nil || !bytes.Equal(got, raw) {
				t.Fatal("time changed on roundtrip", err)
			}
		}
		b := &pdu.SubmitSM{SMFields: pdu.SMFields{ScheduleDeliveryTime: s}}
		encoded, err := c.Append(nil, pdu.PDU{Sequence: 7, Body: b})
		if err == nil {
			if !bytes.Equal(encoded, raw) {
				t.Fatal("encoder changed input time")
			}
			if _, err := c.Decode(encoded); err != nil {
				t.Fatal(err)
			}
		}
		if b.ScheduleDeliveryTime != s {
			t.Fatal("input mutated")
		}
	})
}
