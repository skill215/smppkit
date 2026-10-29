// SPDX-AI-Disclosure: ai-generated
// SPDX-AI-Model: GPT-6.0 Astra
// SPDX-AI-Provider: OpenAI
// SPDX-AI-Scope: Test code generated and revised with Codex under maintainer direction.
// SPDX-AI-Date: 2026-10-29

package main

import (
	"fmt"
	"testing"

	"github.com/skill215/smppkit/pdu"
)

func TestReceiptExample(t *testing.T) {
	for _, version := range []pdu.Version{pdu.V33, pdu.V34, pdu.V50} {
		for _, command := range []pdu.CommandID{pdu.DeliverSMID, pdu.DataSMID} {
			t.Run(fmt.Sprintf("%x/%x", version, command), func(t *testing.T) {
				err := run(version, command)
				if version == pdu.V33 && command == pdu.DataSMID {
					if err == nil {
						t.Fatal("3.3 data_sm accepted")
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}
