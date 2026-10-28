// SPDX-AI-Disclosure: ai-generated
// SPDX-AI-Model: GPT-6.0 Astra
// SPDX-AI-Provider: OpenAI
// SPDX-AI-Scope: Benchmark code generated and revised with Codex under maintainer direction.
// SPDX-AI-Date: 2026-10-28

package codec_test

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/skill215/smppkit/codec"
	"github.com/skill215/smppkit/pdu"
	"github.com/skill215/smppkit/tlv"
)

// Sinks keep results observable without retaining every iteration's allocation.
// Benchmarks are serial; these are not shared by any RunParallel workers.
var benchmarkBytes []byte
var benchmarkPacket pdu.PDU

type codecBenchmarkCase struct {
	name    string
	version pdu.Version
	packet  pdu.PDU
}

// Synthetic messages exercise real string/payload/TLV ownership. Construction
// and fixture serialization stay outside each sub-benchmark's timed b.Loop.
func codecBenchmarkCases() []codecBenchmarkCase {
	source := pdu.Address{TON: 5, Value: "sender"}
	dest := pdu.Address{TON: 1, NPI: 1, Value: "12345678901"}
	var cases []codecBenchmarkCase
	for _, v := range []pdu.Version{pdu.V33, pdu.V34, pdu.V50} {
		cases = append(cases, codecBenchmarkCase{
			fmt.Sprintf("SubmitSM/V%02x/140", v), v,
			pdu.PDU{Sequence: 1, Body: &pdu.SubmitSM{SMFields: pdu.SMFields{
				Source: source, Destination: dest, ShortMessage: bytes.Repeat([]byte{'A'}, 140),
			}}},
		})
	}
	cases = append(cases, codecBenchmarkCase{"SubmitSM/V50/255", pdu.V50,
		pdu.PDU{Sequence: 1, Body: &pdu.SubmitSM{SMFields: pdu.SMFields{
			Source: source, Destination: dest, ShortMessage: bytes.Repeat([]byte{'A'}, 255),
		}}},
	})
	for _, size := range []int{140, 4096, 65535} {
		cases = append(cases, codecBenchmarkCase{
			fmt.Sprintf("DataSM/V50/%d", size), pdu.V50,
			pdu.PDU{Sequence: 1, Body: &pdu.DataSM{
				Source: source, Destination: dest, ESMClass: 2,
				Optional: tlv.Fields{{Tag: tlv.MessagePayload, Value: bytes.Repeat([]byte{'A'}, size)}},
			}},
		})
	}
	for _, count := range []int{2, 255} {
		body := &pdu.SubmitMulti{Source: source, ShortMessage: bytes.Repeat([]byte{'A'}, 140)}
		for range count {
			body.Destinations = append(body.Destinations, pdu.MultiDestination{Kind: pdu.SMEAddress, Address: dest})
		}
		cases = append(cases, codecBenchmarkCase{
			fmt.Sprintf("SubmitMulti/V50/%d", count), pdu.V50, pdu.PDU{Sequence: 1, Body: body},
		})
	}
	return cases
}

func benchmarkFrame(b *testing.B, tc codecBenchmarkCase) (*codec.Codec, []byte) {
	b.Helper()
	// A maximum-width TLV plus header/addresses exceeds a 64 KiB frame cap.
	// Use one explicit 128 KiB profile for every case, not a different hot path.
	c, err := codec.New(codec.Config{Version: tc.version, MaxFrame: 128 * 1024})
	if err != nil {
		b.Fatal(err)
	}
	raw, err := c.Append(nil, tc.packet)
	if err != nil {
		b.Fatal(err)
	}
	if _, err := c.Decode(raw); err != nil {
		b.Fatal(err)
	}
	return c, raw
}

// NewBuffer measures Append(nil, p); ReuseBuffer supplies enough capacity for
// the complete frame. Reuse removes destination growth, not the Codec's private
// construction buffer or the final copy into dst. Both retain normal validation.
func BenchmarkCodecEncode(b *testing.B) {
	for _, tc := range codecBenchmarkCases() {
		b.Run(tc.name, func(b *testing.B) {
			c, raw := benchmarkFrame(b, tc)
			for _, reuse := range []bool{false, true} {
				name := "NewBuffer"
				if reuse {
					name = "ReuseBuffer"
				}
				b.Run(name, func(b *testing.B) {
					var dst, encoded []byte
					if reuse {
						dst = make([]byte, 0, len(raw))
					}
					b.ReportAllocs()
					b.SetBytes(int64(len(raw))) // wire throughput, not memory-bus traffic
					for b.Loop() {
						var err error
						encoded, err = c.Append(dst[:0], tc.packet)
						if err != nil {
							b.Fatal(err)
						}
					}
					benchmarkBytes = encoded
					if !bytes.Equal(encoded, raw) {
						b.Fatal("encoded frame differs from fixture")
					}
					b.ReportMetric(float64(len(raw)), "wire-B/op")
				})
			}
		})
	}
}

// Decode measures the public owned-result API on one already buffered frame.
// It includes the body clone, string materialization and PDU/TLV/list objects;
// it excludes Read's transport/frame allocation and any Session context checks.
func BenchmarkCodecDecode(b *testing.B) {
	for _, tc := range codecBenchmarkCases() {
		b.Run(tc.name, func(b *testing.B) {
			c, raw := benchmarkFrame(b, tc)
			var decoded pdu.PDU
			b.ReportAllocs()
			b.SetBytes(int64(len(raw)))
			for b.Loop() {
				var err error
				decoded, err = c.Decode(raw)
				if err != nil {
					b.Fatal(err)
				}
			}
			benchmarkPacket = decoded
			b.ReportMetric(float64(len(raw)), "wire-B/op")
		})
	}
}

// Copy is a separate hot-buffer baseline, not instrumentation of Codec copies.
// Sizes equal the DataSM benchmark frame lengths (payload + 47 wire octets).
// ReuseBuffer measures copy alone; NewBuffer includes allocation and copy.
// Do not subtract these timings to claim an exact serialization/copy breakdown.
func BenchmarkCodecCopy(b *testing.B) {
	for _, size := range []int{187, 4143, 65582} {
		for _, reuse := range []bool{false, true} {
			name := "NewBuffer"
			if reuse {
				name = "ReuseBuffer"
			}
			b.Run(fmt.Sprintf("%d/%s", size, name), func(b *testing.B) {
				src := bytes.Repeat([]byte{'A'}, size)
				var dst []byte
				if reuse {
					dst = make([]byte, size)
				}
				b.ReportAllocs()
				b.SetBytes(int64(size))
				for b.Loop() {
					if !reuse {
						dst = make([]byte, size)
					}
					copy(dst, src)
				}
				benchmarkBytes = dst
				if !bytes.Equal(dst, src) {
					b.Fatal("copy differs")
				}
			})
		}
	}
}
