package textcodec_test

import (
	"bytes"
	"encoding/hex"
	"github.com/skill215/smppkit/textcodec"
	"testing"
)

func TestIndependentCharsets(t *testing.T) {
	for _, tc := range []struct {
		a         textcodec.Alphabet
		text, hex string
	}{{textcodec.GSM7, "@£A^€", "0001411b141b65"}, {textcodec.Latin1, "\u0080éÿ", "80e9ff"}, {textcodec.UCS2, "A中", "00414e2d"}, {textcodec.UTF16BE, "😀", "d83dde00"}} {
		got, e := tc.a.Encode(tc.text)
		if e != nil {
			t.Fatal(e)
		}
		want, _ := hex.DecodeString(tc.hex)
		if !bytes.Equal(got.Bytes, want) {
			t.Fatalf("%s %x", tc.a.Name(), got.Bytes)
		}
		s, e := tc.a.Decode(want)
		if e != nil || s != tc.text {
			t.Fatal(s, e)
		}
	}
}
func TestStrictErrors(t *testing.T) {
	for _, tc := range []struct {
		a textcodec.Alphabet
		s string
	}{{textcodec.GSM7, "中"}, {textcodec.GSM7, "\x1b"}, {textcodec.Latin1, "€"}, {textcodec.UCS2, "😀"}, {textcodec.UTF16BE, string([]byte{255})}} {
		if _, e := tc.a.Encode(tc.s); e == nil {
			t.Fatal(tc.a, tc.s)
		}
	}
	for _, tc := range []struct {
		a textcodec.Alphabet
		b []byte
	}{{textcodec.GSM7, []byte{0x1b}}, {textcodec.GSM7, []byte{0x1b, 0}}, {textcodec.GSM7, []byte{128}}, {textcodec.UCS2, []byte{0}}, {textcodec.UCS2, []byte{0xd8, 0x3d}}, {textcodec.UTF16BE, []byte{0xdc, 0}}, {textcodec.UTF16BE, []byte{0xd8, 0, 0, 65}}} {
		if _, e := tc.a.Decode(tc.b); e == nil {
			t.Fatal(tc)
		}
	}
}
func FuzzCharsets(f *testing.F) {
	f.Add("A^中😀\u0080")
	f.Fuzz(func(t *testing.T, s string) {
		if len(s) > 65536 {
			return
		}
		for _, a := range []textcodec.Alphabet{textcodec.GSM7, textcodec.Latin1, textcodec.UCS2, textcodec.UTF16BE} {
			enc, e := a.Encode(s)
			if e != nil {
				continue
			}
			if e = textcodec.Validate(enc); e != nil {
				t.Fatal(e)
			}
			got, e := a.Decode(enc.Bytes)
			if e != nil || got != s {
				t.Fatalf("%s roundtrip %v", a.Name(), e)
			}
		}
	})
}
