// SPDX-AI-Disclosure: ai-generated
// SPDX-AI-Model: GPT-6.0 Astra
// SPDX-AI-Provider: OpenAI
// SPDX-AI-Scope: Example program generated and revised with Codex under maintainer direction.
// SPDX-AI-Date: 2026-10-28

// A simulator retains the data_sm request mode for response validation.
package main

import (
	"encoding/hex"
	"errors"
	"fmt"
	"log"

	"github.com/skill215/smppkit/codec"
	"github.com/skill215/smppkit/pdu"
)

func main() {
	c, err := codec.New(codec.Config{Version: pdu.V50})
	if err != nil {
		log.Fatal(err)
	}
	request := &pdu.DataSM{ESMClass: 2}
	if _, err := c.Append(nil, pdu.PDU{Sequence: 7, Body: request}); err != nil {
		log.Fatal(err)
	}
	if err := c.ValidateDataDirection(request, codec.DataSubmission); err != nil {
		log.Fatal(err)
	}
	// Keep a scalar copy, not a pointer to application-owned request state.
	esm := request.ESMClass
	raw, err := hex.DecodeString("0000002380000103000000fe000000076d000425000100042300030300010420000101")
	if err != nil {
		log.Fatal(err)
	}
	response, err := c.Decode(raw)
	if err != nil {
		log.Fatal(err)
	}
	// A standalone runtime must correlate the command and sequence itself.
	if response.Sequence != 7 || response.Body.CommandID() != pdu.DataSMRespID {
		log.Fatal("unexpected response identity")
	}
	if err := c.ValidateDataResponse(response, esm, codec.DataSubmission); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("transaction response validated: status=0x%08x\n", response.Status)
	if err := c.ValidateDataResponse(response, 3, codec.DataSubmission); !errors.Is(err, codec.ErrValidation) {
		log.Fatal("expected store-and-forward rejection", err)
	}
	fmt.Println("same TLVs rejected for explicit store-and-forward")
	// Validating a response does not turn status 0xfe into success. Session
	// returns a PeerError with ResponseReceived and never retries automatically.
}
