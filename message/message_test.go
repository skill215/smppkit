package message_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/skill215/smppkit/message"
	"github.com/skill215/smppkit/pdu"
	"github.com/skill215/smppkit/textcodec"
	"github.com/skill215/smppkit/tlv"
)

func makeParts(t testing.TB, text string, policy message.Policy) []pdu.SMFields {
	t.Helper()
	p, e := message.Prepare(pdu.V50, message.Text{Source: pdu.Address{Value: "from"}, Destination: pdu.Address{Value: "to"}, Text: text}, policy)
	if e != nil {
		t.Fatal(e)
	}
	parts, e := p.Materialize(0x34)
	if e != nil {
		t.Fatal(e)
	}
	return parts
}
func receiver(t testing.TB, cfg message.ReceiveConfig) *message.Receiver {
	t.Helper()
	cfg.AckPolicy = message.AckOnBuffered
	r, e := message.NewReceiver(cfg)
	if e != nil {
		t.Fatal(e)
	}
	return r
}
func TestPreparationIndependentBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name         string
		enc          textcodec.Encoding
		mode         message.Mode
		text         string
		count, first int
	}{
		{"GSM160", textcodec.GSM7, message.UDH8, strings.Repeat("A", 160), 1, 160},
		{"GSM161", textcodec.GSM7, message.UDH8, strings.Repeat("A", 161), 2, 153},
		{"GSM UDH16", textcodec.GSM7, message.UDH16, strings.Repeat("A", 161), 2, 152},
		{"extension boundary", textcodec.GSM7, message.UDH8, strings.Repeat("A", 152) + "^" + strings.Repeat("A", 8), 2, 152},
		{"UCS70", textcodec.UCS2, message.UDH16, strings.Repeat("中", 70), 1, 140},
		{"UCS71", textcodec.UCS2, message.UDH16, strings.Repeat("中", 71), 2, 132},
		{"surrogate boundary", textcodec.UTF16BE, message.UDH16, strings.Repeat("A", 65) + "😀" + strings.Repeat("A", 5), 2, 130},
		{"SAR", textcodec.GSM7, message.SAR, strings.Repeat("A", 161), 2, 152},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, e := message.Prepare(pdu.V50, message.Text{Text: tc.text}, message.Policy{Encoding: tc.enc, Mode: tc.mode})
			if e != nil {
				t.Fatal(e)
			}
			if p.Count() != tc.count || len(p.Preview()[0]) != tc.first {
				t.Fatalf("parts=%d first=%d", p.Count(), len(p.Preview()[0]))
			}
			parts, e := p.Materialize(0x34)
			if e != nil {
				t.Fatal(e)
			}
			if p.Multipart() && tc.mode == message.UDH8 {
				if !bytes.Equal(parts[0].ShortMessage[:6], []byte{5, 0, 3, 0x34, 2, 1}) {
					t.Fatal("independent UDH8 vector")
				}
			}
			var raw []byte
			for _, b := range parts {
				s, e := message.ParseSegment(b)
				if e != nil {
					t.Fatal(e)
				}
				raw = append(raw, s.Payload...)
			}
			decoded, e := tc.enc.Decode(raw)
			if e != nil || decoded != tc.text {
				t.Fatal("boundary lost", e)
			}
			preview := p.Preview()
			clear(preview[0])
			again, _ := p.Materialize(0x34)
			if len(again[0].ShortMessage) > 0 && again[0].ShortMessage[len(again[0].ShortMessage)-1] == 0 {
				t.Fatal("preview aliases plan")
			}
		})
	}
}
func TestPreparationValidationBeforeTransmission(t *testing.T) {
	if _, e := message.Prepare(pdu.V50, message.Text{Text: strings.Repeat("A", 1000) + "中"}, message.Policy{Encoding: textcodec.GSM7}); e == nil {
		t.Fatal("unencodable late character")
	}
	p, e := message.Prepare(pdu.V50, message.Text{Text: "中"}, message.GSMText())
	if e != nil || p.Encoding() != textcodec.UCS2.Name() {
		t.Fatal("fallback not exposed", e)
	}
	for _, v := range []pdu.Version{pdu.V33, pdu.V34, pdu.V50} {
		_, e := message.Prepare(v, message.Text{Text: strings.Repeat("A", 161)}, message.GSMText())
		if (e != nil) != (v == pdu.V33) {
			t.Fatal(v, e)
		}
	}
	_, e = message.Prepare(pdu.V50, message.Text{Text: strings.Repeat("A", 500)}, message.Policy{Encoding: textcodec.GSM7, MaxSegments: 2})
	if !errors.Is(e, message.ErrCapacity) {
		t.Fatal(e)
	}
	p, e = message.Prepare(pdu.V50, message.Text{Text: ""}, message.GSMText())
	if e != nil || p.Count() != 1 {
		t.Fatal("empty policy", e)
	}
	// Additional 16-bit port element consumes six UDH bytes, including IE header.
	parts := makeParts(t, strings.Repeat("A", 161), message.Policy{Encoding: textcodec.GSM7, Mode: message.UDH8, AdditionalUDH: []message.InformationElement{{ID: 5, Data: []byte{0, 1, 0, 2}}}})
	s, e := message.ParseSegment(parts[0])
	if e != nil || len(s.Payload) != 146 || !bytes.Equal(s.Ports, []byte{5, 4, 0, 1, 0, 2}) {
		t.Fatal(s, e)
	}
}
func TestReassemblyReverseDuplicateConflictAndNamespaces(t *testing.T) {
	text := strings.Repeat("A^中", 100)
	parts := makeParts(t, text, message.Policy{Encoding: textcodec.UCS2})
	now := time.Now()
	r := receiver(t, message.ReceiveConfig{Encodings: map[uint8]textcodec.Encoding{8: textcodec.UCS2}})
	for i := len(parts) - 1; i >= 0; i-- {
		a, e := r.Admit("account1", parts[i], now)
		if e != nil {
			t.Fatal(e)
		}
		if i > 0 && a.Complete {
			t.Fatal("early completion")
		}
		dup, e := r.Admit("account1", parts[i], now)
		if e != nil || !dup.Duplicate {
			t.Fatal(dup, e)
		}
	}
	got, e := r.Receive(context.Background())
	if e != nil || got.Text != text || !got.Decoded || len(got.Segments) != len(parts) {
		t.Fatal(got, e)
	}
	if r.Stats().Queued != 0 || r.Stats().BufferedBytes != 0 {
		t.Fatal("resources not transferred", r.Stats())
	}
	bad := parts[0].Clone()
	bad.ShortMessage[len(bad.ShortMessage)-1] ^= 1
	if _, e = r.Admit("account1", bad, now); !errors.Is(e, message.ErrConflict) {
		t.Fatal("completed duplicate conflict", e)
	}
	if _, e = r.Admit("account2", bad, now); e != nil {
		t.Fatal("namespace isolation", e)
	}
	changed := parts[1].Clone()
	changed.ShortMessage[5]++ // total is consistency, not identity
	if _, e = r.Admit("account2", changed, now); !errors.Is(e, message.ErrConflict) {
		t.Fatal("total count split into a different group", e)
	}
	changed = parts[1].Clone()
	changed.DataCoding = 3
	if _, e = r.Admit("account2", changed, now); !errors.Is(e, message.ErrConflict) {
		t.Fatal("DCS conflict", e)
	}
}
func TestConcurrentCompletionOnce(t *testing.T) {
	parts := makeParts(t, strings.Repeat("A", 400), message.GSMText())
	r := receiver(t, message.ReceiveConfig{})
	var wg sync.WaitGroup
	errs := make(chan error, len(parts)*20)
	for range 20 {
		for _, part := range parts {
			wg.Go(func() { _, e := r.Admit("ns", part, time.Now()); errs <- e })
		}
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	if r.Stats().Queued != 1 {
		t.Fatal(r.Stats())
	}
}
func TestCapacityExpiryAndCompletionBackpressure(t *testing.T) {
	now := time.Now()
	parts := makeParts(t, strings.Repeat("A", 200), message.GSMText())
	r := receiver(t, message.ReceiveConfig{MaxGroups: 1, Lifetime: time.Second, QueueSize: 1, EventLimit: 1})
	if _, e := r.Admit("ns", parts[0], now); e != nil {
		t.Fatal(e)
	}
	if _, e := r.Admit("other", parts[0], now); !errors.Is(e, message.ErrCapacity) {
		t.Fatal(e)
	}
	if _, e := r.Admit("ns", parts[0], now.Add(900*time.Millisecond)); e != nil {
		t.Fatal(e)
	}
	if e := r.Expire(now.Add(time.Second)); e != nil {
		t.Fatal(e)
	}
	events := r.Events()
	if len(events) != 1 || len(events[0].Missing) != 1 || events[0].Missing[0] != 2 || r.Stats().Groups != 0 {
		t.Fatal(events, r.Stats())
	}
	if _, e := r.Admit("ns", pdu.SMFields{ShortMessage: []byte("single")}, now); e != nil {
		t.Fatal(e)
	}
	if _, e := r.Admit("ns", parts[0], now); e != nil {
		t.Fatal(e)
	}
	if _, e := r.Admit("ns", parts[1], now); !errors.Is(e, message.ErrCapacity) {
		t.Fatal(e)
	}
	if _, e := r.Receive(context.Background()); e != nil {
		t.Fatal(e)
	}
	if _, e := r.Admit("ns", parts[1], now); e != nil {
		t.Fatal("final part not retryable after capacity clears", e)
	}
	// Receipt content must not be interpreted as UDH or held in an assembly group.
	receipt := pdu.SMFields{ESMClass: 4 | 0x40, ShortMessage: []byte{255, 0, 1}, Optional: tlv.Fields{{Tag: tlv.ReceiptedMessageID, Value: []byte("id\x00")}}}
	if _, e := message.ParseSegment(receipt); e != nil {
		t.Fatal(e)
	}
}
func TestReferencesExhaustionAndRetention(t *testing.T) {
	now := time.Now()
	refs, e := message.NewReferences(8, 2, time.Minute)
	if e != nil {
		t.Fatal(e)
	}
	a, _ := refs.Reserve(now)
	b, _ := refs.Reserve(now)
	if a == b {
		t.Fatal("live reuse")
	}
	if _, e = refs.Reserve(now.Add(59 * time.Second)); !errors.Is(e, message.ErrCapacity) {
		t.Fatal(e)
	}
	if _, e = refs.Reserve(now.Add(time.Minute)); e != nil {
		t.Fatal(e)
	}
}

func TestReferenceSpaceBoundary(t *testing.T) {
	now := time.Now()
	refs, err := message.NewReferences(8, 256, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	values := make(chan uint16, 256)
	for range 256 {
		wg.Go(func() {
			v, err := refs.Reserve(now)
			if err != nil {
				t.Error(err)
				return
			}
			values <- v
		})
	}
	wg.Wait()
	close(values)
	seen := make(map[uint16]bool)
	for v := range values {
		if v > 255 || seen[v] {
			t.Fatalf("invalid/reused reference %d", v)
		}
		seen[v] = true
	}
	if len(seen) != 256 || !seen[0] || !seen[255] {
		t.Fatal("full 8-bit space not used")
	}
	if _, err = refs.Reserve(now.Add(time.Minute - time.Nanosecond)); !errors.Is(err, message.ErrCapacity) {
		t.Fatal(err)
	}
	if _, err = refs.Reserve(now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	plan, err := message.Prepare(pdu.V50, message.Text{Text: strings.Repeat("A", 200)}, message.Policy{Encoding: textcodec.GSM7, Mode: message.UDH8})
	if err != nil {
		t.Fatal(err)
	}
	for _, ref := range []uint16{0, 255} {
		parts, err := plan.Materialize(ref)
		if err != nil || !bytes.Equal(parts[0].ShortMessage[:6], []byte{5, 0, 3, byte(ref), 2, 1}) {
			t.Fatal(parts, err)
		}
	}
	if _, err = plan.Materialize(256); !errors.Is(err, message.ErrMetadata) {
		t.Fatal(err)
	}
}
func TestMalformedMetadata(t *testing.T) {
	for _, raw := range [][]byte{{}, {9, 0}, {5, 0, 3, 1, 0, 1}, {5, 0, 3, 1, 2, 3}, {4, 8, 2, 1, 2}, {1, 0}, {11, 0, 3, 1, 2, 1, 0, 3, 1, 2, 1}} {
		if _, e := message.ParseSegment(pdu.SMFields{ESMClass: 0x40, ShortMessage: raw}); e == nil {
			t.Fatal(raw)
		}
	}
	if _, e := message.ParseSegment(pdu.SMFields{Optional: tlv.Fields{tlv.Uint16(tlv.SARMsgRefNum, 1)}}); e == nil {
		t.Fatal("incomplete SAR")
	}
}

func TestReceiveRawOwnershipAndDecodeError(t *testing.T) {
	encodings := map[uint8]textcodec.Encoding{8: textcodec.UCS2}
	r := receiver(t, message.ReceiveConfig{Encodings: encodings})
	delete(encodings, 8) // configuration map is copied at construction
	fields := pdu.SMFields{DataCoding: 8, ShortMessage: []byte{0xd8, 0}}
	if _, err := r.Admit("ns", fields, time.Now()); err != nil {
		t.Fatal(err)
	}
	clear(fields.ShortMessage) // successful admission owns a copy
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	m, err := r.Receive(ctx) // available queue takes precedence over cancellation
	if err != nil || m.Decoded || !errors.Is(m.DecodeError, textcodec.ErrEncoding) || !bytes.Equal(m.Payload, []byte{0xd8, 0}) || !bytes.Equal(m.Segments[0].Fields.ShortMessage, []byte{0xd8, 0}) {
		t.Fatalf("decode failure lost raw admitted data: %+v %v", m, err)
	}
	if r.Stats().Queued != 0 || r.Stats().BufferedBytes != 0 {
		t.Fatal("ownership transfer retained budget", r.Stats())
	}
	if _, err := r.Receive(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
func FuzzUDH(f *testing.F) {
	f.Add([]byte{5, 0, 3, 1, 2, 1, 'A'})
	f.Add([]byte{6, 8, 4, 0, 1, 2, 1, 'A'})
	f.Fuzz(func(t *testing.T, b []byte) {
		if len(b) > 65536 {
			return
		}
		_, _ = message.ParseSegment(pdu.SMFields{ESMClass: 0x40, ShortMessage: b})
	})
}
func BenchmarkPrepare(b *testing.B) {
	text := message.Text{Text: strings.Repeat("hello^", 100)}
	b.ReportAllocs()
	for b.Loop() {
		if _, e := message.Prepare(pdu.V50, text, message.GSMText()); e != nil {
			b.Fatal(e)
		}
	}
}

func TestDuplicateAdmissionAtByteLimitAndExpiryEvents(t *testing.T) {
	now := time.Now()
	parts := makeParts(t, strings.Repeat("A", 200), message.GSMText())
	probe := receiver(t, message.ReceiveConfig{})
	if _, e := probe.Admit("ns", parts[0], now); e != nil {
		t.Fatal(e)
	}
	limit := probe.Stats().BufferedBytes
	bounded := receiver(t, message.ReceiveConfig{MaxBytes: limit, MaxMessageBytes: limit})
	a, e := bounded.Admit("ns", parts[0], now)
	if e != nil {
		t.Fatal(e)
	}
	dup, e := bounded.Admit("ns", parts[0], now)
	if e != nil || !dup.Duplicate || dup.ID != a.ID || bounded.Stats().BufferedBytes != limit {
		t.Fatal("duplicate consumed capacity", dup, e)
	}
	r := receiver(t, message.ReceiveConfig{EventLimit: 1, Lifetime: time.Second})
	for _, ns := range []string{"one", "two"} {
		if _, e := r.Admit(ns, parts[0], now); e != nil {
			t.Fatal(e)
		}
	}
	if e := r.Expire(now.Add(time.Second)); !errors.Is(e, message.ErrCapacity) {
		t.Fatal(e)
	}
	if r.Stats().Groups != 1 || len(r.Events()) != 1 {
		t.Fatal("expiry lost evidence", r.Stats())
	}
	if e := r.Expire(now.Add(time.Second)); e != nil || r.Stats().Groups != 0 || len(r.Events()) != 1 {
		t.Fatal("expiry recovery failed", e)
	}
}
