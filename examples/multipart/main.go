// SPDX-AI-Disclosure: ai-generated
// SPDX-AI-Model: GPT-6.0 Astra
// SPDX-AI-Provider: OpenAI
// SPDX-AI-Scope: Example program generated and revised with Codex under maintainer direction.
// SPDX-AI-Date: 2026-10-28

// Prepare a message without network I/O or reserving a concatenation reference.
package main

import (
	"fmt"
	"github.com/skill215/smppkit/message"
	"github.com/skill215/smppkit/pdu"
	"log"
	"strings"
)

func main() {
	plan, e := message.Prepare(pdu.V50, message.Text{Text: strings.Repeat("Hello ^ ", 40)}, message.GSMText())
	if e != nil {
		log.Fatal(e)
	}
	fmt.Printf("encoding=%s segments=%d reference_bits=%d\n", plan.Encoding(), plan.Count(), plan.ReferenceBits())
	for i, b := range plan.Preview() {
		fmt.Printf("segment=%d encoded_units=%d\n", i+1, len(b))
	}
}
