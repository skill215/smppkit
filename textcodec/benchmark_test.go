package textcodec_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/skill215/smppkit/textcodec"
)

var benchmarkEncoded textcodec.Encoded
var benchmarkText string

// Encode includes character-boundary allocation; Decode returns an owned Go
// string. Throughput uses input UTF-8 bytes for Encode, encoded bytes for Decode.
func BenchmarkTextCodec(b *testing.B) {
	for _, tc := range []struct {
		name, text string
		encoding   textcodec.Alphabet
	}{
		{"GSM7/Basic160", strings.Repeat("A", 160), textcodec.GSM7},
		{"GSM7/Extension80", strings.Repeat("^", 80), textcodec.GSM7},
		{"Latin1/140", strings.Repeat("ÿ", 140), textcodec.Latin1},
		{"UCS2/70", strings.Repeat("中", 70), textcodec.UCS2},
		{"UTF16BE/35", strings.Repeat("😀", 35), textcodec.UTF16BE},
	} {
		b.Run(tc.name, func(b *testing.B) {
			want, err := tc.encoding.Encode(tc.text)
			if err != nil {
				b.Fatal(err)
			}
			b.Run("Encode", func(b *testing.B) {
				var got textcodec.Encoded
				b.ReportAllocs()
				b.SetBytes(int64(len(tc.text)))
				for b.Loop() {
					got, err = tc.encoding.Encode(tc.text)
					if err != nil {
						b.Fatal(err)
					}
				}
				benchmarkEncoded = got
				if !bytes.Equal(got.Bytes, want.Bytes) {
					b.Fatal("encoding mismatch")
				}
			})
			b.Run("Decode", func(b *testing.B) {
				var got string
				b.ReportAllocs()
				b.SetBytes(int64(len(want.Bytes)))
				for b.Loop() {
					got, err = tc.encoding.Decode(want.Bytes)
					if err != nil {
						b.Fatal(err)
					}
				}
				benchmarkText = got
				if got != tc.text {
					b.Fatal("decoding mismatch")
				}
			})
		})
	}
}
