// SPDX-AI-Disclosure: ai-generated
// SPDX-AI-Model: GPT-6.0 Astra
// SPDX-AI-Provider: OpenAI
// SPDX-AI-Scope: Benchmark code generated and revised with Codex under maintainer direction.
// SPDX-AI-Date: 2026-10-29

package codec_test

import (
	"bytes"
	"fmt"
	"io"
	"testing"

	"github.com/skill215/smppkit/codec"
	"github.com/skill215/smppkit/pdu"
)

func BenchmarkRawFrame(b *testing.B) {
	for _, size := range []int{16, 256, 4096, 65536} {
		raw := wireFrame(0x10200, make([]byte, size-16))
		b.Run(fmt.Sprintf("Read/%d", size), func(b *testing.B) {
			r := bytes.NewReader(raw)
			b.ReportAllocs()
			b.SetBytes(int64(size))
			for b.Loop() {
				r.Reset(raw)
				got, err := codec.ReadFrame(r, size)
				if err != nil {
					b.Fatal(err)
				}
				benchmarkBytes = got
			}
		})
		b.Run(fmt.Sprintf("WriteDiscard/%d", size), func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(size))
			for b.Loop() {
				if n, err := codec.WriteFrame(io.Discard, raw, size); err != nil || n != size {
					b.Fatal(err)
				}
			}
		})
		b.Run(fmt.Sprintf("WriteBuffer/%d", size), func(b *testing.B) {
			var out bytes.Buffer
			out.Grow(size)
			b.ReportAllocs()
			b.SetBytes(int64(size))
			for b.Loop() {
				out.Reset()
				if n, err := codec.WriteFrame(&out, raw, size); err != nil || n != size {
					b.Fatal(err)
				}
			}
			benchmarkBytes = out.Bytes()
		})
	}
}

func BenchmarkDecodeDiagnostic(b *testing.B) {
	c := newCodec(b, pdu.V34)
	bad := wireFrame(4, []byte{0})
	b.Run("DecodeCompleteBadBody", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if _, err := c.Decode(bad); err == nil {
				b.Fatal("accepted invalid body")
			}
		}
	})
	b.Run("ReadCompleteBadBody", func(b *testing.B) {
		r := bytes.NewReader(bad)
		b.ReportAllocs()
		for b.Loop() {
			r.Reset(bad)
			if _, err := c.Read(r); err == nil {
				b.Fatal("accepted invalid body")
			}
		}
	})
}
