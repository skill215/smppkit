// SPDX-AI-Disclosure: ai-generated
// SPDX-AI-Model: GPT-6.0 Astra
// SPDX-AI-Provider: OpenAI
// SPDX-AI-Scope: Example code generated and revised with Codex under maintainer direction.
// SPDX-AI-Date: 2026-11-06

// diagnostics demonstrates stable field/rule inspection in v1.2.0 development.
// Run from the source checkout: go run ./examples/diagnostics
package main

import (
	"errors"
	"fmt"

	"github.com/skill215/smppkit/codec"
	"github.com/skill215/smppkit/pdu"
)

func main() {
	c, err := codec.New(codec.Config{Version: pdu.V34})
	if err != nil {
		panic(err)
	}
	_, err = c.Append(nil, pdu.PDU{Sequence: 1, Body: &pdu.SubmitSM{
		SMFields: pdu.SMFields{Source: pdu.Address{TON: 7}}, // synthetic invalid TON
	}})
	if diagnostic, ok := errors.AsType[*codec.SemanticError](err); ok {
		fmt.Printf("field=%s rule=%s validation=%t\n", diagnostic.Field, diagnostic.Rule, errors.Is(err, codec.ErrValidation))
		if diagnostic.HasTag {
			fmt.Printf("tag=0x%04x\n", diagnostic.Tag)
		}
		if diagnostic.HasIndex {
			fmt.Printf("index=%d\n", diagnostic.Index)
		}
		// An application may branch on documented identifiers. Keep a fallback
		// for future fields/rules; do not derive an SMPP status or retry from this.
		return
	}
	if err != nil {
		// Structural, transport and extension failures have other contracts.
		// Avoid printing arbitrary causes, which may contain application data.
		fmt.Println("failure without built-in semantic field information")
	}
}
