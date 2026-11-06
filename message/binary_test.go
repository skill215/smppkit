// SPDX-AI-Disclosure: ai-generated
// SPDX-AI-Model: GPT-6.0 Astra
// SPDX-AI-Provider: OpenAI
// SPDX-AI-Scope: Test code generated and revised with Codex under maintainer direction.
// SPDX-AI-Date: 2026-11-06

package message_test

import (
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/skill215/smppkit/codec"
	"github.com/skill215/smppkit/message"
	"github.com/skill215/smppkit/pdu"
	"github.com/skill215/smppkit/tlv"
)

func TestBinaryDCSMatrix(t *testing.T) {
	for _, version := range []pdu.Version{pdu.V33, pdu.V34, pdu.V50} {
		for dcs := range 256 {
			plan, err := message.PrepareBytes(version, message.Binary{Data: []byte{0, 255, 128}, DataCoding: byte(dcs)}, message.BinaryPolicy{})
			want := dcs == 4 || version != pdu.V33 && dcs == 2
			if want {
				if err != nil || plan.Encoding() != "binary" || plan.DataCoding() != byte(dcs) || !bytes.Equal(plan.Preview()[0], []byte{0, 255, 128}) {
					t.Fatalf("version=%x dcs=%x: %v", version, dcs, err)
				}
			} else if plan != nil || !errors.Is(err, message.ErrMetadata) {
				t.Fatalf("version=%x dcs=%x admitted: %v", version, dcs, err)
			}
		}
		// The helper whitelist is intentionally narrower than the raw Codec.
		c, _ := codec.New(codec.Config{Version: version})
		for _, dcs := range []byte{0, 2, 8} {
			if _, err := c.Append(nil, pdu.PDU{Sequence: 1, Body: &pdu.SubmitSM{SMFields: pdu.SMFields{DataCoding: dcs}}}); err != nil {
				t.Fatalf("raw Codec narrowed: version=%x dcs=%x: %v", version, dcs, err)
			}
		}
	}
}

// Complete hand-authored SMPP frames. Mandatory fields use empty addresses;
// DCS is 4, sequence 1, the first logical octets are 00 ff 80. Radio budget 8
// makes UDH8 carry two octets and UDH16/SAR one. No encoder builds expectations.
func TestBinaryIndependentWireSamples(t *testing.T) {
	for _, tc := range []struct {
		name  string
		mode  message.Mode
		count int
		hex   string
	}{
		{"UDH8", message.UDH8, 5, "00000029 00000004 00000000 00000001 00 000000 000000 40 00 00 00 00 00 00 04 00 08 0500032a050100ff"},
		{"UDH16", message.UDH16, 9, "00000029 00000004 00000000 00000001 00 000000 000000 40 00 00 00 00 00 00 04 00 08 060804002a090100"},
		{"SAR", message.SAR, 9, "00000032 00000004 00000000 00000001 00 000000 000000 00 00 00 00 00 00 00 04 00 01 00 020c0002002a 020e000109 020f000101"},
		{"Payload", message.Payload, 1, "0000002e 00000004 00000000 00000001 00 000000 000000 00 00 00 00 00 00 00 04 00 00 04240009 00ff80010203040506"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			want, err := hex.DecodeString(strings.ReplaceAll(tc.hex, " ", ""))
			if err != nil {
				t.Fatal(err)
			}
			for _, v := range []pdu.Version{pdu.V34, pdu.V50} {
				plan, err := message.PrepareBytes(v, message.Binary{Data: []byte{0, 255, 128, 1, 2, 3, 4, 5, 6}, DataCoding: 4}, message.BinaryPolicy{Mode: tc.mode, PayloadOctets: 8})
				if err != nil || plan.Count() != tc.count {
					t.Fatal(plan, err)
				}
				parts, err := plan.Materialize(42)
				if err != nil {
					t.Fatal(err)
				}
				c, _ := codec.New(codec.Config{Version: v})
				got, err := c.Append(nil, pdu.PDU{Sequence: 1, Body: &pdu.SubmitSM{SMFields: parts[0]}})
				if err != nil || !bytes.Equal(got, want) {
					t.Fatalf("got %x want %x: %v", got, want, err)
				}
				decoded, err := c.Decode(want)
				if err != nil {
					t.Fatal(err)
				}
				part, err := message.ParseSegment(decoded.Body.(*pdu.SubmitSM).SMFields)
				if err != nil || part.Fields.DataCoding != 4 {
					t.Fatal(part, err)
				}
			}
		})
	}
}

func TestBinaryLimits(t *testing.T) {
	for _, tc := range []struct {
		name    string
		version pdu.Version
		n       int
		policy  message.BinaryPolicy
		count   int
		cause   error
	}{
		{"empty", pdu.V50, 0, message.BinaryPolicy{}, 1, nil},
		{"exact", pdu.V50, 140, message.BinaryPolicy{}, 1, nil},
		{"over-radio", pdu.V50, 141, message.BinaryPolicy{}, 2, nil},
		{"udh8-exact-multiple", pdu.V34, 268, message.BinaryPolicy{Mode: message.UDH8}, 2, nil},
		{"udh16-exact-multiple", pdu.V34, 266, message.BinaryPolicy{}, 2, nil},
		{"max-parts", pdu.V50, 133 * 255, message.BinaryPolicy{}, 255, nil},
		{"over-parts", pdu.V50, 133*255 + 1, message.BinaryPolicy{}, 0, message.ErrCapacity},
		{"explicit-parts", pdu.V50, 141, message.BinaryPolicy{MaxSegments: 1}, 0, message.ErrCapacity},
		{"input-exact", pdu.V50, 20, message.BinaryPolicy{MaxInputBytes: 20}, 1, nil},
		{"input-over", pdu.V50, 21, message.BinaryPolicy{MaxInputBytes: 20}, 0, message.ErrCapacity},
		{"default-input-over", pdu.V50, 65537, message.BinaryPolicy{Mode: message.Payload}, 0, message.ErrCapacity},
		{"payload-max", pdu.V50, 65535, message.BinaryPolicy{Mode: message.Payload}, 1, nil},
		{"payload-over", pdu.V50, 65536, message.BinaryPolicy{Mode: message.Payload}, 0, message.ErrCapacity},
		{"payload-empty", pdu.V50, 0, message.BinaryPolicy{Mode: message.Payload}, 1, nil},
		{"output-exact", pdu.V50, 141, message.BinaryPolicy{MaxOutputBytes: 141 + 2*(256+7)}, 2, nil},
		{"output-over", pdu.V50, 141, message.BinaryPolicy{MaxOutputBytes: 141 + 2*(256+7) - 1}, 0, message.ErrCapacity},
		{"sar-output-exact", pdu.V50, 141, message.BinaryPolicy{Mode: message.SAR, MaxOutputBytes: 141 + 2*(256+100)}, 2, nil},
		{"sar-output-over", pdu.V50, 141, message.BinaryPolicy{Mode: message.SAR, MaxOutputBytes: 141 + 2*(256+100) - 1}, 0, message.ErrCapacity},
		{"payload-output-exact", pdu.V50, 10, message.BinaryPolicy{Mode: message.Payload, MaxOutputBytes: 298}, 1, nil},
		{"payload-output-over", pdu.V50, 10, message.BinaryPolicy{Mode: message.Payload, MaxOutputBytes: 297}, 0, message.ErrCapacity},
		{"33-single", pdu.V33, 140, message.BinaryPolicy{}, 1, nil},
		{"33-multi", pdu.V33, 141, message.BinaryPolicy{}, 0, message.ErrMetadata},
		{"33-sar", pdu.V33, 0, message.BinaryPolicy{Mode: message.SAR}, 0, message.ErrMetadata},
		{"33-payload", pdu.V33, 0, message.BinaryPolicy{Mode: message.Payload}, 0, message.ErrMetadata},
		{"bad-version", 0, 0, message.BinaryPolicy{}, 0, message.ErrMetadata},
		{"bad-mode", pdu.V50, 0, message.BinaryPolicy{Mode: 5}, 0, message.ErrCapacity},
		{"bad-radio", pdu.V50, 0, message.BinaryPolicy{PayloadOctets: 141}, 0, message.ErrCapacity},
		{"negative-input", pdu.V50, 0, message.BinaryPolicy{MaxInputBytes: -1}, 0, message.ErrCapacity},
		{"huge-output", pdu.V50, 0, message.BinaryPolicy{MaxOutputBytes: 1 << 30}, 0, message.ErrCapacity},
		{"too-many-parts", pdu.V50, 0, message.BinaryPolicy{MaxSegments: 256}, 0, message.ErrCapacity},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, err := message.PrepareBytes(tc.version, message.Binary{Data: make([]byte, tc.n), DataCoding: 4}, tc.policy)
			if !errors.Is(err, tc.cause) {
				t.Fatalf("got %v want %v", err, tc.cause)
			}
			if err != nil {
				if p != nil {
					t.Fatal("failed plan returned")
				}
				return
			}
			if p.Count() != tc.count {
				t.Fatal(p.Count(), tc.count)
			}
			parts, err := p.Materialize(42)
			if err != nil {
				t.Fatal(err)
			}
			c, _ := codec.New(codec.Config{Version: tc.version, MaxFrame: 128 << 10})
			for _, part := range parts {
				if _, err = c.Append(nil, pdu.PDU{Sequence: 1, Body: &pdu.SubmitSM{SMFields: part}}); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

func TestBinaryMetadataBudgetAndConflicts(t *testing.T) {
	port := message.InformationElement{ID: 5, Data: []byte{0x15, 0x8a, 0x15, 0x8b}}
	for _, tc := range []struct {
		name     string
		version  pdu.Version
		data     int
		optional tlv.Fields
		policy   message.BinaryPolicy
		count    int
		cause    error
	}{
		{"port-single", pdu.V50, 133, nil, message.BinaryPolicy{AdditionalUDH: []message.InformationElement{port}}, 1, nil},
		{"port-multi", pdu.V50, 134, nil, message.BinaryPolicy{AdditionalUDH: []message.InformationElement{port}}, 2, nil},
		{"header-exact-empty", pdu.V50, 0, nil, message.BinaryPolicy{PayloadOctets: 7, AdditionalUDH: []message.InformationElement{port}}, 1, nil},
		{"header-too-large", pdu.V50, 0, nil, message.BinaryPolicy{PayloadOctets: 6, AdditionalUDH: []message.InformationElement{port}}, 0, message.ErrCapacity},
		{"port-sar", pdu.V50, 255, tlv.Fields{tlv.Uint16(tlv.DestinationPort, 5500)}, message.BinaryPolicy{Mode: message.SAR}, 3, nil},
		{"port-udh-conflict", pdu.V50, 134, tlv.Fields{tlv.Uint16(tlv.DestinationPort, 5500)}, message.BinaryPolicy{}, 0, message.ErrMetadata},
		{"port-double", pdu.V50, 1, tlv.Fields{tlv.Uint16(tlv.DestinationPort, 5500)}, message.BinaryPolicy{AdditionalUDH: []message.InformationElement{port}}, 0, message.ErrMetadata},
		{"port-duplicate", pdu.V50, 1, tlv.Fields{tlv.Uint16(tlv.DestinationPort, 1), tlv.Uint16(tlv.DestinationPort, 2)}, message.BinaryPolicy{}, 0, message.ErrMetadata},
		{"port-width", pdu.V50, 1, tlv.Fields{{Tag: tlv.SourcePort, Value: []byte{1}}}, message.BinaryPolicy{}, 0, message.ErrMetadata},
		{"33-port", pdu.V33, 0, nil, message.BinaryPolicy{AdditionalUDH: []message.InformationElement{port}}, 0, message.ErrMetadata},
		{"33-customer", pdu.V33, 0, tlv.Fields{{Tag: 0x1400}}, message.BinaryPolicy{}, 0, message.ErrMetadata},
		{"caller-sar", pdu.V50, 0, tlv.Fields{tlv.Uint16(tlv.SARMsgRefNum, 1)}, message.BinaryPolicy{}, 0, message.ErrMetadata},
		{"caller-payload", pdu.V50, 0, tlv.Fields{{Tag: tlv.MessagePayload}}, message.BinaryPolicy{}, 0, message.ErrMetadata},
		{"caller-concat", pdu.V50, 0, nil, message.BinaryPolicy{AdditionalUDH: []message.InformationElement{{ID: 0, Data: []byte{1, 1, 1}}}}, 0, message.ErrMetadata},
		{"language", pdu.V50, 0, nil, message.BinaryPolicy{AdditionalUDH: []message.InformationElement{{ID: 0x24, Data: []byte{0}}}}, 0, message.ErrUnsupportedNationalLanguage},
		{"sar-udh", pdu.V50, 0, nil, message.BinaryPolicy{Mode: message.SAR, AdditionalUDH: []message.InformationElement{port}}, 0, message.ErrMetadata},
		{"payload-udh", pdu.V50, 0, nil, message.BinaryPolicy{Mode: message.Payload, AdditionalUDH: []message.InformationElement{port}}, 0, message.ErrMetadata},
		{"large-tlv", pdu.V50, 0, tlv.Fields{{Tag: 0x1400, Value: make([]byte, 65536)}}, message.BinaryPolicy{}, 0, message.ErrMetadata},
		{"metadata-exact", pdu.V50, 141, tlv.Fields{{Tag: 0x1400, Value: []byte{1, 2}}}, message.BinaryPolicy{MaxOutputBytes: 141 + 2*(256+7+34)}, 2, nil},
		{"metadata-over", pdu.V50, 141, tlv.Fields{{Tag: 0x1400, Value: []byte{1, 2}}}, message.BinaryPolicy{MaxOutputBytes: 141 + 2*(256+7+34) - 1}, 0, message.ErrCapacity},
		{"empty-tlv-flood", pdu.V50, 1, make(tlv.Fields, 8192), message.BinaryPolicy{}, 0, message.ErrCapacity},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, err := message.PrepareBytes(tc.version, message.Binary{Data: make([]byte, tc.data), DataCoding: 4, Optional: tc.optional}, tc.policy)
			if !errors.Is(err, tc.cause) {
				t.Fatalf("got %v want %v", err, tc.cause)
			}
			if err == nil && p.Count() != tc.count {
				t.Fatal(p.Count(), tc.count)
			}
		})
	}
}

func TestBinaryOwnershipReassemblyAndConcurrentPlan(t *testing.T) {
	for _, mode := range []message.Mode{message.UDH8, message.UDH16, message.SAR, message.Payload} {
		t.Run(fmt.Sprint(mode), func(t *testing.T) {
			data := bytes.Repeat([]byte{0, 255, 128, 0xc0, 0xaf}, 80)
			want := slices.Clone(data)
			fields := tlv.Fields{{Tag: 0x1400, Value: []byte{9, 8}}}
			policy := message.BinaryPolicy{Mode: mode}
			if mode == message.UDH8 || mode == message.UDH16 {
				policy.AdditionalUDH = []message.InformationElement{{ID: 5, Data: []byte{1, 2, 3, 4}}}
			}
			p, err := message.PrepareBytes(pdu.V50, message.Binary{Data: data, DataCoding: 2, Optional: fields}, policy)
			if err != nil {
				t.Fatal(err)
			}
			clear(data)
			fields[0].Value[0] = 0
			if len(policy.AdditionalUDH) > 0 {
				policy.AdditionalUDH[0].Data[0] = 0
			}
			var wg sync.WaitGroup
			for range 8 {
				wg.Go(func() {
					parts, err := p.Materialize(42)
					if err != nil {
						t.Error(err)
						return
					}
					r, err := message.NewReceiver(message.ReceiveConfig{AckPolicy: message.AckOnBuffered})
					if err != nil {
						t.Error(err)
						return
					}
					for i := len(parts) - 1; i >= 0; i-- {
						if !bytes.Equal(parts[i].Optional[0].Value, []byte{9, 8}) {
							t.Error("optional not snapshotted")
						}
						if len(policy.AdditionalUDH) > 0 && !bytes.Contains(parts[i].ShortMessage, []byte{5, 4, 1, 2, 3, 4}) {
							t.Error("UDH not snapshotted")
						}
						if _, err = r.Admit("binary", parts[i], time.Now()); err != nil {
							t.Error(err)
							return
						}
					}
					got, err := r.Receive(t.Context())
					if err != nil || !bytes.Equal(got.Payload, want) || got.Decoded || got.DecodeError != nil {
						t.Error("raw reconstruction", err)
					}
					// Each returned view is independent of the plan and other workers.
					for _, part := range parts {
						clear(part.ShortMessage)
						for _, f := range part.Optional {
							clear(f.Value)
						}
					}
					for _, b := range p.Preview() {
						clear(b)
					}
				})
			}
			wg.Wait()
			var restored []byte
			for _, b := range p.Preview() {
				restored = append(restored, b...)
			}
			if !bytes.Equal(restored, want) {
				t.Fatal("plan mutated")
			}
		})
	}
}

// Native campaigns use execution budgets on Go 1.26, as in the existing CI.
func FuzzPrepareBytes(f *testing.F) {
	f.Add([]byte{0, 255, 128, 1, 2, 3, 4, 5, 6}, byte(0x50), byte(4), byte(message.UDH8), byte(8))
	f.Add([]byte{}, byte(0x33), byte(2), byte(message.Payload), byte(140))
	for _, mode := range []message.Mode{message.UDH8, message.UDH16, message.SAR, message.Payload} {
		f.Add(bytes.Repeat([]byte{0, 255, 128}, 100), byte(0x34), byte(2), byte(mode), byte(140))
	}
	f.Fuzz(func(t *testing.T, data []byte, version, dcs, mode, octets byte) {
		p, err := message.PrepareBytes(pdu.Version(version), message.Binary{Data: data, DataCoding: dcs}, message.BinaryPolicy{Mode: message.Mode(mode), PayloadOctets: int(octets), MaxInputBytes: 4096})
		// Independent acceptance oracle for this deliberately metadata-free input.
		// It catches accidental over-rejection as well as malformed output.
		validVersion := version == 0x33 || version == 0x34 || version == 0x50
		want := validVersion && (dcs == 4 || version != 0x33 && dcs == 2) && mode <= 4 && octets <= 140 && len(data) <= 4096
		radio := int(octets)
		if radio == 0 {
			radio = 140
		}
		if version == 0x33 && (mode == byte(message.SAR) || mode == byte(message.Payload)) {
			want = false
		}
		if mode != byte(message.Payload) && len(data) > radio {
			overhead := 7
			if mode == byte(message.UDH8) {
				overhead = 6
			}
			capacity := radio - overhead
			if version == 0x33 || capacity <= 0 {
				want = false
			} else if (len(data)+capacity-1)/capacity > 255 {
				want = false
			}
		}
		if (err == nil) != want {
			t.Fatalf("acceptance version=%x dcs=%x mode=%d radio=%d n=%d: %v", version, dcs, mode, radio, len(data), err)
		}
		if err != nil {
			return
		}
		if p.Count() < 1 || p.Count() > 255 || p.Encoding() != "binary" || p.DataCoding() != dcs {
			t.Fatal("plan invariant")
		}
		parts, err := p.Materialize(42)
		if err != nil {
			t.Fatal(err)
		}
		c, err := codec.New(codec.Config{Version: pdu.Version(version)})
		if err != nil {
			t.Fatal(err)
		}
		var got []byte
		for i, part := range parts {
			if _, err = c.Append(nil, pdu.PDU{Sequence: 1, Body: &pdu.SubmitSM{SMFields: part}}); err != nil {
				t.Fatal(err)
			}
			s, err := message.ParseSegment(part)
			if err != nil {
				t.Fatal(err)
			}
			if p.Multipart() && (s.Concat == nil || int(s.Concat.Number) != i+1 || int(s.Concat.Total) != len(parts)) {
				t.Fatal("concat invariant")
			}
			got = append(got, s.Payload...)
		}
		if !bytes.Equal(got, data) {
			t.Fatal("changed octets")
		}
	})
}
