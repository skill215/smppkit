package codec_test

import (
	"bytes"
	"errors"
	"testing"

	"github.com/skill215/smppkit/codec"
	"github.com/skill215/smppkit/pdu"
	"github.com/skill215/smppkit/tlv"
)

type extension struct {
	id    pdu.CommandID
	wrong bool
}

func (x extension) DecodeBody(b []byte) (pdu.Body, error) {
	id := x.id
	if x.wrong {
		id++
	}
	return &pdu.Opaque{ID: id, Data: append([]byte(nil), b...)}, nil
}
func (x extension) AppendBody(b []byte, p pdu.Body) ([]byte, error) {
	raw, ok := p.(*pdu.Opaque)
	if !ok {
		return b, codec.ErrValidation
	}
	return append(b, raw.Data...), nil
}
func TestScopedPrivateCommand(t *testing.T) {
	const req pdu.CommandID = 0x10200
	const resp pdu.CommandID = 0x80010200
	definition := codec.CommandDefinition{Request: req, Response: resp, Versions: []pdu.Version{pdu.V34, pdu.V50}, RequestCodec: extension{id: req}, ResponseCodec: extension{id: resp}}
	c, e := codec.New(codec.Config{Version: pdu.V50, Commands: []codec.CommandDefinition{definition}})
	if e != nil {
		t.Fatal(e)
	}
	if id, ok := c.ResponseCommand(req); !ok || id != resp {
		t.Fatal("private response relationship")
	}
	want := unhex(t, "00000012 00010200 00000000 00000005 1234")
	p, e := c.Decode(want)
	if e != nil {
		t.Fatal(e)
	}
	got, e := c.Append(nil, p)
	if e != nil || !bytes.Equal(got, want) {
		t.Fatal(e)
	}
	// Scope is local; a standard codec preserves but does not authorize the body.
	if _, ok := newCodec(t, pdu.V50).ResponseCommand(req); ok {
		t.Fatal("global registration")
	}
	definition.RequestCodec = extension{id: req, wrong: true}
	bad, e := codec.New(codec.Config{Version: pdu.V50, Commands: []codec.CommandDefinition{definition}})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = bad.Decode(want); e == nil {
		t.Fatal("wrong body identity")
	}
	definition.Request = pdu.SubmitSMID
	if _, e = codec.New(codec.Config{Version: pdu.V50, Commands: []codec.CommandDefinition{definition}}); e == nil {
		t.Fatal("standard override")
	}
}
func TestTLVDefinitionsAndRawPreservation(t *testing.T) {
	def := codec.TLVDefinition{Tag: 0x1500, MinLength: 1, MaxLength: 2, Commands: []pdu.CommandID{pdu.SubmitSMID}, Versions: []pdu.Version{pdu.V50}}
	cfg := codec.Config{Version: pdu.V50, TLVs: []codec.TLVDefinition{def}}
	c, e := codec.New(cfg)
	if e != nil {
		t.Fatal(e)
	}
	cfg.TLVs[0].Commands[0] = pdu.DeliverSMID
	p := pdu.PDU{Sequence: 1, Body: &pdu.SubmitSM{SMFields: pdu.SMFields{Optional: tlv.Fields{{Tag: 0x1500, Value: []byte{7}}}}}}
	wire, e := c.Append(nil, p)
	if e != nil {
		t.Fatal("profile borrows definition", e)
	}
	p.Body.(*pdu.SubmitSM).Optional = append(p.Body.(*pdu.SubmitSM).Optional, tlv.Field{Tag: 0x1500, Value: []byte{8}})
	if _, e = c.Append(nil, p); e == nil {
		t.Fatal("cardinality")
	}
	// Raw unknown repeated fields preserve order when not registered.
	raw := newCodec(t, pdu.V50)
	b, e := raw.Append(nil, p)
	if e != nil {
		t.Fatal(e)
	}
	decoded, e := raw.Decode(b)
	if e != nil || len(decoded.Body.(*pdu.SubmitSM).Optional) != 2 {
		t.Fatal(e)
	}
	for _, suffix := range [][]byte{{0x15}, {0x15, 0, 0}, {0x15, 0, 0, 2, 1}} {
		bad := append(append([]byte(nil), wire...), suffix...)
		bad[3] = byte(len(bad))
		if _, e := raw.Decode(bad); e == nil {
			t.Fatal("truncated TLV accepted")
		}
	}
	def.Tag = tlv.MessagePayload
	if _, e := codec.New(codec.Config{Version: pdu.V50, TLVs: []codec.TLVDefinition{def}}); !errors.Is(e, codec.ErrValidation) {
		t.Fatal(e)
	}
}
func TestCommandSpecificAddressBounds(t *testing.T) {
	for _, v := range []pdu.Version{pdu.V34, pdu.V50} {
		c := newCodec(t, v)
		addr := pdu.Address{Value: string(bytes.Repeat([]byte{'a'}, 21))}
		if _, e := c.Append(nil, pdu.PDU{Sequence: 1, Body: &pdu.SubmitSM{SMFields: pdu.SMFields{Source: addr}}}); e == nil {
			t.Fatal("submit address too long", v)
		}
		if _, e := c.Append(nil, pdu.PDU{Sequence: 1, Body: &pdu.DataSM{Source: addr}}); e != nil {
			t.Fatal("data address has wider bound", v, e)
		}
	}
}
