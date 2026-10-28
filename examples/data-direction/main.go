// SPDX-AI-Disclosure: ai-generated
// SPDX-AI-Model: GPT-6.0 Astra
// SPDX-AI-Provider: OpenAI
// SPDX-AI-Scope: Example program generated and revised with Codex under maintainer direction.
// SPDX-AI-Date: 2026-10-28

// A standalone simulator supplies the original data_sm request direction.
package main

import (
	"encoding/hex"
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
	// Synthetic MC -> ESME receipt: ID "id", message_state 2. Decode validates
	// framing, fields and TLVs; the additional check supplies endpoint context.
	raw, err := hex.DecodeString("0000002600000103000000000000000700000000000000040000001e00036964000427000102")
	if err != nil {
		log.Fatal(err)
	}
	packet, err := c.Decode(raw)
	if err != nil {
		log.Fatal(err)
	}
	if err = c.ValidateDataDirection(packet.Body, codec.DataDelivery); err != nil {
		log.Fatal(err)
	}
	fmt.Println("delivery receipt accepted")
	if err = c.ValidateDataDirection(packet.Body, codec.DataSubmission); err != nil {
		fmt.Println("same receipt rejected as submission")
	} else {
		log.Fatal("expected submission rejection")
	}
	// A data_sm_resp to this request also uses DataDelivery, even though it
	// travels ESME -> MC. Session automatically handles both direction checks.
	response := pdu.PDU{Sequence: packet.Sequence, Body: &pdu.DataSMResp{}}
	encoded, err := c.Append(nil, response)
	if err != nil {
		log.Fatal(err)
	}
	if err = c.ValidateDataResponse(response, packet.Body.(*pdu.DataSM).ESMClass, codec.DataDelivery); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("response=%x\n", encoded)
}
