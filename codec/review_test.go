// SPDX-AI-Disclosure: ai-generated
// SPDX-AI-Model: GPT-6.0 Astra
// SPDX-AI-Provider: OpenAI
// SPDX-AI-Scope: Test code generated and revised with Codex under maintainer direction.
// SPDX-AI-Date: 2026-10-28

package codec_test

import (
	"bytes"
	"encoding/binary"
	"errors"
	"testing"

	"github.com/skill215/smppkit/codec"
	"github.com/skill215/smppkit/pdu"
	"github.com/skill215/smppkit/tlv"
)

func TestReviewCustomerTLVBoundsBeforeCallback(t *testing.T) {
	calls := 0
	c, err := codec.New(codec.Config{Version: pdu.V50, TLVs: []codec.TLVDefinition{{
		Tag: 0x1500, MinLength: 1, MaxLength: 1, Versions: []pdu.Version{pdu.V50}, Commands: []pdu.CommandID{pdu.SubmitSMID},
		Validate: func(value []byte) error {
			calls++
			if len(value) != 1 {
				t.Error("callback received a value outside its declared bounds")
			}
			return nil
		},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.Append(nil, pdu.PDU{Sequence: 1, Body: &pdu.SubmitSM{SMFields: pdu.SMFields{Optional: tlv.Fields{{Tag: 0x1500}}}}})
	if err == nil || calls != 0 {
		t.Fatalf("encode: err=%v callback calls=%d", err, calls)
	}
	// Independent empty submit mandatory body followed by a zero-length vendor TLV.
	frame := make([]byte, 37)
	binary.BigEndian.PutUint32(frame, 37)
	binary.BigEndian.PutUint32(frame[4:], 4)
	binary.BigEndian.PutUint32(frame[12:], 1)
	copy(frame[33:], []byte{0x15, 0, 0, 0})
	_, err = c.Decode(frame)
	if err == nil || calls != 0 {
		t.Fatalf("decode: err=%v callback calls=%d", err, calls)
	}
}

func TestReviewDataSMRejectsUDHIWithSAR(t *testing.T) {
	optional := tlv.Fields{tlv.Uint16(tlv.SARMsgRefNum, 1), {Tag: tlv.SARTotalSegments, Value: []byte{2}}, {Tag: tlv.SARSegmentSeqnum, Value: []byte{1}}}
	for _, v := range []pdu.Version{pdu.V34, pdu.V50} {
		c := newCodec(t, v)
		if _, err := c.Append(nil, pdu.PDU{Sequence: 1, Body: &pdu.DataSM{ESMClass: 0x40, Optional: optional}}); !errors.Is(err, codec.ErrValidation) {
			t.Errorf("version %x accepted mixed UDH/SAR: %v", v, err)
		}
		// data_sm mandatory fields occupy ten octets; esm_class is offset 23.
		b := unhex(t, "0000002a 00000103 00000000 00000001 00000000000000 40 00 00 020c00020001 020e000102 020f000101")
		if _, err := c.Decode(b); !errors.Is(err, codec.ErrValidation) {
			t.Errorf("version %x decoded mixed UDH/SAR: %v", v, err)
		}
	}
}

func BenchmarkReviewRejectOversizedFrame(b *testing.B) {
	c, err := codec.New(codec.Config{Version: pdu.V50, MaxFrame: 1024})
	if err != nil {
		b.Fatal(err)
	}
	payload := bytes.Repeat([]byte{1}, 65535)
	fields := make(tlv.Fields, 64)
	for i := range fields {
		fields[i] = tlv.Field{Tag: 0x1500, Value: payload}
	}
	packet := pdu.PDU{Sequence: 1, Body: &pdu.SubmitSM{SMFields: pdu.SMFields{Optional: fields}}}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if _, err = c.Append(nil, packet); err == nil {
			b.Fatal("oversized frame accepted")
		}
	}
}

func TestReviewPortTLVValidation(t *testing.T) {
	for _, version := range []pdu.Version{pdu.V34, pdu.V50} {
		c := newCodec(t, version)
		// Independent data_sm with source_port=10 and destination_port=100.
		raw := unhex(t, "00000026 00000103 00000000 00000001 00000000000000000000 020a0002000a 020b00020064")
		if _, err := c.Decode(raw); err != nil {
			t.Fatal(err)
		}
		raw[23] = 0x40
		if _, err := c.Decode(raw); err == nil {
			t.Fatal("UDHI with port TLVs accepted")
		}
		for _, fields := range []tlv.Fields{
			{{Tag: tlv.SourcePort, Value: []byte{1}}},
			{tlv.Uint16(tlv.DestinationPort, 1), tlv.Uint16(tlv.DestinationPort, 2)},
		} {
			if _, err := c.Append(nil, pdu.PDU{Sequence: 1, Body: &pdu.DataSM{Optional: fields}}); err == nil {
				t.Fatal("invalid ports accepted")
			}
		}
	}
}

func TestReviewOversizedOpaqueLeavesDestinationUntouched(t *testing.T) {
	c, err := codec.New(codec.Config{Version: pdu.V50, MaxFrame: 32})
	if err != nil {
		t.Fatal(err)
	}
	dst := make([]byte, 3, 128)
	copy(dst, "abc")
	before := bytes.Clone(dst[:cap(dst)])
	out, err := c.Append(dst, pdu.PDU{Sequence: 1, Body: &pdu.Opaque{ID: 0x10200, Data: make([]byte, 65535)}})
	if !errors.Is(err, codec.ErrValidation) || len(out) != len(dst) || !bytes.Equal(dst[:cap(dst)], before) {
		t.Fatalf("error=%v destination modified", err)
	}
}
