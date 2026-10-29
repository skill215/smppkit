// SPDX-AI-Disclosure: ai-generated
// SPDX-AI-Model: GPT-6.0 Astra
// SPDX-AI-Provider: OpenAI
// SPDX-AI-Scope: Library code generated and revised with Codex under maintainer direction.
// SPDX-AI-Date: 2026-10-29

package codec

import (
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/skill215/smppkit/internal/wire"
	"github.com/skill215/smppkit/pdu"
)

// DecodeErrorKind classifies a failure without parsing diagnostic strings.
// It is open-ended: callers must tolerate future categories. Zero is unspecified.
type DecodeErrorKind uint8

const (
	DecodeFraming     DecodeErrorKind = iota + 1 // size/configuration or frame acquisition
	DecodeUnsupported                            // known unsupported command or version
	DecodeMandatory                              // mandatory field structure, including C-octets and trailing bytes
	DecodeTLV                                    // optional-section structure or validation
	DecodeSemantic                               // header/body semantics outside the optional section
	DecodeExtension                              // registered command callback failed or returned the wrong body
)

// FrameHeader contains raw numeric header fields. Presence alone does not imply
// valid semantics, a complete frame, permission to respond, or a bound session.
type FrameHeader struct {
	Length   uint32
	Command  pdu.CommandID
	Status   pdu.Status
	Sequence uint32
}

// DecodeError is returned by Decode, Read, ReadBind, ReadFrame and ParseHeader.
// Unwrap preserves existing sentinels, FieldError and transport/extension causes.
// Error prints only safe numeric context, never the cause's arbitrary text.
// The cause can still contain application/transport data; do not log it blindly.
// Fields are diagnostic snapshots; a failed PDU is not usable protocol input.
type DecodeError struct {
	Kind      DecodeErrorKind
	Header    FrameHeader
	HasHeader bool // all 16 header bytes were available
	// HeaderValid means declared length is within the configured bounds and
	// sequence/status rules pass. It does NOT assert a supported command/version,
	// matching supplied byte length, valid body or successful stream acquisition.
	HeaderValid bool
	// StreamRead is true only for ReadFrame, Codec.Read and ReadBind failures.
	// Decode/ParseHeader do not consume a stream and leave these three fields zero.
	StreamRead    bool
	BytesRead     int  // exact bytes consumed by this call, not allocated buffer length
	FrameComplete bool // StreamRead and exactly the declared frame acquired
	cause         error
}

func (e *DecodeError) Error() string {
	return fmt.Sprintf("smpp: decode failure kind=%d header=%t command=0x%08x sequence=%d", e.Kind, e.HasHeader, e.Header.Command, e.Header.Sequence)
}
func (e *DecodeError) Unwrap() error { return e.cause }

// categorizedError marks the site of a failure, not words in its message. It is
// removed when constructing the public snapshot; existing causes remain intact.
type categorizedError struct {
	kind  DecodeErrorKind
	cause error
}

func (e *categorizedError) Error() string { return "smpp: classified decode failure" }
func (e *categorizedError) Unwrap() error { return e.cause }

func decodeFailure(err error, kind DecodeErrorKind, src []byte, limit int) *DecodeError {
	if marked, ok := err.(*categorizedError); ok {
		kind, err = marked.kind, marked.cause
	}
	if kind == 0 {
		switch {
		case errors.Is(err, ErrUnsupported):
			kind = DecodeUnsupported
		default:
			if _, ok := errors.AsType[*wire.Error](err); ok {
				kind = DecodeMandatory
			} else {
				kind = DecodeSemantic
			}
		}
	}
	e := &DecodeError{Kind: kind, cause: err}
	if len(src) >= 16 {
		e.HasHeader = true
		e.Header = rawHeader(src)
		e.HeaderValid = limit >= 16 && limit <= 16<<20 && e.Header.Length >= 16 && uint64(e.Header.Length) <= uint64(limit) && header(e.Header.Command, e.Header.Sequence, e.Header.Status) == nil
	}
	return e
}
func rawHeader(src []byte) FrameHeader {
	return FrameHeader{binary.BigEndian.Uint32(src), pdu.CommandID(binary.BigEndian.Uint32(src[4:])), pdu.Status(binary.BigEndian.Uint32(src[8:])), binary.BigEndian.Uint32(src[12:])}
}

// readFailure copies our error snapshot rather than modifying an error that may
// be retained by an extension. Raw acquisition errors already carry this context.
func readFailure(err error, n int, complete bool) error {
	e := *err.(*DecodeError) // only called on errors produced by our Decode/bootstrap
	e.StreamRead = true
	e.BytesRead = n
	e.FrameComplete = complete
	return &e
}
