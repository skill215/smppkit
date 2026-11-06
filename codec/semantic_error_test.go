// SPDX-AI-Disclosure: ai-generated
// SPDX-AI-Model: GPT-6.0 Astra
// SPDX-AI-Provider: OpenAI
// SPDX-AI-Scope: Test code generated and revised with Codex under maintainer direction.
// SPDX-AI-Date: 2026-11-06

package codec_test

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/skill215/smppkit/codec"
	"github.com/skill215/smppkit/pdu"
	"github.com/skill215/smppkit/tlv"
)

// Literal wire samples are independent of Append. The shortest submit_sm has
// 17 mandatory body octets; these samples change source TON and data_coding.
const diagnosticTONHex = "00000021 00000004 00000000 00000007 0007000000000000000000000000000000"
const diagnosticDCSHex = "00000021 00000004 00000000 00000007 00000000000000000000000000000b0000"
const diagnosticTLVHex = "00000024 00000103 00000000 00000007 00000000000000000000 0427000102 0427000102"

func wantDiagnostic(t testing.TB, err error, field string, rule codec.SemanticRule, index, tag int) *codec.SemanticError {
	t.Helper()
	e, ok := errors.AsType[*codec.SemanticError](err)
	if !ok || e.Field != field || e.Rule != rule || e.HasIndex != (index >= 0) || e.HasTag != (tag >= 0) || index >= 0 && e.Index != index || tag >= 0 && e.Tag != uint16(tag) {
		t.Fatalf("diagnostic=%+v err=%v; want field=%s rule=%s index=%d tag=%d", e, err, field, rule, index, tag)
	}
	return e
}

func wantDecodeKind(t testing.TB, err error, kind codec.DecodeErrorKind) {
	t.Helper()
	e, ok := errors.AsType[*codec.DecodeError](err)
	if !ok || e.Kind != kind {
		t.Fatalf("decode error=%v; want kind=%d", err, kind)
	}
}

func TestSemanticDiagnosticIndependentSM(t *testing.T) {
	// Offsets below are from the minimal submit_sm layout, not our serializer.
	for _, tc := range []struct {
		name, field string
		offset      int
		value       byte
		mutate      func(*pdu.SMFields)
	}{
		{"source-ton", "source_addr_ton", 1, 7, func(f *pdu.SMFields) { f.Source.TON = 7 }},
		{"source-npi", "source_addr_npi", 2, 2, func(f *pdu.SMFields) { f.Source.NPI = 2 }},
		{"dest-ton", "dest_addr_ton", 4, 7, func(f *pdu.SMFields) { f.Destination.TON = 7 }},
		{"dest-npi", "dest_addr_npi", 5, 2, func(f *pdu.SMFields) { f.Destination.NPI = 2 }},
		{"esm", "esm_class", 7, 0x3c, func(f *pdu.SMFields) { f.ESMClass = 0x3c }},
		{"priority", "priority_flag", 9, 4, func(f *pdu.SMFields) { f.PriorityFlag = 4 }},
		{"receipt", "registered_delivery", 12, 0x80, func(f *pdu.SMFields) { f.RegisteredDelivery = 0x80 }},
		{"replace", "replace_if_present_flag", 13, 2, func(f *pdu.SMFields) { f.ReplaceIfPresent = 2 }},
		{"coding", "data_coding", 14, 11, func(f *pdu.SMFields) { f.DataCoding = 11 }},
		{"default-id", "sm_default_msg_id", 15, 255, func(f *pdu.SMFields) { f.DefaultMessageID = 255 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := newCodec(t, pdu.V34)
			b := &pdu.SubmitSM{}
			tc.mutate(&b.SMFields)
			body := make([]byte, 17)
			body[tc.offset] = tc.value
			prefix := []byte{0xca, 0xfe}
			got, encodeErr := c.Append(prefix, pdu.PDU{Sequence: 7, Body: b})
			if !bytes.Equal(got, prefix) {
				t.Fatal("failed append modified destination")
			}
			_, decodeErr := c.Decode(semanticFrame(pdu.SubmitSMID, pdu.OK, body))
			for _, err := range []error{encodeErr, decodeErr} {
				wantDiagnostic(t, err, tc.field, codec.RuleValue, -1, -1)
				if !errors.Is(err, codec.ErrValidation) {
					t.Fatal("lost validation cause", err)
				}
			}
			wantDecodeKind(t, decodeErr, codec.DecodeSemantic)
		})
	}
	for _, tc := range []struct{ hex, field string }{{diagnosticTONHex, "source_addr_ton"}, {diagnosticDCSHex, "data_coding"}} {
		_, err := newCodec(t, pdu.V34).Decode(unhex(t, tc.hex))
		wantDiagnostic(t, err, tc.field, codec.RuleValue, -1, -1)
	}
}

func TestSemanticDiagnosticTLVs(t *testing.T) {
	for _, tc := range []struct {
		name       string
		v          pdu.Version
		optional   tlv.Fields
		wire       string
		rule       codec.SemanticRule
		index, tag int
	}{
		{"value", pdu.V50, tlv.Fields{{Tag: tlv.MessageState, Value: []byte{10}}}, "042700010a", codec.RuleValue, 0, 0x0427},
		{"length", pdu.V50, tlv.Fields{{Tag: tlv.MessageState, Value: []byte{1, 2}}}, "042700020102", codec.RuleLength, 0, 0x0427},
		{"duplicate", pdu.V50, tlv.Fields{{Tag: 0x1400}, {Tag: tlv.MessageState, Value: []byte{2}}, {Tag: tlv.MessageState, Value: []byte{2}}}, "14000000 0427000102 0427000102", codec.RuleCardinality, 2, 0x0427},
		{"version", pdu.V34, tlv.Fields{{Tag: tlv.BillingIdentification, Value: []byte{0x80}}}, "060b000180", codec.RuleVersion, 0, 0x060b},
		{"command", pdu.V50, tlv.Fields{{Tag: tlv.SCInterfaceVersion, Value: []byte{0x50}}}, "0210000150", codec.RuleCommand, 0, 0x0210},
		{"broadcast", pdu.V50, tlv.Fields{{Tag: 0x0600}}, "06000000", codec.RuleCommand, 0, 0x0600},
		{"missing-sar", pdu.V50, tlv.Fields{{Tag: tlv.SARTotalSegments, Value: []byte{2}}}, "020e000102", codec.RuleDependency, -1, 0x020c},
		{"sar-order", pdu.V50, tlv.Fields{{Tag: tlv.SARMsgRefNum, Value: []byte{0, 1}}, {Tag: tlv.SARTotalSegments, Value: []byte{2}}, {Tag: tlv.SARSegmentSeqnum, Value: []byte{3}}}, "020c00020001 020e000102 020f000103", codec.RuleDependency, 2, 0x020f},
		{"callback", pdu.V50, tlv.Fields{{Tag: tlv.CallbackNumPresInd, Value: []byte{0}}}, "0302000100", codec.RuleDependency, 0, 0x0302},
		{"network-node", pdu.V50, tlv.Fields{{Tag: tlv.SourceNetworkID, Value: []byte("123456\x00")}}, "060d000731323334353600", codec.RuleDependency, 0, 0x060d},
		{"portability", pdu.V50, tlv.Fields{{Tag: tlv.DestAddrNPResolution, Value: []byte{2}}}, "0611000102", codec.RuleDependency, 0, 0x0611},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := newCodec(t, tc.v)
			_, encodeErr := c.Append(nil, pdu.PDU{Sequence: 7, Body: &pdu.DataSM{Optional: tc.optional}})
			body := append(make([]byte, 10), unhex(t, tc.wire)...)
			_, decodeErr := c.Decode(semanticFrame(pdu.DataSMID, pdu.OK, body))
			for _, err := range []error{encodeErr, decodeErr} {
				wantDiagnostic(t, err, "optional", tc.rule, tc.index, tc.tag)
			}
			wantDecodeKind(t, decodeErr, codec.DecodeTLV)
		})
	}
	_, err := newCodec(t, pdu.V50).Decode(unhex(t, diagnosticTLVHex))
	wantDiagnostic(t, err, "optional", codec.RuleCardinality, 1, 0x0427)
	// Unknown tag zero and duplicates stay opaque; HasTag cannot use zero as absence.
	p := pdu.PDU{Sequence: 7, Body: &pdu.DataSM{Optional: tlv.Fields{{Tag: 0}, {Tag: 0}}}}
	if _, err := newCodec(t, pdu.V50).Append(nil, p); err != nil {
		t.Fatal(err)
	}
	p.Body.(*pdu.DataSM).Optional[0].Value = make([]byte, 65536)
	c, _ := codec.New(codec.Config{Version: pdu.V50, MaxFrame: 128 << 10})
	_, err = c.Append(nil, p)
	wantDiagnostic(t, err, "optional", codec.RuleLength, 0, 0)
}

func TestSemanticDiagnosticRepeatedDestinations(t *testing.T) {
	c := newCodec(t, pdu.V50)
	body := &pdu.SubmitMulti{Destinations: []pdu.MultiDestination{
		{Kind: pdu.SMEAddress}, {Kind: pdu.SMEAddress, Address: pdu.Address{NPI: 2}}, {Kind: pdu.SMEAddress},
	}}
	_, err := c.Append(nil, pdu.PDU{Sequence: 7, Body: body})
	wantDiagnostic(t, err, "dest_address.dest_addr_npi", codec.RuleValue, 1, -1)
	raw := unhex(t, "00 000000 03 01000000 01000200 01000000 00000000000000000000")
	_, err = c.Decode(semanticFrame(pdu.SubmitMultiID, pdu.OK, raw))
	wantDiagnostic(t, err, "dest_address.dest_addr_npi", codec.RuleValue, 1, -1)
	// A sticky source error must never be relocated into the destination list.
	body.Source.TON = 7
	_, err = c.Append(nil, pdu.PDU{Sequence: 7, Body: body})
	wantDiagnostic(t, err, "source_addr_ton", codec.RuleValue, -1, -1)
	resp := &pdu.SubmitMultiResp{Unsuccessful: []pdu.UnsuccessfulSME{{}, {Address: pdu.Address{TON: 7}}, {}}}
	_, err = c.Append(nil, pdu.PDU{Sequence: 7, Body: resp})
	wantDiagnostic(t, err, "unsuccess_sme.dest_addr_ton", codec.RuleValue, 1, -1)
	raw = unhex(t, "00 03 00000000000000 07000000000000 00000000000000")
	_, err = c.Decode(semanticFrame(pdu.SubmitMultiRespID, pdu.OK, raw))
	wantDiagnostic(t, err, "unsuccess_sme.dest_addr_ton", codec.RuleValue, 1, -1)
}

func TestSemanticDiagnosticHistoricalFieldErrors(t *testing.T) {
	for _, tc := range []struct {
		name                    string
		id                      pdu.CommandID
		body                    []byte
		field, oldField, reason string
		rule                    codec.SemanticRule
		index, offset           int
	}{
		{"bind", pdu.BindTransmitterID, unhex(t, "00000050000000"), "interface_version", "interface_version", "profile mismatch", codec.RuleProfile, -1, 23},
		{"count", pdu.SubmitMultiID, unhex(t, "0000000000"), "number_of_dests", "number_of_dests", "outside version bound", codec.RuleCount, -1, 21},
		{"flag", pdu.SubmitMultiID, unhex(t, "000000000103"), "dest_address.dest_flag", "dest_flag", "unknown destination kind", codec.RuleValue, 0, 22},
		{"replace-length", pdu.ReplaceSMID, append(unhex(t, "0000000000000000ff"), make([]byte, 255)...), "short_message", "short_message", "exceeds version bound", codec.RuleLength, -1, 280},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := newCodec(t, pdu.V34).Decode(semanticFrame(tc.id, pdu.OK, tc.body))
			wantDiagnostic(t, err, tc.field, tc.rule, tc.index, -1)
			wantDecodeKind(t, err, codec.DecodeMandatory)
			old, ok := errors.AsType[*codec.FieldError](err)
			if !ok || old.Field != tc.oldField || old.Offset != tc.offset || old.Reason != tc.reason || errors.Is(err, codec.ErrValidation) {
				t.Fatalf("historical cause changed: %+v / %v", old, err)
			}
		})
	}
	// A truncated bind must not become a fabricated profile mismatch.
	_, err := newCodec(t, pdu.V34).Decode(semanticFrame(pdu.BindTransmitterID, pdu.OK, []byte{0}))
	if _, ok := errors.AsType[*codec.SemanticError](err); ok {
		t.Fatal("annotated structural truncation", err)
	}
}

func TestSemanticDiagnosticContextAndConflicts(t *testing.T) {
	c := newCodec(t, pdu.V50)
	for _, tc := range []struct {
		name       string
		err        error
		field      string
		rule       codec.SemanticRule
		index, tag int
	}{
		{"direction-input", c.ValidateDataDirection(&pdu.DataSM{}, 0), "data_direction", codec.RuleValue, -1, -1},
		{"body", c.ValidateDataDirection(&pdu.SubmitSM{}, codec.DataSubmission), "body", codec.RuleBody, -1, -1},
		{"datagram", c.ValidateDataDirection(&pdu.DataSM{ESMClass: 1, RegisteredDelivery: 1}, codec.DataSubmission), "registered_delivery", codec.RuleMode, -1, -1},
		{"direction", c.ValidateDataDirection(&pdu.DataSM{Optional: tlv.Fields{{Tag: tlv.ReceiptedMessageID}}}, codec.DataSubmission), "optional", codec.RuleDirection, 0, int(tlv.ReceiptedMessageID)},
		{"mode", c.ValidateDataResponse(pdu.PDU{Body: &pdu.DataSMResp{MessageResponseFields: pdu.MessageResponseFields{Optional: tlv.Fields{{Tag: tlv.DeliveryFailureReason}}}}}, 3, codec.DataSubmission), "optional", codec.RuleMode, 0, int(tlv.DeliveryFailureReason)},
		{"status", c.ValidateDataResponse(pdu.PDU{Body: &pdu.DataSMResp{MessageResponseFields: pdu.MessageResponseFields{Optional: tlv.Fields{{Tag: tlv.DeliveryFailureReason}}}}}, 2, codec.DataSubmission), "optional", codec.RuleStatus, 0, int(tlv.DeliveryFailureReason)},
	} {
		t.Run(tc.name, func(t *testing.T) { wantDiagnostic(t, tc.err, tc.field, tc.rule, tc.index, tc.tag) })
	}
	_, err := c.Append(nil, pdu.PDU{Sequence: 7, Body: &pdu.SubmitSM{SMFields: pdu.SMFields{ESMClass: 0x40, Optional: tlv.Fields{{Tag: tlv.SourcePort, Value: []byte{0, 1}}}}}})
	wantDiagnostic(t, err, "optional", codec.RuleConflict, 0, int(tlv.SourcePort))
	_, err = c.Append(nil, pdu.PDU{Sequence: 7, Body: &pdu.SubmitSM{SMFields: pdu.SMFields{ShortMessage: []byte{0}, Optional: tlv.Fields{{Tag: tlv.MessagePayload}}}}})
	wantDiagnostic(t, err, "short_message", codec.RuleConflict, -1, -1)
	_, err = newCodec(t, pdu.V33).Append(nil, pdu.PDU{Sequence: 7, Body: &pdu.SubmitSM{SMFields: pdu.SMFields{Optional: tlv.Fields{{Tag: 0x1400}}}}})
	wantDiagnostic(t, err, "optional", codec.RuleVersion, -1, -1)
}

func TestSemanticDiagnosticHeaderTimesAndUnusedFields(t *testing.T) {
	c := newCodec(t, pdu.V34)
	// A syntactically terminated but invalid time remains semantic, whereas an
	// unterminated C-octet is still structural and has no invented rule.
	b := &pdu.QuerySMResp{FinalDate: "bad", MessageState: 2}
	_, enc := c.Append(nil, pdu.PDU{Sequence: 7, Body: b})
	_, dec := c.Decode(semanticFrame(pdu.QuerySMRespID, pdu.OK, []byte{0, 'b', 'a', 'd', 0, 2, 0}))
	for _, err := range []error{enc, dec} {
		wantDiagnostic(t, err, "final_date", codec.RuleTime, -1, -1)
	}
	wantDecodeKind(t, dec, codec.DecodeSemantic)
	_, err := c.Decode(semanticFrame(pdu.QuerySMRespID, pdu.OK, []byte{0, 'b', 'a', 'd'}))
	wantDecodeKind(t, err, codec.DecodeMandatory)
	if _, ok := errors.AsType[*codec.SemanticError](err); ok {
		t.Fatal("structural time became semantic")
	}
	_, err = c.Decode(unhex(t, "00000012 80000005 00000000 00000007 7800"))
	wantDiagnostic(t, err, "message_id", codec.RuleUnused, -1, -1)
	_, err = c.Append(nil, pdu.PDU{Sequence: 7, Body: &pdu.DeliverSMResp{MessageResponseFields: pdu.MessageResponseFields{MessageID: "x"}}})
	wantDiagnostic(t, err, "message_id", codec.RuleUnused, -1, -1)
	for _, tc := range []struct {
		packet     pdu.PDU
		raw, field string
		rule       codec.SemanticRule
	}{
		{pdu.PDU{Body: &pdu.EnquireLink{}}, "00000010 00000015 00000000 00000000", "sequence_number", codec.RuleValue},
		{pdu.PDU{Sequence: 7, Status: 1, Body: &pdu.EnquireLink{}}, "00000010 00000015 00000001 00000007", "command_status", codec.RuleStatus},
		{pdu.PDU{Sequence: 7, Body: &pdu.GenericNACK{}}, "00000010 80000000 00000000 00000007", "command_status", codec.RuleStatus},
	} {
		_, enc := c.Append(nil, tc.packet)
		_, dec := c.Decode(unhex(t, tc.raw))
		for _, err := range []error{enc, dec} {
			wantDiagnostic(t, err, tc.field, tc.rule, -1, -1)
		}
		wantDecodeKind(t, dec, codec.DecodeSemantic)
	}
	_, err = codec.New(codec.Config{Version: 0x35})
	wantDiagnostic(t, err, "interface_version", codec.RuleVersion, -1, -1)
	if !errors.Is(err, codec.ErrUnsupported) || errors.Is(err, codec.ErrValidation) {
		t.Fatal("changed unsupported cause")
	}
	_, err = newCodec(t, pdu.V33).Decode(semanticFrame(pdu.DataSMID, pdu.OK, make([]byte, 10)))
	wantDiagnostic(t, err, "command_id", codec.RuleCommand, -1, -1)
	wantDecodeKind(t, err, codec.DecodeUnsupported)
}

func TestSemanticDiagnosticCustomerDefinitionBounds(t *testing.T) {
	for _, tc := range []struct {
		name     string
		version  pdu.Version
		commands []pdu.CommandID
		values   tlv.Fields
		rule     codec.SemanticRule
		index    int
	}{
		{"version", pdu.V34, []pdu.CommandID{pdu.DataSMID}, tlv.Fields{{Tag: 0x1500, Value: []byte{1}}}, codec.RuleVersion, 0},
		{"command", pdu.V50, []pdu.CommandID{pdu.SubmitSMID}, tlv.Fields{{Tag: 0x1500, Value: []byte{1}}}, codec.RuleCommand, 0},
		{"length", pdu.V50, []pdu.CommandID{pdu.DataSMID}, tlv.Fields{{Tag: 0x1500}}, codec.RuleLength, 0},
		{"cardinality", pdu.V50, []pdu.CommandID{pdu.DataSMID}, tlv.Fields{{Tag: 0x1500, Value: []byte{1}}, {Tag: 0x1500, Value: []byte{2}}}, codec.RuleCardinality, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			c, err := codec.New(codec.Config{Version: tc.version, TLVs: []codec.TLVDefinition{{Tag: 0x1500, MinLength: 1, MaxLength: 1, Commands: tc.commands, Versions: []pdu.Version{pdu.V50}, Validate: func([]byte) error { calls++; return nil }}}})
			if err != nil {
				t.Fatal(err)
			}
			_, err = c.Append(nil, pdu.PDU{Sequence: 7, Body: &pdu.DataSM{Optional: tc.values}})
			wantDiagnostic(t, err, "optional", tc.rule, tc.index, 0x1500)
			// Only preceding valid occurrences reach trusted callbacks.
			if calls != tc.index {
				t.Fatalf("callback calls=%d want=%d", calls, tc.index)
			}
		})
	}
}

func TestSemanticDiagnosticExtensionCause(t *testing.T) {
	secret := "synthetic-secret-do-not-log"
	for _, cause := range []error{errors.New(secret), fmt.Errorf("%s: %w", secret, codec.ErrUnsupported), &codec.FieldError{Field: secret, Reason: secret}} {
		c, err := codec.New(codec.Config{Version: pdu.V50, TLVs: []codec.TLVDefinition{{Tag: 0x1500, MinLength: 1, MaxLength: 1, Commands: []pdu.CommandID{pdu.DataSMID}, Versions: []pdu.Version{pdu.V50}, Validate: func([]byte) error { return cause }}}})
		if err != nil {
			t.Fatal(err)
		}
		_, encodeErr := c.Append(nil, pdu.PDU{Sequence: 7, Body: &pdu.DataSM{Optional: tlv.Fields{{Tag: 0x1500, Value: []byte{0}}}}})
		_, decodeErr := c.Decode(semanticFrame(pdu.DataSMID, pdu.OK, append(make([]byte, 10), 0x15, 0, 0, 1, 0)))
		for _, err := range []error{encodeErr, decodeErr} {
			e := wantDiagnostic(t, err, "optional", codec.RuleExtension, 0, 0x1500)
			if !errors.Is(err, codec.ErrValidation) || !errors.Is(err, cause) {
				t.Fatal("lost callback cause", err)
			}
			if strings.Contains(err.Error(), secret) || strings.Contains(e.Error(), secret) {
				t.Fatal("unsafe error summary")
			}
		}
		wantDecodeKind(t, decodeErr, codec.DecodeTLV)
	}
}

func TestSemanticDiagnosticConcurrentSnapshots(t *testing.T) {
	c := newCodec(t, pdu.V50)
	raw := unhex(t, diagnosticTLVHex)
	var wg sync.WaitGroup
	for range 16 {
		wg.Go(func() {
			for range 40 {
				_, err := c.Decode(raw)
				e := wantDiagnostic(t, err, "optional", codec.RuleCardinality, 1, 0x0427)
				// Caller mutation of its own error must not affect later operations.
				e.Field, e.Index = "caller", 999
			}
		})
	}
	wg.Wait()
}

// Reused by FuzzDecode: inspect diagnostic shape without inventing a second
// validation oracle. All inputs are untrusted wire bytes, no custom callbacks.
func assertSemanticSnapshot(t testing.TB, err error) {
	t.Helper()
	if e, ok := errors.AsType[*codec.SemanticError](err); ok {
		if e.Field == "" || e.Rule == "" || e.HasIndex && e.Index < 0 || !e.HasIndex && e.Index != 0 || !e.HasTag && e.Tag != 0 || errors.Unwrap(e) == nil {
			t.Fatalf("invalid diagnostic snapshot: %+v", e)
		}
		if e.HasTag && e.Field != "optional" {
			t.Fatalf("tag on non-TLV field: %+v", e)
		}
	}
}
