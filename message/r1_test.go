// SPDX-AI-Disclosure: ai-generated
// SPDX-AI-Model: GPT-6.0 Astra
// SPDX-AI-Provider: OpenAI
// SPDX-AI-Scope: Test code generated and revised with Codex under maintainer direction.
// SPDX-AI-Date: 2026-10-28

package message_test

import (
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/skill215/smppkit/codec"
	"github.com/skill215/smppkit/message"
	"github.com/skill215/smppkit/pdu"
	"github.com/skill215/smppkit/textcodec"
)

// Custom encoders own peer-specific DCS semantics. A slice deliberately makes
// this implementation non-comparable: checking built-ins must not compare
// arbitrary Encoding interface values. DCS 4 selects raw eight-bit GSM data;
// interpreting it as Latin-1 here is an explicit application/peer agreement.
type r1PeerEncoding struct {
	textcodec.Encoding
	dcs []byte
}

func (e r1PeerEncoding) DataCoding() uint8 { return e.dcs[0] }

func TestR1EncodingVersionPolicy(t *testing.T) {
	for _, v := range []pdu.Version{pdu.V33, pdu.V34, pdu.V50} {
		for _, tc := range []struct {
			name, text         string
			encoding, fallback textcodec.Encoding
			coding             byte
			payload            []byte
			latin1             bool
		}{
			{"latin1", "@", textcodec.Latin1, nil, 3, []byte{0x40}, true},
			{"latin1-pointer", "@", new(textcodec.Latin1), nil, 3, []byte{0x40}, true},
			{"empty-latin1", "", textcodec.Latin1, nil, 3, nil, true},
			{"fallback-latin1", "ÿ", textcodec.GSM7, textcodec.Latin1, 3, []byte{0xff}, true},
			{"fallback-latin1-pointer", "ÿ", textcodec.GSM7, new(textcodec.Latin1), 3, []byte{0xff}, true},
			{"unused-latin1-fallback", "@", textcodec.GSM7, textcodec.Latin1, 0, []byte{0}, false},
			{"latin1-to-ucs2", "中", textcodec.Latin1, textcodec.UCS2, 8, []byte{0x4e, 0x2d}, false},
			{"ucs2", "@", textcodec.UCS2, nil, 8, []byte{0, 0x40}, false},
			{"utf16-compat", "😀", textcodec.UTF16BE, nil, 8, []byte{0xd8, 0x3d, 0xde, 0}, false},
			{"explicit-peer-policy", "@", r1PeerEncoding{textcodec.Latin1, []byte{4}}, nil, 4, []byte{0x40}, false},
		} {
			t.Run(fmt.Sprintf("%x/%s", v, tc.name), func(t *testing.T) {
				plan, err := message.Prepare(v, message.Text{Text: tc.text}, message.Policy{Encoding: tc.encoding, Fallback: tc.fallback})
				if v == pdu.V33 && tc.latin1 {
					if !errors.Is(err, message.ErrMetadata) || plan != nil {
						t.Fatalf("incompatible coding accepted: %v %v", plan, err)
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				parts, err := plan.Materialize(0)
				if err != nil || len(parts) != 1 || plan.DataCoding() != tc.coding || parts[0].DataCoding != tc.coding || !bytes.Equal(parts[0].ShortMessage, tc.payload) {
					t.Fatalf("coding/payload changed: %+v %v", parts, err)
				}
				c, err := codec.New(codec.Config{Version: v})
				if err != nil {
					t.Fatal(err)
				}
				if _, err := c.Append(nil, pdu.PDU{Sequence: 1, Body: &pdu.SubmitSM{SMFields: parts[0]}}); err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}

// RV19 does not prohibit raw GSM DCS=3 in the versioned Codec. These literal
// bytes reproduce the earlier bad planner output and must remain preservable
// by a low-level application which owns its DCS/payload interpretation.
func TestR1RawV33CodingRemainsAvailable(t *testing.T) {
	wire, err := hex.DecodeString("00000022000000040000000000000001000000000000000000000000000003000140")
	if err != nil {
		t.Fatal(err)
	}
	c, err := codec.New(codec.Config{Version: pdu.V33})
	if err != nil {
		t.Fatal(err)
	}
	packet, err := c.Decode(wire)
	if err != nil {
		t.Fatal(err)
	}
	b := packet.Body.(*pdu.SubmitSM)
	if b.DataCoding != 3 || !bytes.Equal(b.ShortMessage, []byte{0x40}) {
		t.Fatalf("raw fields changed: %+v", b)
	}
	got, err := c.Append(nil, packet)
	if err != nil || !bytes.Equal(got, wire) {
		t.Fatalf("wire changed: %x %v", got, err)
	}
}

// Exercise encoding selection/fallback across version boundaries, rather than
// testing only encoder round trips. Check both acceptance and rejection: a
// planner that rejects every input must fail this oracle. The encoder supplies
// bytes/selection; the acceptance limits below are independent of Prepare.
func FuzzPrepareVersionEncoding(f *testing.F) {
	for _, s := range []string{
		"", "@", "ÿ", "中", "😀", "hello ^{}€", "\xff",
		strings.Repeat("A", 160), strings.Repeat("A", 161),
		strings.Repeat("^", 80), strings.Repeat("^", 81),
		strings.Repeat("中", 70), strings.Repeat("中", 71),
		strings.Repeat("😀", 35), strings.Repeat("😀", 36),
		strings.Repeat("A", 4096),
	} {
		// Seed every primary/fallback pair; mutation should not be required to
		// reach direct Latin-1, selected fallback or an unused fallback.
		for first := range uint8(4) {
			for second := range uint8(4) {
				f.Add(s, first, second)
			}
		}
	}
	f.Fuzz(func(t *testing.T, s string, first, second uint8) {
		if len(s) > 4096 {
			t.Skip()
		}
		primary := textcodec.Alphabet(first%4 + 1)
		fallback := textcodec.Alphabet(second%4 + 1)
		selected := primary
		encoded, encodeErr := selected.Encode(s)
		if encodeErr != nil {
			selected = fallback
			encoded, encodeErr = selected.Encode(s)
		}
		for _, v := range []pdu.Version{pdu.V33, pdu.V34, pdu.V50} {
			plan, err := message.Prepare(v, message.Text{Text: s}, message.Policy{Encoding: primary, Fallback: fallback})
			if encodeErr != nil {
				if !errors.Is(err, textcodec.ErrEncoding) || plan != nil {
					t.Fatalf("unencodable input: version=%x plan=%v err=%v", v, plan, err)
				}
				continue
			}
			// Default radio budget: 160 unpacked GSM septets or 140 octets.
			// 3.3 cannot use the later Latin-1 DCS or standard concatenation.
			singleLimit := 140
			if selected == textcodec.GSM7 {
				singleLimit = 160
			}
			if v == pdu.V33 && (selected == textcodec.Latin1 || len(encoded.Bytes) > singleLimit) {
				if !errors.Is(err, message.ErrMetadata) || plan != nil {
					t.Fatalf("unsupported 3.3 policy accepted: encoding=%s bytes=%d err=%v", selected.Name(), len(encoded.Bytes), err)
				}
				continue
			}
			// At most 4096 input bytes become at most 8192 bytes in these
			// built-ins, safely below default input/encoded/255-part limits.
			// Every remaining valid input must therefore be accepted in 3.4/5.0;
			// 3.3 reaches here only for a supported single-part encoding.
			if err != nil || plan == nil {
				t.Fatalf("valid bounded input rejected: version=%x encoding=%s bytes=%d err=%v", v, selected.Name(), len(encoded.Bytes), err)
			}
			if plan.DataCoding() != selected.DataCoding() || plan.Encoding() != selected.Name() || !bytes.Equal(bytes.Join(plan.Preview(), nil), encoded.Bytes) {
				t.Fatalf("accepted plan changed selected encoding or content: version=%x", v)
			}
		}
	})
}
