// SPDX-AI-Disclosure: ai-generated
// SPDX-AI-Model: GPT-6.0 Astra
// SPDX-AI-Provider: OpenAI
// SPDX-AI-Scope: Test code generated and revised with Codex under maintainer direction.
// SPDX-AI-Date: 2026-10-29

package message_test

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/skill215/smppkit/codec"
	"github.com/skill215/smppkit/message"
	"github.com/skill215/smppkit/pdu"
	"github.com/skill215/smppkit/textcodec"
)

// Embedding preserves the existing Encoding interface while making any accidental
// callback visible. Unsupported declarations must fail before encoding/fallback
// and must not reach caller-supplied decoding code.
type noLanguageCallbacks struct{ textcodec.Alphabet }

func (noLanguageCallbacks) Encode(string) (textcodec.Encoded, error) {
	panic("unsupported UDH reached encoder")
}
func (noLanguageCallbacks) Decode([]byte) (string, error) { panic("unsupported UDH reached decoder") }

func TestNationalLanguagePrepareBeforeCallbacks(t *testing.T) {
	for _, v := range []pdu.Version{pdu.V33, pdu.V34, pdu.V50} {
		for _, id := range []byte{0x24, 0x25} {
			for _, data := range [][]byte{nil, {0}, {1}, {255}, {1, 2}, make([]byte, 256)} {
				policy := message.GSMText()
				policy.Encoding = noLanguageCallbacks{textcodec.GSM7}
				policy.Fallback = noLanguageCallbacks{textcodec.UCS2}
				policy.AdditionalUDH = []message.InformationElement{{ID: id, Data: data}}
				plan, err := message.Prepare(v, message.Text{Text: "Ø"}, policy)
				if plan != nil || !errors.Is(err, message.ErrUnsupportedNationalLanguage) {
					t.Fatalf("v=%x id=%x length=%d plan=%v error=%v", v, id, len(data), plan, err)
				}
			}
		}
	}
}

func receiveLanguageFields(t testing.TB, fields pdu.SMFields, enc textcodec.Encoding) message.Message {
	t.Helper()
	var encodings map[uint8]textcodec.Encoding
	if enc != nil {
		encodings = map[uint8]textcodec.Encoding{enc.DataCoding(): enc}
	}
	r := receiver(t, message.ReceiveConfig{Encodings: encodings})
	if _, err := r.Admit("language", fields, time.Now()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	m, err := r.Receive(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if stats := r.Stats(); stats.Queued != 0 || stats.BufferedBytes != 0 {
		t.Fatalf("charges not released: %+v", stats)
	}
	return m
}

func requireLanguageError(t testing.TB, m message.Message) {
	t.Helper()
	if m.Decoded || m.Text != "" || !errors.Is(m.DecodeError, message.ErrUnsupportedNationalLanguage) {
		t.Fatalf("decoded=%v text=%q error=%v", m.Decoded, m.Text, m.DecodeError)
	}
}

func TestNationalLanguageIndependentWire(t *testing.T) {
	// Hand-authored deliver_sm, not produced by our encoder. UDHL=3, locking
	// table=Turkish, payload=0x0b (Ğ in that table, Ø in the default alphabet).
	raw, err := hex.DecodeString(strings.ReplaceAll("0000002c 00000005 00000000 00000007 00 0101 31323300 0101 34353600 40 00 00 00 00 00 00 00 00 05 032501010b", " ", ""))
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range []pdu.Version{pdu.V34, pdu.V50} {
		c, err := codec.New(codec.Config{Version: v})
		if err != nil {
			t.Fatal(err)
		}
		packet, err := c.Decode(raw)
		if err != nil {
			t.Fatal(err)
		}
		fields := packet.Body.(*pdu.DeliverSM).SMFields
		m := receiveLanguageFields(t, fields, textcodec.GSM7)
		requireLanguageError(t, m)
		if !bytes.Equal(m.Payload, []byte{0x0b}) || !bytes.Equal(m.Segments[0].UDH, []byte{0x25, 1, 1}) || !bytes.Equal(m.Segments[0].Fields.ShortMessage, fields.ShortMessage) {
			t.Fatal("lost original evidence")
		}
		// Codec continues to support raw callers, without interpreting language IEs.
		out, err := c.Append(nil, packet)
		if err != nil || !bytes.Equal(out, raw) {
			t.Fatalf("raw round trip: %v", err)
		}
	}
}

func TestNationalLanguageReceivePolicies(t *testing.T) {
	for _, header := range [][]byte{
		{0x24, 1, 1}, {0x25, 1, 1}, {0x24, 1, 0}, {0x25, 1, 255},
		{0x24, 0}, {0x25, 2, 1, 2}, {0x24, 1, 1, 0x24, 1, 2},
		{0x70, 1, 9, 0x25, 1, 1}, {0x24, 1, 1, 0x25, 1, 1},
	} {
		raw := append([]byte{byte(len(header))}, header...)
		raw = append(raw, 0x0b)
		fields := pdu.SMFields{ESMClass: 0x40, ShortMessage: raw}
		for _, enc := range []textcodec.Encoding{textcodec.GSM7, noLanguageCallbacks{textcodec.GSM7}, noLanguageCallbacks{textcodec.UCS2}} {
			fields.DataCoding = enc.DataCoding()
			m := receiveLanguageFields(t, fields, enc)
			requireLanguageError(t, m)
		}
		// Without text decoding there is no decoder failure. Raw storage is owned.
		m := receiveLanguageFields(t, fields, nil)
		if m.Decoded || m.DecodeError != nil || !bytes.Equal(m.Payload, []byte{0x0b}) {
			t.Fatal("raw-only reception changed")
		}
		saved := bytes.Clone(m.Segments[0].Fields.ShortMessage)
		raw[0] = 0
		if !bytes.Equal(m.Segments[0].Fields.ShortMessage, saved) {
			t.Fatal("borrowed input")
		}
	}
}

func TestNationalLanguageMultipartAndConflict(t *testing.T) {
	r := receiver(t, message.ReceiveConfig{Encodings: map[uint8]textcodec.Encoding{0: noLanguageCallbacks{textcodec.GSM7}}})
	first := pdu.SMFields{ESMClass: 0x40, ShortMessage: []byte{8, 0, 3, 0x2a, 2, 1, 0x25, 1, 1, 0x0b}}
	second := pdu.SMFields{ESMClass: 0x40, ShortMessage: []byte{8, 0, 3, 0x2a, 2, 2, 0x25, 1, 1, 0x41}}
	now := time.Now()
	if a, err := r.Admit("multipart", second, now); err != nil || a.Complete {
		t.Fatalf("second: %+v %v", a, err)
	}
	conflict := first.Clone()
	conflict.ShortMessage[8] = 2
	if _, err := r.Admit("multipart", conflict, now); !errors.Is(err, message.ErrConflict) {
		t.Fatalf("mixed table: %v", err)
	}
	if a, err := r.Admit("multipart", first, now); err != nil || !a.Complete {
		t.Fatalf("first: %+v %v", a, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	m, err := r.Receive(ctx)
	if err != nil {
		t.Fatal(err)
	}
	requireLanguageError(t, m)
	if !bytes.Equal(m.Payload, []byte{0x0b, 0x41}) || len(m.Segments) != 2 {
		t.Fatal("lost assembled bytes")
	}
	if a, err := r.Admit("multipart", first, now); err != nil || !a.Duplicate {
		t.Fatalf("duplicate after failed decode: %+v %v", a, err)
	}
}

func TestNationalLanguageGuardDoesNotScanPayloadOrOtherIEValues(t *testing.T) {
	// A language-like byte inside another IE's value is opaque; a language-like
	// byte in payload is an ordinary GSM7 code. Neither declares a language table.
	for _, fields := range []pdu.SMFields{
		{ShortMessage: []byte{0x25}},
		{ESMClass: 0x40, ShortMessage: []byte{5, 0x70, 3, 0x25, 1, 1, 0x0b}},
		{ESMClass: 0x40, ShortMessage: []byte{0, 0x0b}},
	} {
		m := receiveLanguageFields(t, fields, textcodec.GSM7)
		if !m.Decoded || m.DecodeError != nil {
			t.Fatalf("ordinary decoding blocked: %v", m.DecodeError)
		}
	}
	// Receipt classification precedes UDH interpretation and remains raw.
	m := receiveLanguageFields(t, pdu.SMFields{ESMClass: 0x44, ShortMessage: []byte{3, 0x25, 1, 1, 0x0b}}, noLanguageCallbacks{textcodec.GSM7})
	if !m.Receipt || m.Decoded || m.DecodeError != nil {
		t.Fatal("receipt handling changed")
	}
	// IE data cannot borrow bytes from the text beyond UDHL.
	_, err := message.ParseSegment(pdu.SMFields{ESMClass: 0x40, ShortMessage: []byte{2, 0x25, 1, 1, 0x0b}})
	if !errors.Is(err, message.ErrMetadata) {
		t.Fatalf("IE crossed UDHL: %v", err)
	}
}

func FuzzNationalLanguageProtection(f *testing.F) {
	for _, raw := range [][]byte{{3, 0x25, 1, 1, 0x0b}, {3, 0x24, 1, 0, 0x1b, 0x47}, {2, 0x25, 1, 1}, {0, 0x41}, {8, 0, 3, 42, 1, 1, 0x25, 1, 1, 0x0b}} {
		f.Add(raw)
	}
	f.Fuzz(func(t *testing.T, raw []byte) {
		if len(raw) > 2048 {
			return
		}
		fields := pdu.SMFields{ESMClass: 0x40, ShortMessage: raw}
		seg, err := message.ParseSegment(fields)
		if err != nil || seg.Concat != nil && seg.Concat.Total != 1 {
			return
		}
		m := receiveLanguageFields(t, fields, textcodec.GSM7)
		if !bytes.Equal(m.Payload, seg.Payload) || !bytes.Equal(m.Segments[0].Fields.ShortMessage, raw) {
			t.Fatal("decode changed raw evidence")
		}
		if errors.Is(m.DecodeError, message.ErrUnsupportedNationalLanguage) && (m.Decoded || m.Text != "") {
			t.Fatal("unsupported language returned text")
		}
	})
}
