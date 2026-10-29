// SPDX-AI-Disclosure: ai-generated
// SPDX-AI-Model: GPT-6.0 Astra
// SPDX-AI-Provider: OpenAI
// SPDX-AI-Scope: Library code generated and revised with Codex under maintainer direction.
// SPDX-AI-Date: 2026-10-29

package codec

import (
	"encoding/binary"
	"fmt"
	"io"

	"github.com/skill215/smppkit/internal/wire"
)

func frameLimit(maxFrame int) (int, error) {
	if maxFrame == 0 {
		maxFrame = 64 << 10
	}
	if maxFrame < 16 || maxFrame > 16<<20 {
		return 0, fmt.Errorf("%w: MaxFrame", ErrValidation)
	}
	return maxFrame, nil
}

// ParseHeader extracts the first 16 bytes without validating length, command,
// status or sequence and without copying/retaining src. Extra bytes are ignored.
// Use Codec.Decode for semantic validation. A short input returns DecodeFraming
// wrapping ErrValidation, with no header and no stream-consumption claim.
func ParseHeader(src []byte) (FrameHeader, error) {
	if len(src) < 16 {
		return FrameHeader{}, decodeFailure(ErrValidation, DecodeFraming, nil, 0)
	}
	return rawHeader(src), nil
}

// ReadFrame reads one bounded raw frame without interpreting command/body fields.
// maxFrame defaults to 64 KiB; accepted limits are 16 bytes through 16 MiB.
// The length is checked after four bytes, before allocating the remaining frame.
// Invalid configuration consumes nothing. Invalid lengths consume only four
// bytes and are not drained; there is no resynchronization or automatic retry.
//
// The result owns exactly the received prefix, including on errors; len and cap
// exclude unread storage. Empty EOF returns nil data. DecodeError records exact
// progress; errors.Is/As preserve the I/O cause. An EOF cause after the length
// prefix is NOT a clean frame boundary: inspect BytesRead, not EOF alone.
// Complete acquisition does not imply semantic validity. Call Codec.Decode to
// validate, or apply an explicit application policy before forwarding raw data.
//
// The caller owns deadlines, cancellation, serialization of reads on a stream,
// and the connection lifecycle. No logging, ACK, sequencing or heartbeat occurs.
func ReadFrame(r io.Reader, maxFrame int) ([]byte, error) {
	limit, err := frameLimit(maxFrame)
	if err != nil {
		e := decodeFailure(err, DecodeFraming, nil, 0)
		e.StreamRead = true
		return nil, e
	}
	raw, err := wire.ReadFrame(r, limit)
	if err == nil {
		return raw, nil
	}
	e := decodeFailure(err, DecodeFraming, raw, limit)
	e.StreamRead = true
	e.BytesRead = len(raw)
	return raw, e
}

func validFrame(src []byte, limit int) bool {
	return len(src) >= 16 && len(src) <= limit && uint64(binary.BigEndian.Uint32(src)) == uint64(len(src))
}

// WriteFrame validates only the length/cap and makes one Write of the exact raw
// frame, without allocation/copy or semantic validation. Limits match ReadFrame.
// Malformed/unknown body fields are unchanged; forwarding policy is the caller's.
// The caller must keep src immutable until return and serialize transport writes.
//
// n is the writer's actual progress. A short write with nil error becomes
// io.ErrShortWrite; non-nil writer errors are preserved even if n == len(src).
// Invalid configuration/frame length returns n=0 and wraps ErrValidation before
// I/O. No retries or flush are performed; buffered writers require caller Flush.
// A partial write may invalidate the connection; never retry the whole frame
// merely because this function returned an error. Writer counts outside the
// io.Writer contract are rejected with io.ErrShortWrite and n=0.
func WriteFrame(w io.Writer, src []byte, maxFrame int) (n int, err error) {
	limit, err := frameLimit(maxFrame)
	if err != nil {
		return 0, err
	}
	if !validFrame(src, limit) {
		return 0, fmt.Errorf("%w: command_length", ErrValidation)
	}
	n, err = w.Write(src[:len(src):len(src)])
	if n < 0 || n > len(src) {
		return 0, io.ErrShortWrite
	}
	if err == nil && n < len(src) {
		err = io.ErrShortWrite
	}
	return n, err
}
