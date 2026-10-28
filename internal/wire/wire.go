// SPDX-AI-Disclosure: ai-generated
// SPDX-AI-Model: GPT-6.0 Astra
// SPDX-AI-Provider: OpenAI
// SPDX-AI-Scope: Library code generated and revised with Codex under maintainer direction.
// SPDX-AI-Date: 2026-10-28

// Package wire contains bounded, unexported protocol primitives.
package wire

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"strings"
)

// Error contains location and field metadata, never the field value.
type Error struct {
	Field  string
	Offset int
	Reason string
}

func (e *Error) Error() string {
	return fmt.Sprintf("smpp: field %s at byte %d: %s", e.Field, e.Offset, e.Reason)
}

// Reader borrows a bounded body slice. The first error is sticky; field readers
// then return zero values, so fixed-layout decoding can report one safe location.
type Reader struct {
	Data []byte
	Pos  int
	Err  error
}

func (r *Reader) Fail(f, why string) {
	if r.Err == nil {
		r.Err = &Error{f, 16 + r.Pos, why}
	}
}
func (r *Reader) Bytes(n int, f string) []byte {
	if r.Err != nil {
		return nil
	}
	if n < 0 || n > len(r.Data)-r.Pos {
		r.Fail(f, "truncated")
		return nil
	}
	// Fields can share the owned decoded body, but not append capacity. Without
	// this bound, appending to one field can overwrite the next field's value.
	// This also isolates empty fields without allocating another payload copy.
	v := r.Data[r.Pos : r.Pos+n : r.Pos+n]
	r.Pos += n
	return v
}
func (r *Reader) U8(f string) byte {
	b := r.Bytes(1, f)
	if len(b) == 0 {
		return 0
	}
	return b[0]
}

// C consumes an ASCII, NUL-terminated field whose maximum includes the terminator.
// This rule applies to C-octets, not opaque short_message or TLV octet strings.
func (r *Reader) C(max int, f string) string {
	if r.Err != nil {
		return ""
	}
	b := r.Data[r.Pos:]
	n := bytes.IndexByte(b, 0)
	if n < 0 || n >= max {
		r.Fail(f, "missing NUL or exceeds C-octet bound")
		return ""
	}
	for _, value := range b[:n] {
		if value > 127 {
			r.Fail(f, "non-ASCII C-octet")
			return ""
		}
	}
	r.Pos += n + 1
	return string(b[:n])
}
func (r *Reader) End() error {
	if r.Err == nil && r.Pos != len(r.Data) {
		r.Fail("body", "trailing bytes")
	}
	return r.Err
}

var ErrLimit = errors.New("smpp: frame exceeds MaxFrame")

// Writer refuses growth before copying. Limit includes the frame header.
type Writer struct {
	Data  []byte
	Err   error
	Limit int
}

// reserve checks length before growth and clamps capacity as well. Letting
// append choose capacity could otherwise allocate beyond the configured cap.
func (w *Writer) reserve(n int) bool {
	if w.Err != nil {
		return false
	}
	if n < 0 || n > w.Limit-len(w.Data) {
		w.Err = ErrLimit
		return false
	}
	if len(w.Data)+n > cap(w.Data) {
		capacity := max(len(w.Data)+n, 2*cap(w.Data))
		capacity = min(capacity, w.Limit)
		data := make([]byte, len(w.Data), capacity)
		copy(data, w.Data)
		w.Data = data
	}
	return true
}
func (w *Writer) Bytes(v []byte) {
	if w.reserve(len(v)) {
		w.Data = append(w.Data, v...)
	}
}

func (w *Writer) C(v string, max int, f string) {
	if w.Err != nil {
		return
	}
	if len(v) >= max || strings.IndexByte(v, 0) >= 0 {
		w.Err = &Error{f, len(w.Data), "invalid C-octet length or embedded NUL"}
		return
	}
	for i := range len(v) {
		if v[i] > 127 {
			w.Err = &Error{f, len(w.Data), "non-ASCII C-octet"}
			return
		}
	}
	if w.reserve(len(v) + 1) {
		w.Data = append(w.Data, v...)
		w.Data = append(w.Data, 0)
	}
}
func (w *Writer) U8(v byte) {
	if w.reserve(1) {
		w.Data = append(w.Data, v)
	}
}

// ReadFrame reads only the four-byte length before validating the allocation.
// It imposes no deadlines itself; the owner of the transport must supply them.
func ReadFrame(r io.Reader, max int) ([]byte, error) {
	var size [4]byte
	if _, err := io.ReadFull(r, size[:]); err != nil {
		return nil, err
	}
	n := binary.BigEndian.Uint32(size[:])
	if n < 16 || uint64(n) > uint64(max) {
		return nil, &Error{"command_length", 0, "outside frame limit"}
	}
	b := make([]byte, int(n))
	copy(b, size[:])
	_, err := io.ReadFull(r, b[4:])
	return b, err
}
