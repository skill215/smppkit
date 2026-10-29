// SPDX-AI-Disclosure: ai-generated
// SPDX-AI-Model: GPT-6.0 Astra
// SPDX-AI-Provider: OpenAI
// SPDX-AI-Scope: Test code generated and revised with Codex under maintainer direction.
// SPDX-AI-Date: 2026-10-29

package codec_test

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"testing/iotest"

	"github.com/skill215/smppkit/codec"
	"github.com/skill215/smppkit/pdu"
)

func decodeDetail(t testing.TB, err error, kind codec.DecodeErrorKind) *codec.DecodeError {
	t.Helper()
	d, ok := errors.AsType[*codec.DecodeError](err)
	if !ok || d.Kind != kind {
		t.Fatalf("error=%v detail=%+v want kind=%v", err, d, kind)
	}
	return d
}

// wireFrame builds synthetic headers independently of Codec.Append. Body bytes
// are literal wire fields, so tests don't rely on our codec agreeing with itself.
func wireFrame(id uint32, body []byte) []byte {
	b := make([]byte, 16+len(body))
	binary.BigEndian.PutUint32(b, uint32(len(b)))
	binary.BigEndian.PutUint32(b[4:], id)
	binary.BigEndian.PutUint32(b[12:], 7)
	copy(b[16:], body)
	return b
}

func TestRawFramePartialProgress(t *testing.T) {
	raw := unhex(t, "00000014 00010200 00000000 00000007 deadbeef")
	for end := 0; end < len(raw); end++ {
		t.Run(fmt.Sprint(end), func(t *testing.T) {
			src := bytes.Clone(raw[:end])
			got, err := codec.ReadFrame(iotest.OneByteReader(bytes.NewReader(src)), 64)
			d := decodeDetail(t, err, codec.DecodeFraming)
			if !bytes.Equal(got, src) || cap(got) != len(got) || !d.StreamRead || d.BytesRead != end || d.FrameComplete || d.HasHeader != (end >= 16) || d.HeaderValid != (end >= 16) {
				t.Fatalf("end=%d raw=%x detail=%+v", end, got, d)
			}
			cause := io.ErrUnexpectedEOF
			if end == 0 || end == 4 {
				cause = io.EOF
			}
			if !errors.Is(err, cause) {
				t.Fatalf("lost I/O cause: %v", err)
			}
			if end == 0 && got != nil {
				t.Fatal("clean EOF must return nil")
			}
			if end > 0 {
				src[0] ^= 0xff
				if got[0] != raw[0] {
					t.Fatal("borrowed reader storage")
				}
			}
		})
	}
}

type failIfRead struct{ t testing.TB }

func (r failIfRead) Read([]byte) (int, error) {
	r.t.Fatal("read before validating limit")
	return 0, io.EOF
}
func TestRawFrameBoundsBeforeAllocation(t *testing.T) {
	for _, limit := range []int{-1, 1, 15, 16<<20 + 1} {
		b, err := codec.ReadFrame(failIfRead{t}, limit)
		d := decodeDetail(t, err, codec.DecodeFraming)
		if b != nil || d.BytesRead != 0 || d.HasHeader || !errors.Is(err, codec.ErrValidation) {
			t.Fatal(d)
		}
	}
	for _, length := range []uint32{0, 1, 15, 65, 0xffffffff} {
		raw := make([]byte, 20)
		binary.BigEndian.PutUint32(raw, length)
		r := bytes.NewReader(raw)
		got, err := codec.ReadFrame(r, 64)
		d := decodeDetail(t, err, codec.DecodeFraming)
		var field *codec.FieldError
		if len(got) != 4 || cap(got) != 4 || r.Len() != 16 || d.BytesRead != 4 || d.HasHeader || d.FrameComplete || !errors.As(err, &field) || field.Field != "command_length" {
			t.Fatalf("length=%d detail=%+v", length, d)
		}
	}
	for _, size := range []int{16, 64, 65536} {
		raw := wireFrame(0x10200, make([]byte, size-16))
		got, err := codec.ReadFrame(bytes.NewReader(raw), size)
		if err != nil || !bytes.Equal(got, raw) || len(got) != cap(got) {
			t.Fatalf("boundary %d: %v", size, err)
		}
	}
	// Zero uses the default cap, not unlimited allocation.
	raw := wireFrame(0x10200, make([]byte, 65537-16))
	r := bytes.NewReader(raw)
	if _, err := codec.ReadFrame(r, 0); err == nil || r.Len() != len(raw)-4 {
		t.Fatal("default cap ignored")
	}
}

func TestRawFramePreservesInvalidSemanticsAndCoalescedInput(t *testing.T) {
	a := unhex(t, "00000014 00000004 00000008 ffffffff ff002500")
	b := unhex(t, "00000010 00000015 00000000 00000008")
	input := append(bytes.Clone(a), b...)
	r := iotest.HalfReader(bytes.NewReader(input))
	for _, want := range [][]byte{a, b} {
		got, err := codec.ReadFrame(r, 0)
		if err != nil || !bytes.Equal(got, want) {
			t.Fatalf("frame: %x %v", got, err)
		}
		var out bytes.Buffer
		if n, err := codec.WriteFrame(&out, got, 0); err != nil || n != len(want) || !bytes.Equal(out.Bytes(), want) {
			t.Fatal("raw bytes changed")
		}
	}
	h, err := codec.ParseHeader(a)
	if err != nil || h.Length != 20 || h.Command != pdu.SubmitSMID || h.Status != 8 || h.Sequence != 0xffffffff {
		t.Fatalf("raw header %+v %v", h, err)
	}
	_, err = newCodec(t, pdu.V34).Decode(a)
	d := decodeDetail(t, err, codec.DecodeSemantic)
	if !d.HasHeader || d.HeaderValid {
		t.Fatal("unvalidated header marked valid")
	}
	_, err = codec.ParseHeader(a[:15])
	d = decodeDetail(t, err, codec.DecodeFraming)
	if !errors.Is(err, codec.ErrValidation) || d.HasHeader || d.StreamRead {
		t.Fatal(d)
	}
}

type countedWriter struct {
	n, calls int
	err      error
	received []byte
}

func (w *countedWriter) Write(b []byte) (int, error) {
	w.calls++
	w.received = bytes.Clone(b)
	return w.n, w.err
}
func TestWriteRawFrameProgress(t *testing.T) {
	raw := unhex(t, "00000010 00000015 00000000 00000007")
	transport := errors.New("transport failure")
	for _, tc := range []struct {
		n         int
		err, want error
	}{{16, nil, nil}, {3, nil, io.ErrShortWrite}, {0, nil, io.ErrShortWrite}, {3, transport, transport}, {16, transport, transport}, {-1, nil, io.ErrShortWrite}, {17, nil, io.ErrShortWrite}} {
		w := &countedWriter{n: tc.n, err: tc.err}
		n, err := codec.WriteFrame(w, raw, 16)
		wantN := tc.n
		if wantN < 0 || wantN > 16 {
			wantN = 0
		}
		if n != wantN || !errors.Is(err, tc.want) || w.calls != 1 || !bytes.Equal(w.received, raw) {
			t.Fatalf("n=%d err=%v calls=%d", n, err, w.calls)
		}
	}
	for _, tc := range []struct {
		raw   []byte
		limit int
	}{{raw, -1}, {raw, 15}, {raw[:15], 0}, {append(bytes.Clone(raw), 0), 0}} {
		w := &countedWriter{}
		n, err := codec.WriteFrame(w, tc.raw, tc.limit)
		if n != 0 || !errors.Is(err, codec.ErrValidation) || w.calls != 0 {
			t.Fatal("invalid frame reached writer")
		}
	}
}

func TestDecodeErrorCategoriesAndReadContext(t *testing.T) {
	// Seventeen zero-valued mandatory fields form an empty 3.4 submit_sm.
	empty := make([]byte, 17)
	semantic := bytes.Clone(empty)
	semantic[14] = 0x0b // reserved data_coding
	for _, tc := range []struct {
		name  string
		raw   []byte
		kind  codec.DecodeErrorKind
		cause error
		field bool
	}{
		{"mandatory", wireFrame(4, []byte{0}), codec.DecodeMandatory, nil, true},
		{"tlv header", wireFrame(4, append(bytes.Clone(empty), 0x15)), codec.DecodeTLV, nil, true},
		{"tlv value", wireFrame(4, append(bytes.Clone(empty), 0x15, 0, 0, 2, 1)), codec.DecodeTLV, nil, true},
		{"tlv duplicate", wireFrame(4, append(bytes.Clone(empty), 4, 0x24, 0, 0, 4, 0x24, 0, 0)), codec.DecodeTLV, codec.ErrValidation, false},
		{"semantic", wireFrame(4, semantic), codec.DecodeSemantic, codec.ErrValidation, false},
		{"unsupported", wireFrame(0x111, nil), codec.DecodeUnsupported, codec.ErrUnsupported, false},
		{"missing success body", wireFrame(0x80000004, nil), codec.DecodeMandatory, nil, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := newCodec(t, pdu.V34)
			packet, err := c.Decode(tc.raw)
			d := decodeDetail(t, err, tc.kind)
			if !d.HasHeader || !d.HeaderValid || d.StreamRead || d.FrameComplete || d.BytesRead != 0 || d.Header.Command != pdu.CommandID(binary.BigEndian.Uint32(tc.raw[4:])) || packet.Sequence != 7 || packet.Body != nil {
				t.Fatalf("detail=%+v packet=%+v", d, packet)
			}
			if tc.cause != nil && !errors.Is(err, tc.cause) {
				t.Fatal("lost sentinel")
			}
			if tc.field {
				f, ok := errors.AsType[*codec.FieldError](err)
				if !ok || f.Offset < 16 {
					t.Fatal("lost field context")
				}
				if errors.Is(err, codec.ErrValidation) {
					t.Fatal("changed structural error identity")
				}
			}
			next := unhex(t, "00000010 00000015 00000000 00000008")
			r := bytes.NewReader(append(bytes.Clone(tc.raw), next...))
			_, err = c.Read(r)
			d = decodeDetail(t, err, tc.kind)
			if !d.StreamRead || !d.FrameComplete || d.BytesRead != len(tc.raw) || r.Len() != len(next) {
				t.Fatalf("lost consumption %+v", d)
			}
			p, err := c.Read(r)
			if err != nil || p.Sequence != 8 {
				t.Fatalf("next frame: %v", err)
			}
		})
	}
	c := newCodec(t, pdu.V34)
	for _, raw := range [][]byte{nil, {0, 0, 0, 16}, append(wireFrame(0x15, nil), 0)} {
		_, err := c.Decode(raw)
		d := decodeDetail(t, err, codec.DecodeFraming)
		if !errors.Is(err, codec.ErrValidation) || d.StreamRead || d.FrameComplete {
			t.Fatal(d)
		}
	}
	// A whole frame need not be semantically valid; a header can be complete even
	// when its body is short. Those are independent pieces of diagnostic evidence.
	partial := wireFrame(4, empty)[:18]
	_, err := c.Read(bytes.NewReader(partial))
	d := decodeDetail(t, err, codec.DecodeFraming)
	if !d.HasHeader || !d.HeaderValid || d.FrameComplete || d.BytesRead != 18 {
		t.Fatal(d)
	}
}

func TestUnknownAndNegativeResponseContracts(t *testing.T) {
	for _, v := range []pdu.Version{pdu.V33, pdu.V34, pdu.V50} {
		c := newCodec(t, v)
		p, err := c.Decode(wireFrame(0x10200, []byte{0, 255}))
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := p.Body.(*pdu.Opaque); !ok {
			t.Fatal("unknown command became an error")
		}
		raw := unhex(t, "00000010 80000004 00000058 00000007")
		p, err = c.Decode(raw)
		if v == pdu.V33 {
			decodeDetail(t, err, codec.DecodeMandatory)
		} else if err != nil {
			t.Fatal(err)
		} else if _, ok := p.Body.(*pdu.Omitted); !ok {
			t.Fatal("lost omitted negative response")
		}
	}
}

type failingCommand struct {
	extension
	cause error
}

func (x failingCommand) DecodeBody([]byte) (pdu.Body, error) { return nil, x.cause }
func TestExtensionErrorSafeContextAndIdentity(t *testing.T) {
	cause := errors.New("private payload/password must not appear")
	def := codec.CommandDefinition{Request: 0x10200, Response: 0x80010200, Versions: []pdu.Version{pdu.V34}, RequestCodec: failingCommand{cause: cause}, ResponseCodec: extension{id: 0x80010200}}
	c, err := codec.New(codec.Config{Version: pdu.V34, Commands: []codec.CommandDefinition{def}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.Decode(wireFrame(0x10200, []byte("private payload")))
	d := decodeDetail(t, err, codec.DecodeExtension)
	if !errors.Is(err, cause) || strings.Contains(err.Error(), "private") || strings.Contains(err.Error(), "password") || d.Header.Command != 0x10200 {
		t.Fatal("unsafe diagnostics or lost cause")
	}
	def.RequestCodec = extension{id: 0x10200, wrong: true}
	c, _ = codec.New(codec.Config{Version: pdu.V34, Commands: []codec.CommandDefinition{def}})
	_, err = c.Decode(wireFrame(0x10200, nil))
	decodeDetail(t, err, codec.DecodeExtension)
	if !errors.Is(err, codec.ErrValidation) {
		t.Fatal("wrong extension body lost validation cause")
	}
}

func TestBindBootstrapErrorContext(t *testing.T) {
	for _, tc := range []struct {
		raw   []byte
		kind  codec.DecodeErrorKind
		field bool
	}{
		{wireFrame(9, []byte{0}), codec.DecodeMandatory, true},
		{wireFrame(9, []byte{0, 0, 0, 0x60, 0, 0, 0}), codec.DecodeUnsupported, false},
		{wireFrame(0x10200, []byte{0, 0, 0, 0x34, 0, 0, 0}), codec.DecodeSemantic, false},
	} {
		_, _, err := codec.ReadBind(bytes.NewReader(tc.raw), 0)
		d := decodeDetail(t, err, tc.kind)
		if !d.HasHeader || !d.HeaderValid || !d.StreamRead || !d.FrameComplete || d.BytesRead != len(tc.raw) {
			t.Fatal(d)
		}
		if tc.field {
			if _, ok := errors.AsType[*codec.FieldError](err); !ok {
				t.Fatal("lost bootstrap field")
			}
		}
	}
	_, _, err := codec.ReadBind(bytes.NewReader([]byte{0, 0}), 0)
	d := decodeDetail(t, err, codec.DecodeFraming)
	if d.BytesRead != 2 || d.FrameComplete || d.HasHeader {
		t.Fatal(d)
	}
}

func TestRawFrameConcurrentCodecAndOwnedResults(t *testing.T) {
	c := newCodec(t, pdu.V34)
	raw := wireFrame(0x10200, []byte{1, 2, 3})
	bad := wireFrame(4, nil)
	var wg sync.WaitGroup
	for range 12 {
		wg.Go(func() {
			for range 40 {
				got, err := codec.ReadFrame(bytes.NewReader(raw), 0)
				if err != nil {
					t.Error(err)
					return
				}
				p, err := c.Decode(got)
				if err != nil {
					t.Error(err)
					return
				}
				got[16] = 9
				if p.Body.(*pdu.Opaque).Data[0] != 1 {
					t.Error("decoded body borrowed raw frame")
				}
				_, err = c.Read(bytes.NewReader(bad))
				d, ok := errors.AsType[*codec.DecodeError](err)
				if !ok || !d.FrameComplete || d.Header.Command != 4 {
					t.Error("shared diagnostic state")
				}
			}
		})
	}
	wg.Wait()
	if raw[16] != 1 {
		t.Fatal("reader borrowed shared source")
	}
}

func TestRawFrameTypedAddressModification(t *testing.T) {
	// Binary UDHI content and two identical unknown TLVs must survive a deliberate
	// address edit. Both original and expected frames are independent wire bytes.
	raw := unhex(t, "00000031 00000004 00000000 00000007 00 0000 00 0000 3100 40 00 00 00 00 00 00 04 00 05 037001ffff 15000001aa 15000001bb")
	want := unhex(t, "00000033 00000004 00000000 00000007 00 0000 00 0000 32323200 40 00 00 00 00 00 00 04 00 05 037001ffff 15000001aa 15000001bb")
	c := newCodec(t, pdu.V34)
	p, err := c.Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	p.Body.(*pdu.SubmitSM).Destination.Value = "222"
	out, err := c.Append(nil, p)
	if err != nil || !bytes.Equal(out, want) {
		t.Fatalf("modified frame=%x error=%v", out, err)
	}
}

func FuzzRawFrame(f *testing.F) {
	for _, b := range [][]byte{nil, {0, 0, 0, 15}, wireFrame(0x15, nil), wireFrame(0x10200, []byte{255, 0}), wireFrame(4, make([]byte, 17))} {
		f.Add(b)
	}
	f.Fuzz(func(t *testing.T, src []byte) {
		if len(src) > 8192 {
			return
		}
		before := bytes.Clone(src)
		r := bytes.NewReader(src)
		raw, err := codec.ReadFrame(r, 4096)
		if len(raw) != len(src)-r.Len() || cap(raw) != len(raw) || !bytes.Equal(raw, src[:len(raw)]) || !bytes.Equal(before, src) {
			t.Fatal("prefix/accounting violation")
		}
		if err != nil {
			d := decodeDetail(t, err, codec.DecodeFraming)
			if !d.StreamRead || d.BytesRead != len(raw) || d.FrameComplete {
				t.Fatal(d)
			}
			return
		}
		if len(raw) < 16 || len(raw) > 4096 || int(binary.BigEndian.Uint32(raw)) != len(raw) {
			t.Fatal("successful invalid frame")
		}
		var out bytes.Buffer
		n, e := codec.WriteFrame(&out, raw, 4096)
		if e != nil || n != len(raw) || !bytes.Equal(out.Bytes(), raw) {
			t.Fatal("raw round trip")
		}
		c := newCodec(t, pdu.V34)
		_, e = c.Decode(raw)
		if e != nil {
			d, ok := errors.AsType[*codec.DecodeError](e)
			if !ok || d.StreamRead || !d.HasHeader {
				t.Fatal("missing decode context")
			}
		}
	})
}

func TestReadFramePreservesTransportFailure(t *testing.T) {
	cause := errors.New("transport interrupted")
	raw := wireFrame(4, make([]byte, 17))
	for _, end := range []int{0, 2, 4, 12, 18} {
		input := io.MultiReader(bytes.NewReader(raw[:end]), iotest.ErrReader(cause))
		got, err := codec.ReadFrame(input, 0)
		d := decodeDetail(t, err, codec.DecodeFraming)
		if !errors.Is(err, cause) || d.BytesRead != end || !bytes.Equal(got, raw[:end]) || d.FrameComplete {
			t.Fatalf("end=%d detail=%+v", end, d)
		}
	}
}
