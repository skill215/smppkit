// SPDX-AI-Disclosure: ai-generated
// SPDX-AI-Model: GPT-6.0 Astra
// SPDX-AI-Provider: OpenAI
// SPDX-AI-Scope: Example program generated and revised with Codex under maintainer direction.
// SPDX-AI-Date: 2026-11-06

// Prepare opaque octets without transport; client/server examples demonstrate
// sending and receiving these plans through the public Session.SendBinary API.
package main

import (
	"bytes"
	"fmt"
	"log"

	"github.com/skill215/smppkit/message"
	"github.com/skill215/smppkit/pdu"
)

func main() {
	plan, err := message.PrepareBytes(pdu.V50, message.Binary{
		Data: bytes.Repeat([]byte{0, 255, 128}, 100), DataCoding: 4,
	}, message.BinaryPolicy{Mode: message.UDH16})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("encoding=%s dcs=%02x requests=%d reference_bits=%d\n", plan.Encoding(), plan.DataCoding(), plan.Count(), plan.ReferenceBits())
	// Preview owns its buffers. It neither allocates a reference nor sends data.
	// SendBinary performs Codec/Session preflight and reserves a reference lease.
	for i, part := range plan.Preview() {
		fmt.Printf("part=%d bytes=%d\n", i+1, len(part))
	}
}
