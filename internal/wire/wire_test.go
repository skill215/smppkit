// SPDX-AI-Disclosure: ai-generated
// SPDX-AI-Model: GPT-6.0 Astra
// SPDX-AI-Provider: OpenAI
// SPDX-AI-Scope: Test code generated and revised with Codex under maintainer direction.
// SPDX-AI-Date: 2026-10-28

package wire

import (
	"bytes"
	"testing"
)

func TestWriterLimitBeforeCopy(t *testing.T) {
	for _, limit := range []int{16, 17, 63, 1024} {
		w := Writer{Data: make([]byte, 16), Limit: limit}
		w.Bytes(bytes.Repeat([]byte{1}, limit-16))
		if w.Err != nil || len(w.Data) != limit || cap(w.Data) > limit {
			t.Fatalf("limit=%d len=%d cap=%d err=%v", limit, len(w.Data), cap(w.Data), w.Err)
		}
		before := bytes.Clone(w.Data)
		w.Bytes(make([]byte, 65535))
		w.U8(1)
		w.C("x", 2, "field")
		if w.Err != ErrLimit || !bytes.Equal(w.Data, before) || cap(w.Data) > limit {
			t.Fatal("writer grew after reaching limit")
		}
	}
}
