// SPDX-AI-Disclosure: ai-generated
// SPDX-AI-Model: GPT-6.0 Astra
// SPDX-AI-Provider: OpenAI
// SPDX-AI-Scope: Test code generated and revised with Codex under maintainer direction.
// SPDX-AI-Date: 2026-10-28

package message_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/skill215/smppkit/message"
	"github.com/skill215/smppkit/pdu"
	"github.com/skill215/smppkit/textcodec"
	"github.com/skill215/smppkit/tlv"
)

func TestReviewSARPortNamespacesDoNotMix(t *testing.T) {
	r := receiver(t, message.ReceiveConfig{})
	now := time.Now()
	segment := func(port uint16, number byte, text string) pdu.SMFields {
		return pdu.SMFields{Source: pdu.Address{Value: "source"}, Destination: pdu.Address{Value: "dest"}, ShortMessage: []byte(text), Optional: tlv.Fields{
			tlv.Uint16(tlv.SARMsgRefNum, 1), {Tag: tlv.SARTotalSegments, Value: []byte{2}}, {Tag: tlv.SARSegmentSeqnum, Value: []byte{number}}, tlv.Uint16(0x020a, 10), tlv.Uint16(0x020b, port),
		}}
	}
	if _, err := r.Admit("ns", segment(100, 1, "A1"), now); err != nil {
		t.Fatal(err)
	}
	admission, err := r.Admit("ns", segment(200, 2, "B2"), now)
	if err != nil || admission.Complete {
		t.Fatalf("different application ports merged: %+v %v", admission, err)
	}
	if _, err = r.Admit("ns", segment(100, 2, "A2"), now); err != nil {
		t.Fatal(err)
	}
	a, err := r.Receive(context.Background())
	if err != nil || string(a.Payload) != "A1A2" {
		t.Fatalf("A: %q %v", a.Payload, err)
	}
	if _, err = r.Admit("ns", segment(200, 1, "B1"), now); err != nil {
		t.Fatal(err)
	}
	b, err := r.Receive(context.Background())
	if err != nil || string(b.Payload) != "B1B2" {
		t.Fatalf("B: %q %v", b.Payload, err)
	}
}

type mutableEncoding struct {
	textcodec.Alphabet
	name string
	dcs  byte
}

func (e *mutableEncoding) Name() string     { return e.name }
func (e *mutableEncoding) DataCoding() byte { return e.dcs }
func TestReviewPreparedPlanFreezesEncodingMetadata(t *testing.T) {
	enc := &mutableEncoding{Alphabet: textcodec.Latin1, name: "original", dcs: 3}
	plan, err := message.Prepare(pdu.V50, message.Text{Text: "hello"}, message.Policy{Encoding: enc})
	if err != nil {
		t.Fatal(err)
	}
	enc.name = "changed"
	enc.dcs = 8
	if plan.Encoding() != "original" || plan.DataCoding() != 3 {
		t.Fatalf("plan changed after preparation: %s %d", plan.Encoding(), plan.DataCoding())
	}
	parts, err := plan.Materialize(0)
	if err != nil || parts[0].DataCoding != plan.DataCoding() {
		t.Fatal("preview and wire coding differ", err)
	}
}

func TestReviewParseRejectsUnrepresentablePayloadLength(t *testing.T) {
	_, err := message.ParseSegment(pdu.SMFields{Optional: tlv.Fields{{Tag: tlv.MessagePayload, Value: make([]byte, 65536)}}})
	if err == nil {
		t.Fatal("accepted payload that cannot be represented by a TLV length")
	}
}

func TestReviewSARReservesRadioHeaderBudget(t *testing.T) {
	for _, tc := range []struct {
		name               string
		ports              bool
		size, count, first int
	}{
		{"single", false, 160, 1, 160},
		{"concat", false, 305, 3, 152},
		{"port single", true, 152, 1, 152},
		{"port concat", true, 291, 3, 145},
	} {
		t.Run(tc.name, func(t *testing.T) {
			msg := message.Text{Text: strings.Repeat("A", tc.size)}
			if tc.ports {
				msg.Optional = tlv.Fields{tlv.Uint16(tlv.SourcePort, 1), tlv.Uint16(tlv.DestinationPort, 2)}
			}
			plan, err := message.Prepare(pdu.V50, msg, message.Policy{Encoding: textcodec.GSM7, Mode: message.SAR})
			if err != nil {
				t.Fatal(err)
			}
			if plan.Count() != tc.count || len(plan.Preview()[0]) != tc.first {
				t.Fatalf("count=%d first=%d", plan.Count(), len(plan.Preview()[0]))
			}
			parts, err := plan.Materialize(123)
			if err != nil {
				t.Fatal(err)
			}
			for _, part := range parts {
				if part.ESMClass&0x40 != 0 {
					t.Fatal("radio overhead must not add UDH to the SMPP SAR payload")
				}
			}
		})
	}
}

func TestReviewPortMetadataBoundsAndCanonicalOrder(t *testing.T) {
	for _, fields := range []tlv.Fields{
		{{Tag: tlv.SourcePort, Value: []byte{1}}},
		{tlv.Uint16(tlv.SourcePort, 1), tlv.Uint16(tlv.SourcePort, 2)},
	} {
		if _, err := message.ParseSegment(pdu.SMFields{Optional: fields}); err == nil {
			t.Fatal("invalid port metadata accepted")
		}
	}
	a, err := message.ParseSegment(pdu.SMFields{Optional: tlv.Fields{tlv.Uint16(tlv.SourcePort, 0), tlv.Uint16(tlv.DestinationPort, 1)}})
	if err != nil {
		t.Fatal(err)
	}
	b, err := message.ParseSegment(pdu.SMFields{Optional: tlv.Fields{tlv.Uint16(tlv.DestinationPort, 1), tlv.Uint16(tlv.SourcePort, 0)}})
	if err != nil || string(a.Ports) != string(b.Ports) {
		t.Fatal("port order changed identity", err)
	}
	missing, err := message.ParseSegment(pdu.SMFields{Optional: tlv.Fields{tlv.Uint16(tlv.DestinationPort, 1)}})
	if err != nil || string(a.Ports) == string(missing.Ports) {
		t.Fatal("missing source port aliases zero port", err)
	}
	if _, err := message.ParseSegment(pdu.SMFields{ESMClass: 0x40, Optional: a.Fields.Optional}); err == nil {
		t.Fatal("UDHI and port TLVs accepted")
	}
}

func TestPrepareRejectsInvalidAdditionalPortUDH(t *testing.T) {
	for _, elements := range [][]message.InformationElement{
		{{ID: 4, Data: []byte{1}}},
		{{ID: 5, Data: []byte{0, 1, 2}}},
		{{ID: 4, Data: []byte{1, 2}}, {ID: 4, Data: []byte{3, 4}}},
		{{ID: 4, Data: []byte{1, 2}}, {ID: 5, Data: []byte{0, 3, 0, 4}}},
	} {
		policy := message.GSMText()
		policy.AdditionalUDH = elements
		if _, err := message.Prepare(pdu.V50, message.Text{Text: "hello"}, policy); !errors.Is(err, message.ErrMetadata) {
			t.Fatalf("invalid port UDH accepted: %v", err)
		}
	}
}

func TestAdditionalUDHExactBytesAndOwnership(t *testing.T) {
	for _, tc := range []struct {
		id           byte
		data, header []byte
	}{
		{4, []byte{7, 8}, []byte{4, 4, 2, 7, 8}},
		{0x70, []byte{0xaa, 0xbb}, []byte{4, 0x70, 2, 0xaa, 0xbb}},
	} {
		policy := message.Policy{Encoding: textcodec.Latin1, Mode: message.UDH8, AdditionalUDH: []message.InformationElement{{ID: tc.id, Data: tc.data}}}
		plan, err := message.Prepare(pdu.V50, message.Text{Text: "hello"}, policy)
		if err != nil {
			t.Fatal(err)
		}
		tc.data[0] = 0xff // input ownership must end at Prepare return
		parts, err := plan.Materialize(0)
		if err != nil {
			t.Fatal(err)
		}
		want := append(bytes.Clone(tc.header), []byte("hello")...)
		if len(parts) != 1 || parts[0].ESMClass != 0x40 || !bytes.Equal(parts[0].ShortMessage, want) {
			t.Fatalf("wire payload %x, want %x", parts[0].ShortMessage, want)
		}
	}
}

func FuzzAdditionalUDH(f *testing.F) {
	f.Add(byte(4), []byte{7, 8})
	f.Add(byte(5), []byte{0, 1, 2})
	f.Add(byte(0x70), []byte{0xaa, 0xbb})
	f.Fuzz(func(t *testing.T, id byte, data []byte) {
		if len(data) > 300 {
			t.Skip()
		}
		policy := message.Policy{Encoding: textcodec.Latin1, AdditionalUDH: []message.InformationElement{{ID: id, Data: data}}}
		plan, err := message.Prepare(pdu.V50, message.Text{Text: "hello"}, policy)
		if err != nil {
			return
		}
		parts, err := plan.Materialize(1)
		if err != nil {
			t.Fatal(err)
		}
		for _, part := range parts {
			if len(part.ShortMessage) > 140 {
				t.Fatal("radio budget exceeded")
			}
			if _, err := message.ParseSegment(part); err != nil {
				t.Fatalf("prepared invalid UDH: %v", err)
			}
		}
	})
}
