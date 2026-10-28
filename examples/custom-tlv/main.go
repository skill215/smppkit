// SPDX-AI-Disclosure: ai-generated
// SPDX-AI-Model: GPT-6.0 Astra
// SPDX-AI-Provider: OpenAI
// SPDX-AI-Scope: Example program generated and revised with Codex under maintainer direction.
// SPDX-AI-Date: 2026-10-28

// Customer TLV definitions are scoped to one immutable codec.
package main

import (
	"fmt"
	"github.com/skill215/smppkit/codec"
	"github.com/skill215/smppkit/pdu"
	"github.com/skill215/smppkit/tlv"
	"log"
)

func main() {
	c, e := codec.New(codec.Config{Version: pdu.V50, TLVs: []codec.TLVDefinition{{Tag: 0x1500, MinLength: 1, MaxLength: 16, Versions: []pdu.Version{pdu.V50}, Commands: []pdu.CommandID{pdu.SubmitSMID}}}})
	if e != nil {
		log.Fatal(e)
	}
	wire, e := c.Append(nil, pdu.PDU{Sequence: 1, Body: &pdu.SubmitSM{SMFields: pdu.SMFields{ShortMessage: []byte("hello"), Optional: tlv.Fields{{Tag: 0x1500, Value: []byte("example")}}}}})
	if e != nil {
		log.Fatal(e)
	}
	fmt.Printf("encoded %d octets\n", len(wire))
}
