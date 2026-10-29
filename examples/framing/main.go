// SPDX-AI-Disclosure: ai-generated
// SPDX-AI-Model: GPT-6.0 Astra
// SPDX-AI-Provider: OpenAI
// SPDX-AI-Scope: Example code generated and revised with Codex under maintainer direction.
// SPDX-AI-Date: 2026-10-29

// Framing demonstrates transport-independent raw acquisition and explicit policy.
// Run: go run ./examples/framing
package main

import (
	"bytes"
	"errors"
	"fmt"

	"github.com/skill215/smppkit/codec"
	"github.com/skill215/smppkit/pdu"
)

func main() {
	// A complete submit_sm frame with only service_type: framing succeeds while
	// mandatory-field decoding fails. Followed by a valid enquiry on the stream.
	raw := []byte{0, 0, 0, 17, 0, 0, 0, 4, 0, 0, 0, 0, 0, 0, 0, 7, 0,
		0, 0, 0, 16, 0, 0, 0, 21, 0, 0, 0, 0, 0, 0, 0, 8}
	input := bytes.NewReader(raw)
	frame, err := codec.ReadFrame(input, 1024)
	if err != nil {
		panic(err)
	}
	h, err := codec.ParseHeader(frame)
	if err != nil {
		panic(err)
	}
	fmt.Printf("raw frame: bytes=%d command=%08x sequence=%d\n", len(frame), h.Command, h.Sequence)
	c, err := codec.New(codec.Config{Version: pdu.V34})
	if err != nil {
		panic(err)
	}
	_, err = c.Decode(frame)
	if detail, ok := errors.AsType[*codec.DecodeError](err); ok {
		fmt.Printf("typed decode rejected: kind=%d header_valid=%t stream_read=%t\n", detail.Kind, detail.HeaderValid, detail.StreamRead)
	}
	// This is an explicit example policy: discard the fully acquired invalid
	// request, generate no reply, and inspect the next frame. A real endpoint may
	// instead close the transport; header availability alone never authorizes ACK.
	next, err := codec.ReadFrame(input, 1024)
	if err != nil {
		panic(err)
	}
	if _, err = c.Decode(next); err != nil {
		panic(err)
	}
	var output bytes.Buffer
	n, err := codec.WriteFrame(&output, next, 1024)
	if err != nil {
		panic(err)
	}
	fmt.Printf("next frame: written=%d unchanged=%t\n", n, bytes.Equal(next, output.Bytes()))
}
