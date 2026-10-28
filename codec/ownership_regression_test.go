// SPDX-AI-Disclosure: ai-generated
// SPDX-AI-Model: GPT-6.0 Astra
// SPDX-AI-Provider: OpenAI
// SPDX-AI-Scope: Test code generated and revised with Codex under maintainer direction.
// SPDX-AI-Date: 2026-10-28

package codec_test

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/skill215/smppkit/pdu"
)

// RV20: literal frames, not Codec.Append output. Include empty fields because
// append to a zero-length slice must not write into a following TLV either.
var rv20Frames = []struct{ name, hex string }{
	{"adjacent-TLVs", "00000024 00000103 00000000 00000001 00000000000000000000 1500000111 1501000122"},
	{"empty-TLV", "00000023 00000103 00000000 00000001 00000000000000000000 15000000 1501000122"},
	{"short-message", "00000028 00000004 00000000 00000001 0000000000000000000000000000000002 4869 1501000122"},
	{"empty-short-message", "00000026 00000004 00000000 00000001 00 000000 000000 00 00 00 00 00 00 00 00 00 00 1501000122"},
}

func rv20ByteFields(p pdu.PDU) [][]byte {
	var fields [][]byte
	switch b := p.Body.(type) {
	case *pdu.SubmitSM:
		fields = append(fields, b.ShortMessage)
		for _, f := range b.Optional {
			fields = append(fields, f.Value)
		}
	case *pdu.DeliverSM:
		fields = append(fields, b.ShortMessage)
		for _, f := range b.Optional {
			fields = append(fields, f.Value)
		}
	case *pdu.SubmitMulti:
		fields = append(fields, b.ShortMessage)
		for _, f := range b.Optional {
			fields = append(fields, f.Value)
		}
	case *pdu.DataSM:
		for _, f := range b.Optional {
			fields = append(fields, f.Value)
		}
	}
	return fields
}

// Check append behavior as well as capacities. A shared owned body is allowed;
// append must allocate rather than reach a sibling field or the wire input.
func assertRV20Isolation(t *testing.T, p pdu.PDU, input []byte) {
	t.Helper()
	original := bytes.Clone(input)
	fields := rv20ByteFields(p)
	snapshots := make([][]byte, len(fields))
	for i, b := range fields {
		snapshots[i] = bytes.Clone(b)
	}
	for i, b := range fields {
		grown := append(b, 1, 2, 3, 4, 5)
		if !bytes.Equal(grown[len(b):], []byte{1, 2, 3, 4, 5}) {
			t.Fatal("append lost bytes")
		}
		for j, other := range fields {
			if !bytes.Equal(other, snapshots[j]) {
				t.Fatalf("append field %d changed field %d: %x -> %x", i, j, snapshots[j], other)
			}
		}
		if cap(b) != len(b) {
			t.Fatalf("field %d exposes sibling/spare capacity: len=%d cap=%d", i, len(b), cap(b))
		}
	}
	if !bytes.Equal(input, original) {
		t.Fatal("decoded edit changed input")
	}
}

func TestRV20DecodedFieldIsolation(t *testing.T) {
	for _, v := range []pdu.Version{pdu.V34, pdu.V50} {
		for _, tc := range rv20Frames {
			for _, stream := range []bool{false, true} {
				t.Run(fmt.Sprintf("%x/%s/Read=%t", v, tc.name, stream), func(t *testing.T) {
					raw := unhex(t, tc.hex)
					c := newCodec(t, v)
					var p pdu.PDU
					var err error
					if stream {
						p, err = c.Read(bytes.NewReader(raw))
					} else {
						p, err = c.Decode(raw)
					}
					if err != nil {
						t.Fatal(err)
					}
					assertRV20Isolation(t, p, raw)
					// Ordinary in-range mutation also stays local, and cannot change input.
					fields := rv20ByteFields(p)
					original := bytes.Clone(raw)
					for i, b := range fields {
						if len(b) == 0 {
							continue
						}
						snapshots := make([][]byte, len(fields))
						for j, other := range fields {
							snapshots[j] = bytes.Clone(other)
						}
						b[0] ^= 0xff
						for j, other := range fields {
							if j != i && !bytes.Equal(other, snapshots[j]) {
								t.Fatal("in-range mutation changed sibling field")
							}
						}
						b[0] = snapshots[i][0]
					}
					if !bytes.Equal(raw, original) {
						t.Fatal("input mutation")
					}
				})
			}
		}
	}
}
