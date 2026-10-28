// Package textcodec implements strict text conversion. GSM7 represents unpacked
// septets, never guessed packed bytes. UCS2 rejects supplementary characters;
// UTF16BE is an explicit compatibility encoding with the same DCS value.
package textcodec

import (
	"encoding/binary"
	"errors"
	"fmt"
	"unicode/utf16"
	"unicode/utf8"
)

var ErrEncoding = errors.New("smpp: unrepresentable or invalid text")

// Encoded records safe end offsets for indivisible characters. Boundaries are
// strictly increasing and end at len(Bytes), or are empty for empty input.
type Encoded struct {
	Bytes      []byte
	Boundaries []int
}

// Encoding implementations must be concurrency safe and return owned storage.
// Decode must not mutate or retain its input. Callbacks are synchronous trusted
// code; callers must bound their work and allocation, including on errors.
// UnitBits is 7 for unpacked GSM septets, 8 for octet encodings. Every output
// byte consumes one unit; Boundaries must preserve multi-unit characters.
// DataCoding is an explicit peer/profile mapping, not version negotiation.
// Custom implementations used by message.Prepare must choose a DCS appropriate
// to that version; the planner can infer compatibility only for built-ins.
type Encoding interface {
	Name() string
	DataCoding() uint8
	UnitBits() int
	Encode(string) (Encoded, error)
	Decode([]byte) (string, error)
}

// Alphabet selects a built-in strict converter. The zero value is invalid.
type Alphabet uint8

const (
	GSM7 Alphabet = iota + 1
	Latin1
	UCS2
	UTF16BE
)

// Name is a diagnostic encoding label, not a value sent on the wire.
func (a Alphabet) Name() string {
	switch a {
	case GSM7:
		return "GSM7-unpacked"
	case Latin1:
		return "ISO-8859-1"
	case UCS2:
		return "UCS-2BE"
	case UTF16BE:
		return "UTF-16BE"
	}
	return "unknown"
}

// DataCoding returns this encoding's SMPP 3.4/5.0 DCS assignment. Latin1's
// value 3 is not a Latin-1 mapping in SMPP 3.3's GSM DCS; message.Prepare rejects
// that built-in combination. UCS2 and UTF16BE both use 8, so the receiver must
// choose its interpretation explicitly.
func (a Alphabet) DataCoding() uint8 {
	switch a {
	case Latin1:
		return 3
	case UCS2, UTF16BE:
		return 8
	}
	return 0
}

// UnitBits distinguishes unpacked septets from octets for radio budgeting.
func (a Alphabet) UnitBits() int {
	if a == GSM7 {
		return 7
	}
	return 8
}

// The alphabet and extension positions are protocol facts from 3GPP TS 23.038
// §6.2.1. No implementation code or tables are copied from another library.
const gsmAlphabet = "@£$¥èéùìòÇ\nØø\rÅåΔ_ΦΓΛΩΠΨΣΘΞ\x1bÆæßÉ !\"#¤%&'()*+,-./0123456789:;<=>?¡ABCDEFGHIJKLMNOPQRSTUVWXYZÄÖÑÜ§¿abcdefghijklmnopqrstuvwxyzäöñüà"

var gsmRunes = []rune(gsmAlphabet)
var gsmExtensions = map[byte]rune{0x0a: '\f', 0x14: '^', 0x28: '{', 0x29: '}', 0x2f: '\\', 0x3c: '[', 0x3d: '~', 0x3e: ']', 0x40: '|', 0x65: '€'}
var gsmEncode = func() map[rune][]byte {
	m := make(map[rune][]byte)
	for i, r := range gsmRunes {
		if i != 0x1b {
			m[r] = []byte{byte(i)}
		}
	}
	for i, r := range gsmExtensions {
		m[r] = []byte{0x1b, i}
	}
	return m
}()

// Encode converts valid UTF-8 without replacement characters or partial output
// on failure. Boundaries include complete GSM escapes and UTF-16 surrogate pairs.
func (a Alphabet) Encode(s string) (Encoded, error) {
	var out Encoded
	if !utf8.ValidString(s) || a < GSM7 || a > UTF16BE {
		return out, ErrEncoding
	}
	out.Bytes = make([]byte, 0, len(s))
	out.Boundaries = make([]int, 0, utf8.RuneCountInString(s))
	for _, r := range s {
		switch a {
		case GSM7:
			b, ok := gsmEncode[r]
			if !ok {
				return Encoded{}, fmt.Errorf("%w: GSM alphabet", ErrEncoding)
			}
			out.Bytes = append(out.Bytes, b...)
		case Latin1:
			if r > 255 {
				return Encoded{}, fmt.Errorf("%w: ISO-8859-1", ErrEncoding)
			}
			out.Bytes = append(out.Bytes, byte(r))
		case UCS2:
			if r > 0xffff {
				return Encoded{}, fmt.Errorf("%w: UCS-2 supplementary character", ErrEncoding)
			}
			out.Bytes = binary.BigEndian.AppendUint16(out.Bytes, uint16(r))
		case UTF16BE:
			if r > 0xffff {
				hi, lo := utf16.EncodeRune(r)
				out.Bytes = binary.BigEndian.AppendUint16(out.Bytes, uint16(hi))
				out.Bytes = binary.BigEndian.AppendUint16(out.Bytes, uint16(lo))
			} else {
				out.Bytes = binary.BigEndian.AppendUint16(out.Bytes, uint16(r))
			}
		}
		out.Boundaries = append(out.Boundaries, len(out.Bytes))
	}
	return out, nil
}

// Decode rejects malformed escapes, odd two-byte data and invalid surrogates;
// it never guesses a different encoding or returns a partially decoded string.
func (a Alphabet) Decode(b []byte) (string, error) {
	out := make([]rune, 0, len(b))
	switch a {
	case GSM7:
		for i := 0; i < len(b); i++ {
			v := b[i]
			if v > 127 {
				return "", ErrEncoding
			}
			if v == 0x1b {
				i++
				if i == len(b) {
					return "", ErrEncoding
				}
				r, ok := gsmExtensions[b[i]]
				if !ok {
					return "", ErrEncoding
				}
				out = append(out, r)
			} else {
				out = append(out, gsmRunes[v])
			}
		}
	case Latin1:
		for _, v := range b {
			out = append(out, rune(v))
		}
	case UCS2, UTF16BE:
		if len(b)%2 != 0 {
			return "", ErrEncoding
		}
		for i := 0; i < len(b); i += 2 {
			r := rune(binary.BigEndian.Uint16(b[i:]))
			if utf16.IsSurrogate(r) {
				if a == UCS2 || r >= 0xdc00 || i+4 > len(b) {
					return "", ErrEncoding
				}
				lo := rune(binary.BigEndian.Uint16(b[i+2:]))
				if lo < 0xdc00 || lo > 0xdfff {
					return "", ErrEncoding
				}
				out = append(out, utf16.DecodeRune(r, lo))
				i += 2
			} else {
				out = append(out, r)
			}
		}
	default:
		return "", ErrEncoding
	}
	return string(out), nil
}

// Validate checks boundary offsets, not alphabet membership or DCS semantics.
// It borrows e without mutation; the custom encoder still owns text correctness.
func Validate(e Encoded) error {
	last := 0
	for _, end := range e.Boundaries {
		if end <= last || end > len(e.Bytes) {
			return ErrEncoding
		}
		last = end
	}
	if last != len(e.Bytes) {
		return ErrEncoding
	}
	return nil
}
