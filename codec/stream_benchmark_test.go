package codec_test

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"testing"

	"github.com/skill215/smppkit/pdu"
	"github.com/skill215/smppkit/tlv"
)

// Read includes framing/input allocation as well as Decode's owned result.
// Reset reuses the reader, not the frame allocated by Codec.Read; no sockets or
// Session deadlines are involved. Compare with Decode using the same fixtures.
func BenchmarkCodecRead(b *testing.B) {
	for _, tc := range codecBenchmarkCases() {
		b.Run(tc.name, func(b *testing.B) {
			c, raw := benchmarkFrame(b, tc)
			reader := bytes.NewReader(raw)
			var packet pdu.PDU
			b.ReportAllocs()
			b.SetBytes(int64(len(raw)))
			for b.Loop() {
				reader.Reset(raw)
				var err error
				packet, err = c.Read(reader)
				if err != nil {
					b.Fatal(err)
				}
			}
			benchmarkPacket = packet
		})
	}
}

// Isolate parameter-count scaling with distinct unknown customer tags. Values
// are eight bytes each; definition callbacks and standard-tag dependencies are
// deliberately absent. Both paths still perform normal Codec validation.
func BenchmarkCodecTLVs(b *testing.B) {
	for _, count := range []int{0, 8, 64} {
		fields := make(tlv.Fields, count)
		for i := range fields {
			fields[i] = tlv.Field{Tag: 0x1500 + uint16(i), Value: bytes.Repeat([]byte{'v'}, 8)}
		}
		tc := codecBenchmarkCase{fmt.Sprintf("%d", count), pdu.V50, pdu.PDU{Sequence: 1, Body: &pdu.SubmitSM{SMFields: pdu.SMFields{ShortMessage: []byte("hello"), Optional: fields}}}}
		b.Run(tc.name, func(b *testing.B) {
			c, raw := benchmarkFrame(b, tc)
			b.Run("EncodeReuse", func(b *testing.B) {
				dst := make([]byte, 0, len(raw))
				b.ReportAllocs()
				b.SetBytes(int64(len(raw)))
				for b.Loop() {
					var err error
					dst, err = c.Append(dst[:0], tc.packet)
					if err != nil {
						b.Fatal(err)
					}
				}
				benchmarkBytes = dst
				if !bytes.Equal(dst, raw) {
					b.Fatal("frame mismatch")
				}
			})
			b.Run("Decode", func(b *testing.B) {
				var packet pdu.PDU
				b.ReportAllocs()
				b.SetBytes(int64(len(raw)))
				for b.Loop() {
					var err error
					packet, err = c.Decode(raw)
					if err != nil {
						b.Fatal(err)
					}
				}
				benchmarkPacket = packet
			})
		})
	}
}

// Rejection has a separate cost from successful decode. The malformed TLV has
// a correct frame length but a value shorter than its declared parameter length.
func BenchmarkCodecReject(b *testing.B) {
	c, raw := benchmarkFrame(b, codecBenchmarkCase{"", pdu.V50, pdu.PDU{Sequence: 1, Body: &pdu.DataSM{Optional: tlv.Fields{{Tag: 0x1500, Value: []byte{1, 2}}}}}})
	truncated := bytes.Clone(raw[:len(raw)-1])
	binary.BigEndian.PutUint32(truncated, uint32(len(truncated)))
	for _, tc := range []struct {
		name string
		raw  []byte
	}{{"ShortHeader", raw[:15]}, {"TruncatedTLV", truncated}} {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if _, err := c.Decode(tc.raw); err == nil {
					b.Fatal("malformed frame accepted")
				}
			}
		})
	}
}
