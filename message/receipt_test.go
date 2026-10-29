// SPDX-AI-Disclosure: ai-generated
// SPDX-AI-Model: GPT-6.0 Astra
// SPDX-AI-Provider: OpenAI
// SPDX-AI-Scope: Test code generated and revised with Codex under maintainer direction.
// SPDX-AI-Date: 2026-10-29

package message_test

import (
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/skill215/smppkit/codec"
	"github.com/skill215/smppkit/message"
	"github.com/skill215/smppkit/pdu"
	"github.com/skill215/smppkit/tlv"
)

// Independently authored complete frames. No production encoder or receipt
// constructor generates these expectations. Empty addresses are legal here.
const receiptTextWire = "00000035 00000005 00000000 00000007 00000000000000 04000000000000010014 69643a3030416220737461743a44454c49565244"
const receipt33Wire = "00000035 00000005 00000000 00000007 00000000000000 04000000000000000014 69643a3030416220737461743a44454c49565244"
const receiptTLVWire = "00000036 00000005 00000000 00000007 00000000000000 04000000000000000000 001e00053030416200 0427000102 04230003031234"
const receiptMixedWire = "00000043 00000005 00000000 00000007 00000000000000 04000000000000010014 69643a3030416220737461743a44454c49565244 001e00053030416200 0427000102"
const receiptDataWire = "00000028 00000103 00000000 00000007 00000000000000 200000 001e00053030416200 0427000101"
const receiptDataTextWire = "00000032 00000103 00000000 00000007 00000000000000 040001 04240014 69643a3030416220737461743a44454c49565244"
const fullReceiptText = "id:00000A1 sub:001 dlvrd:000 submit date:2610291100 done date:2610291101 stat:UNDELIV err:0AF text:hi"

func receiptBytes(t testing.TB, s string) []byte {
	t.Helper()
	b, e := hex.DecodeString(strings.Join(strings.Fields(s), ""))
	if e != nil {
		t.Fatal(e)
	}
	return b
}
func receiptCodec(t testing.TB, v pdu.Version) *codec.Codec {
	t.Helper()
	c, e := codec.New(codec.Config{Version: v})
	if e != nil {
		t.Fatal(e)
	}
	return c
}
func receiptOptions(v pdu.Version) message.ReceiptParseOptions {
	return message.ReceiptParseOptions{Version: v, Dialect: message.ReceiptKeyValue}
}
func receiptBody(s string) *pdu.DeliverSM {
	return &pdu.DeliverSM{SMFields: pdu.SMFields{ESMClass: 4, DataCoding: 1, ShortMessage: []byte(s)}}
}
func receiptHas(r message.Receipt, issue message.ReceiptIssue, field string) bool {
	for _, d := range r.Diagnostics {
		if d.Issue == issue && d.Field == field {
			return true
		}
	}
	return false
}

func TestReceiptIndependentFrames(t *testing.T) {
	for _, tc := range []struct {
		name, wire string
		v          pdu.Version
		format     message.ReceiptFormat
		command    pdu.CommandID
		state      byte
		kind       message.ReceiptKind
		network    *message.ReceiptNetworkError
	}{
		{"3.3 text", receipt33Wire, pdu.V33, message.ReceiptTextOnly, pdu.DeliverSMID, 2, message.MCReceipt, nil},
		{"3.4 text", receiptTextWire, pdu.V34, message.ReceiptTextOnly, pdu.DeliverSMID, 2, message.MCReceipt, nil},
		{"5.0 text", receiptTextWire, pdu.V50, message.ReceiptTextOnly, pdu.DeliverSMID, 2, message.MCReceipt, nil},
		{"3.4 TLV", receiptTLVWire, pdu.V34, message.ReceiptTLVOnly, pdu.DeliverSMID, 2, message.MCReceipt, &message.ReceiptNetworkError{Type: 3, Code: 0x1234}},
		{"5.0 TLV", receiptTLVWire, pdu.V50, message.ReceiptTLVOnly, pdu.DeliverSMID, 2, message.MCReceipt, &message.ReceiptNetworkError{Type: 3, Code: 0x1234}},
		{"3.4 mixed", receiptMixedWire, pdu.V34, message.ReceiptTextAndTLV, pdu.DeliverSMID, 2, message.MCReceipt, nil},
		{"5.0 mixed", receiptMixedWire, pdu.V50, message.ReceiptTextAndTLV, pdu.DeliverSMID, 2, message.MCReceipt, nil},
		{"3.4 intermediate", receiptDataWire, pdu.V34, message.ReceiptTLVOnly, pdu.DataSMID, 1, message.IntermediateNotification, nil},
		{"5.0 intermediate", receiptDataWire, pdu.V50, message.ReceiptTLVOnly, pdu.DataSMID, 1, message.IntermediateNotification, nil},
		{"data text", receiptDataTextWire, pdu.V50, message.ReceiptTextOnly, pdu.DataSMID, 2, message.MCReceipt, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := receiptCodec(t, tc.v)
			wire := receiptBytes(t, tc.wire)
			packet, e := c.Decode(wire)
			if e != nil {
				t.Fatal(e)
			}
			r, e := message.ParseReceipt(packet.Body, receiptOptions(tc.v))
			if e != nil {
				t.Fatal(e)
			}
			if r.Kind != tc.kind {
				t.Fatal(r.Kind)
			}
			if tc.format != message.ReceiptTLVOnly {
				id, ok := r.Texts[0].Value("id")
				if !ok || id != "00Ab" {
					t.Fatal(r)
				}
			}
			if tc.format != message.ReceiptTextOnly {
				if !r.TLV.HasMessageID || r.TLV.MessageID != "00Ab" || !r.TLV.HasState || r.TLV.State != tc.state || !reflect.DeepEqual(r.TLV.NetworkError, tc.network) {
					t.Fatal(r)
				}
			}
			body, e := message.BuildReceipt(message.ReceiptSpec{Kind: tc.kind, MessageID: "00Ab", State: tc.state, NetworkError: tc.network}, message.ReceiptBuildOptions{Version: tc.v, Command: tc.command, Format: tc.format})
			if e != nil {
				t.Fatal(e)
			}
			got, e := c.Append(nil, pdu.PDU{Sequence: 7, Body: body})
			if e != nil || !bytes.Equal(got, wire) {
				t.Fatalf("wire=%x expected=%x err=%v", got, wire, e)
			}
		})
	}
}

func TestReceiptTextEvidenceAndDialects(t *testing.T) {
	r, e := message.ParseReceipt(receiptBody(fullReceiptText), receiptOptions(pdu.V34))
	if e != nil || len(r.Diagnostics) != 0 {
		t.Fatal(r, e)
	}
	for key, want := range map[string]string{"id": "00000A1", "sub": "001", "dlvrd": "000", "submit date": "2610291100", "done date": "2610291101", "stat": "UNDELIV", "err": "0AF", "text": "hi"} {
		if got, ok := r.Texts[0].Value(key); !ok || got != want {
			t.Fatal(key, got, ok)
		}
	}
	// Text is terminal: arbitrary field-looking content here is not another ID.
	r, e = message.ParseReceipt(receiptBody("stat:DELIVRD id:00000A1 Text:id:secret"), receiptOptions(pdu.V34))
	if e != nil || receiptHas(r, message.ReceiptDuplicate, "id") {
		t.Fatal(r, e)
	}
	if got, ok := r.Texts[0].Value("text"); !ok || got != "id:secret" {
		t.Fatal(got, ok)
	}
	if !receiptHas(r, message.ReceiptMissing, "sub") || !receiptHas(r, message.ReceiptMissing, "dlvrd") {
		t.Fatal("missing counts were synthesized")
	}
	r, e = message.ParseReceipt(receiptBody(fullReceiptText), message.ReceiptParseOptions{Version: pdu.V34})
	if e != nil || len(r.Texts[0].Fields) != 0 || string(r.Texts[0].Raw) != fullReceiptText {
		t.Fatal(r, e)
	}
	for _, body := range []pdu.Body{&pdu.SubmitSM{}, &pdu.DeliverSM{SMFields: pdu.SMFields{ShortMessage: []byte(fullReceiptText), Optional: tlv.Fields{{Tag: tlv.ReceiptedMessageID, Value: []byte("id\x00")}}}}} {
		if _, e = message.ParseReceipt(body, receiptOptions(pdu.V50)); !errors.Is(e, message.ErrNotReceipt) {
			t.Fatal(e)
		}
	}
}

func TestReceiptConflictsDuplicatesAndOwnership(t *testing.T) {
	body := receiptBody(fullReceiptText)
	body.Optional = tlv.Fields{{Tag: tlv.ReceiptedMessageID, Value: []byte("different\x00")}, {Tag: tlv.MessageState, Value: []byte{2}}, {Tag: tlv.NetworkErrorCode, Value: []byte{3, 0, 1}}, {Tag: 0x1400, Value: []byte{7}}}
	r, e := message.ParseReceipt(body, receiptOptions(pdu.V50))
	if e != nil || !receiptHas(r, message.ReceiptConflict, "id") || !receiptHas(r, message.ReceiptConflict, "stat") {
		t.Fatal(r, e)
	}
	if receiptHas(r, message.ReceiptConflict, "err") || r.TLV.NetworkError.Code != 1 {
		t.Fatal("text err was conflated with network_error_code")
	}
	r.Texts[0].Raw[0] = 'X'
	r.Optional[0].Value[0] = 'X'
	r.Optional[3].Value[0] = 9
	if body.ShortMessage[0] != 'i' || body.Optional[0].Value[0] != 'd' || body.Optional[3].Value[0] != 7 {
		t.Fatal("parse aliases caller buffers")
	}
	body.Optional = append(body.Optional, tlv.Field{Tag: tlv.ReceiptedMessageID, Value: []byte("second\x00")}, tlv.Field{Tag: tlv.MessageState, Value: []byte{5}})
	r, e = message.ParseReceipt(body, receiptOptions(pdu.V50))
	if e != nil || r.TLV.HasMessageID || r.TLV.HasState || !receiptHas(r, message.ReceiptDuplicate, "id") || !receiptHas(r, message.ReceiptDuplicate, "stat") {
		t.Fatal(r, e)
	}
	body = receiptBody("id:first id:second stat:DELIVRD")
	r, e = message.ParseReceipt(body, receiptOptions(pdu.V34))
	if e != nil || !receiptHas(r, message.ReceiptDuplicate, "id") {
		t.Fatal(r, e)
	}
	if _, ok := r.Texts[0].Value("id"); ok {
		t.Fatal("duplicate ID got precedence")
	}
	if len(r.Texts[0].Fields) != 3 || r.Texts[0].Fields[1].Value != "second" {
		t.Fatal("duplicate evidence lost")
	}
	body = receiptBody("id:a stat:DELIVRD")
	body.Optional = tlv.Fields{{Tag: tlv.MessagePayload, Value: []byte("id:b stat:UNDELIV")}}
	r, e = message.ParseReceipt(body, receiptOptions(pdu.V34))
	if e != nil || len(r.Texts) != 2 || !receiptHas(r, message.ReceiptConflict, "payload") || !receiptHas(r, message.ReceiptConflict, "id") || !receiptHas(r, message.ReceiptConflict, "stat") {
		t.Fatal(r, e)
	}
	r.Texts[1].Raw[0] = 'X'
	if r.Optional[0].Value[0] != 'i' {
		t.Fatal("raw carriers alias")
	}
	body.Optional = append(body.Optional, body.Optional[0])
	r, e = message.ParseReceipt(body, receiptOptions(pdu.V34))
	if e != nil || len(r.Texts) != 1 || !receiptHas(r, message.ReceiptDuplicate, "message_payload") {
		t.Fatal(r, e)
	}
}

func TestReceiptMalformedUnknownAndSafeDiagnostics(t *testing.T) {
	for _, tc := range []struct {
		input, field string
		issue        message.ReceiptIssue
	}{
		{"id: stat:DELIVRD", "id", message.ReceiptMalformed},
		{"id:a stat:NEWSTATE", "stat", message.ReceiptUnsupported},
		{"id:a sub:1 stat:DELIVRD", "sub", message.ReceiptMalformed},
		{"id:a submit date:202610291100 stat:DELIVRD", "submit date", message.ReceiptMalformed},
		{"id:a done date:2613322560 stat:DELIVRD", "done date", message.ReceiptMalformed},
		{"id:a done date:2604311200 stat:DELIVRD", "done date", message.ReceiptMalformed},
		{"id:a done date:2602291200 stat:DELIVRD", "done date", message.ReceiptMalformed},
		{"private-secret:foo id:a stat:DELIVRD", "syntax", message.ReceiptUnsupported},
		{"id:a stat:DELIVRD text:" + strings.Repeat("a", 21), "text", message.ReceiptMalformed},
	} {
		t.Run(tc.field+tc.input[:min(4, len(tc.input))], func(t *testing.T) {
			r, e := message.ParseReceipt(receiptBody(tc.input), receiptOptions(pdu.V34))
			if e != nil || !receiptHas(r, tc.issue, tc.field) {
				t.Fatal(r, e)
			}
			if strings.Contains(fmt.Sprint(r.Diagnostics), "private-secret") {
				t.Fatal("diagnostics leaked untrusted text")
			}
			if string(r.Texts[0].Raw) != tc.input {
				t.Fatal("raw evidence changed")
			}
		})
	}
	for _, fields := range []tlv.Fields{
		{{Tag: tlv.ReceiptedMessageID, Value: []byte("no-null")}, {Tag: tlv.MessageState, Value: []byte{2, 3}}, {Tag: tlv.NetworkErrorCode, Value: []byte{3, 1}}},
		{{Tag: tlv.ReceiptedMessageID, Value: []byte{'a', 0, 'b', 0}}, {Tag: tlv.MessageState, Value: nil}, {Tag: tlv.NetworkErrorCode, Value: nil}},
	} {
		r, e := message.ParseReceipt(&pdu.DataSM{ESMClass: 4, Optional: fields}, receiptOptions(pdu.V50))
		if e != nil || r.TLV.HasMessageID || r.TLV.HasState || r.TLV.NetworkError != nil || !receiptHas(r, message.ReceiptMalformed, "id") {
			t.Fatal(r, e)
		}
	}
	r, e := message.ParseReceipt(&pdu.DataSM{ESMClass: 0x20, Optional: tlv.Fields{{Tag: tlv.MessageState, Value: []byte{255}}, {Tag: tlv.NetworkErrorCode, Value: []byte{255, 1, 2}}}}, receiptOptions(pdu.V50))
	if e != nil || !r.TLV.HasState || r.TLV.State != 255 || r.TLV.NetworkError.Code != 258 || !receiptHas(r, message.ReceiptUnsupported, "stat") {
		t.Fatal(r, e)
	}
}

func TestReceiptEncodingVersionAndHardBounds(t *testing.T) {
	for _, dcs := range []byte{2, 0xf6} {
		b := receiptBody("id:a stat:DELIVRD")
		b.DataCoding = dcs
		r, e := message.ParseReceipt(b, receiptOptions(pdu.V50))
		if e != nil || len(r.Texts[0].Fields) != 0 || !receiptHas(r, message.ReceiptUnsupported, "data_coding") {
			t.Fatal(r, e)
		}
	}
	b := receiptBody("id:a stat:DELIVRD")
	b.ESMClass |= 0x40
	r, e := message.ParseReceipt(b, receiptOptions(pdu.V34))
	if e != nil || !receiptHas(r, message.ReceiptUnsupported, "udh") {
		t.Fatal(r, e)
	}
	b = receiptBody("id:a stat:DELIVRD")
	b.DataCoding = 0
	b.ShortMessage = []byte{0x1b}
	r, e = message.ParseReceipt(b, receiptOptions(pdu.V34))
	if e != nil || !receiptHas(r, message.ReceiptMalformed, "payload") {
		t.Fatal(r, e)
	}
	b = receiptBody("")
	b.Optional = tlv.Fields{{Tag: tlv.MessageState, Value: []byte{2}}, {Tag: tlv.MessagePayload, Value: []byte("id:a stat:DELIVRD")}}
	r, e = message.ParseReceipt(b, receiptOptions(pdu.V33))
	if e != nil || r.TLV.HasState || len(r.Texts) != 0 || !receiptHas(r, message.ReceiptUnsupported, "tlv") {
		t.Fatal(r, e)
	}
	for _, body := range []pdu.Body{(*pdu.DeliverSM)(nil), (*pdu.DataSM)(nil), &pdu.DataSM{ESMClass: 4}} {
		if _, e := message.ParseReceipt(body, receiptOptions(pdu.V33)); !errors.Is(e, message.ErrMetadata) {
			t.Fatal(e)
		}
	}
	if _, e := message.ParseReceipt(receiptBody("id:a"), message.ReceiptParseOptions{}); !errors.Is(e, message.ErrMetadata) {
		t.Fatal(e)
	}
	for _, body := range []*pdu.DeliverSM{
		receiptBody(strings.Repeat("a", 65537)),
		{SMFields: pdu.SMFields{ESMClass: 4, Optional: make(tlv.Fields, 257)}},
		{SMFields: pdu.SMFields{ESMClass: 4, Optional: tlv.Fields{{Tag: 0x1400, Value: make([]byte, 65533)}}}},
		receiptBody(strings.Repeat("id:a ", 33)),
		receiptBody(strings.Repeat("unknown ", 33)),
	} {
		if r, e := message.ParseReceipt(body, receiptOptions(pdu.V50)); !errors.Is(e, message.ErrCapacity) || len(r.Texts) != 0 || len(r.Optional) != 0 {
			t.Fatal(r, e)
		}
	}
	b = receiptBody(strings.Repeat("a", 65536))
	if _, e = message.ParseReceipt(b, message.ReceiptParseOptions{Version: pdu.V50}); e != nil {
		t.Fatal(e)
	}
	b.Optional = tlv.Fields{{Tag: 0x1400}}
	if _, e = message.ParseReceipt(b, receiptOptions(pdu.V50)); !errors.Is(e, message.ErrCapacity) {
		t.Fatal(e)
	}
}

func TestReceiptBuildBoundariesAndOwnership(t *testing.T) {
	base := message.ReceiptSpec{Kind: message.MCReceipt, MessageID: "00Ab", State: 2}
	options := message.ReceiptBuildOptions{Version: pdu.V50, Command: pdu.DeliverSMID, Format: message.ReceiptTextAndTLV}
	for _, change := range []func(*message.ReceiptSpec){
		func(s *message.ReceiptSpec) { s.Kind = 0 }, func(s *message.ReceiptSpec) { s.MessageID = "" }, func(s *message.ReceiptSpec) { s.MessageID = strings.Repeat("a", 65) },
		func(s *message.ReceiptSpec) { s.MessageID = "a stat:DELIVRD" }, func(s *message.ReceiptSpec) { s.State = 255 }, func(s *message.ReceiptSpec) { s.State = 9 },
		func(s *message.ReceiptSpec) { s.Text = &message.ReceiptTextSpec{Submitted: "1"} }, func(s *message.ReceiptSpec) { s.Text = &message.ReceiptTextSpec{DoneDate: "2613322560"} },
		func(s *message.ReceiptSpec) {
			v := strings.Repeat("x", 21)
			s.Text = &message.ReceiptTextSpec{Text: &v}
		}, func(s *message.ReceiptSpec) { s.NetworkError = &message.ReceiptNetworkError{Type: 0} },
		func(s *message.ReceiptSpec) { s.Source = pdu.Address{Value: strings.Repeat("a", 65)} }, func(s *message.ReceiptSpec) { s.Text = &message.ReceiptTextSpec{Error: "0\nBAD"} },
	} {
		spec := base
		change(&spec)
		if b, e := message.BuildReceipt(spec, options); e == nil || b != nil {
			t.Fatal(b, e)
		}
	}
	for _, o := range []message.ReceiptBuildOptions{{}, {Version: pdu.V33, Command: pdu.DataSMID, Format: message.ReceiptTextOnly}, {Version: pdu.V33, Command: pdu.DeliverSMID, Format: message.ReceiptTLVOnly}, {Version: pdu.V50, Command: pdu.SubmitSMID, Format: message.ReceiptTLVOnly}} {
		if b, e := message.BuildReceipt(base, o); e == nil || b != nil {
			t.Fatal(b, e)
		}
	}
	text := ""
	base.Text = &message.ReceiptTextSpec{Submitted: "001", Delivered: "000", SubmitDate: "2610291100", DoneDate: "2610291101", Error: "0AF", Text: &text}
	base.NetworkError = &message.ReceiptNetworkError{Type: 3, Code: 0x1234}
	b, e := message.BuildReceipt(base, options)
	if e != nil {
		t.Fatal(e)
	}
	d := b.(*pdu.DeliverSM)
	if string(d.ShortMessage) != "id:00Ab sub:001 dlvrd:000 submit date:2610291100 done date:2610291101 stat:DELIVRD err:0AF text:" {
		t.Fatalf("text %q", d.ShortMessage)
	}
	text = "changed"
	base.NetworkError.Code = 9
	if !strings.HasSuffix(string(d.ShortMessage), "text:") || d.Optional[2].Value[2] != 0x34 {
		t.Fatal("builder borrowed caller input")
	}
	// No silent discard of caller-provided evidence by incompatible formats.
	options.Format = message.ReceiptTLVOnly
	if _, e := message.BuildReceipt(base, options); e == nil {
		t.Fatal("text silently discarded")
	}
	base.Text = nil
	options.Format = message.ReceiptTextOnly
	if _, e := message.BuildReceipt(base, options); e == nil {
		t.Fatal("network error silently discarded")
	}
	// 5.0-only states are supported through standard TLVs, not invented labels.
	for _, state := range []byte{0, 9} {
		base.State = state
		options.Format = message.ReceiptTLVOnly
		if _, e := message.BuildReceipt(base, options); e != nil {
			t.Fatal(e)
		}
		options.Version = pdu.V34
		if _, e := message.BuildReceipt(base, options); e == nil {
			t.Fatal("5.0 state accepted for 3.4")
		}
		options.Version = pdu.V50
	}
	// Legacy DCS 0 is really GSM7, including characters whose code differs from ASCII.
	base = message.ReceiptSpec{Kind: message.MCReceipt, MessageID: "a_b", State: 2}
	options = message.ReceiptBuildOptions{Version: pdu.V33, Command: pdu.DeliverSMID, Format: message.ReceiptTextOnly}
	b, e = message.BuildReceipt(base, options)
	if e != nil {
		t.Fatal(e)
	}
	if !bytes.HasPrefix(b.(*pdu.DeliverSM).ShortMessage, []byte{'i', 'd', ':', 'a', 0x11, 'b'}) {
		t.Fatal("legacy GSM7 encoding lost")
	}
	r, e := message.ParseReceipt(b, receiptOptions(pdu.V33))
	if e != nil {
		t.Fatal(e)
	}
	if id, ok := r.Texts[0].Value("id"); !ok || id != "a_b" {
		t.Fatal(id, ok)
	}
}

func TestReceiptConcurrentHelpers(t *testing.T) {
	body := receiptBody(fullReceiptText)
	spec := message.ReceiptSpec{Kind: message.MCReceipt, MessageID: "id", State: 2}
	var wg sync.WaitGroup
	for range 16 {
		wg.Go(func() {
			for range 20 {
				if _, e := message.ParseReceipt(body, receiptOptions(pdu.V34)); e != nil {
					t.Error(e)
				}
				if _, e := message.BuildReceipt(spec, message.ReceiptBuildOptions{Version: pdu.V50, Command: pdu.DataSMID, Format: message.ReceiptTextAndTLV}); e != nil {
					t.Error(e)
				}
			}
		})
	}
	wg.Wait()
	if string(body.ShortMessage) != fullReceiptText {
		t.Fatal("shared input changed")
	}
}

func TestReceiptUCS2AndExactLimits(t *testing.T) {
	// Literal UCS2 "id:a stat:DELIVRD", authored without a text encoder.
	raw := receiptBytes(t, "0069 0064 003a 0061 0020 0073 0074 0061 0074 003a 0044 0045 004c 0049 0056 0052 0044")
	for _, v := range []pdu.Version{pdu.V33, pdu.V34, pdu.V50} {
		b := receiptBody("")
		b.DataCoding = 8
		b.ShortMessage = raw
		r, e := message.ParseReceipt(b, receiptOptions(v))
		if e != nil {
			t.Fatal(e)
		}
		if id, ok := r.Texts[0].Value("id"); !ok || id != "a" {
			t.Fatal(r)
		}
		b.ShortMessage = raw[:len(raw)-1]
		r, e = message.ParseReceipt(b, receiptOptions(v))
		if e != nil || !receiptHas(r, message.ReceiptMalformed, "payload") {
			t.Fatal(r, e)
		}
	}
	for _, b := range []*pdu.DeliverSM{
		receiptBody(strings.Repeat("id:a ", 32)),
		{SMFields: pdu.SMFields{ESMClass: 4, Optional: make(tlv.Fields, 256)}},
		{SMFields: pdu.SMFields{ESMClass: 4, Optional: tlv.Fields{{Tag: 0x1400, Value: make([]byte, 65532)}}}},
	} {
		if _, e := message.ParseReceipt(b, receiptOptions(pdu.V50)); e != nil {
			t.Fatal(e)
		}
	}
	for _, state := range []byte{0, 9} {
		b := &pdu.DataSM{ESMClass: 0x20, Optional: tlv.Fields{{Tag: tlv.MessageState, Value: []byte{state}}, {Tag: tlv.NetworkErrorCode, Value: []byte{8, 1, 2}}}}
		r, e := message.ParseReceipt(b, receiptOptions(pdu.V34))
		if e != nil || !receiptHas(r, message.ReceiptUnsupported, "stat") || !receiptHas(r, message.ReceiptUnsupported, "network_error_code") {
			t.Fatal(r, e)
		}
		r, e = message.ParseReceipt(b, receiptOptions(pdu.V50))
		if e != nil || receiptHas(r, message.ReceiptUnsupported, "stat") || receiptHas(r, message.ReceiptUnsupported, "network_error_code") {
			t.Fatal(r, e)
		}
	}
	text := strings.Repeat("x", 20)
	spec := message.ReceiptSpec{Kind: message.MCReceipt, MessageID: strings.Repeat("a", 64), State: 2, Text: &message.ReceiptTextSpec{Submitted: "001", Delivered: "001", SubmitDate: "2610291100", DoneDate: "2610291101", Error: strings.Repeat("e", 32), Text: &text}}
	if b, e := message.BuildReceipt(spec, message.ReceiptBuildOptions{Version: pdu.V33, Command: pdu.DeliverSMID, Format: message.ReceiptTextOnly}); !errors.Is(e, codec.ErrValidation) || b != nil {
		t.Fatal("oversized 3.3 text must fail without truncation", b, e)
	}
	if _, e := message.BuildReceipt(spec, message.ReceiptBuildOptions{Version: pdu.V50, Command: pdu.DeliverSMID, Format: message.ReceiptTextAndTLV}); e != nil {
		t.Fatal(e)
	}
	// TLV identities follow C-octet ASCII rules, not the narrower text grammar.
	// Do not impose printable-token restrictions on an opaque protocol ID.
	spec = message.ReceiptSpec{Kind: message.MCReceipt, MessageID: "\x01opaque id:00", State: 2}
	built, err := message.BuildReceipt(spec, message.ReceiptBuildOptions{Version: pdu.V50, Command: pdu.DeliverSMID, Format: message.ReceiptTLVOnly})
	if err != nil {
		t.Fatal(err)
	}
	r, err := message.ParseReceipt(built, receiptOptions(pdu.V50))
	if err != nil || !r.TLV.HasMessageID || r.TLV.MessageID != spec.MessageID {
		t.Fatal(r, err)
	}
}
