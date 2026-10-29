// SPDX-AI-Disclosure: ai-generated
// SPDX-AI-Model: GPT-6.0 Astra
// SPDX-AI-Provider: OpenAI
// SPDX-AI-Scope: Library code generated and revised with Codex under maintainer direction.
// SPDX-AI-Date: 2026-10-29

package message

import "errors"

// ErrUnsupportedNationalLanguage reports an unsupported UDH language-table
// declaration (IE 0x24 or 0x25). Prepare rejects these before encoding. Receive
// reports this in Message.DecodeError only when text decoding was requested;
// raw payload and per-segment provenance remain available. This also applies
// to custom encoders and explicit default-table declarations. Raw Codec
// operations and raw-only reception remain available without table inference.
var ErrUnsupportedNationalLanguage = errors.New("smpp: UDH national-language tables are unsupported")

func nationalLanguageIE(id byte) bool { return id == 0x24 || id == 0x25 }

// hasNationalLanguage scans the non-concatenation IE sequence produced by
// ParseSegment: no UDHL octet, and all IE lengths already bounded by that UDH.
// Only the declaration's presence matters; interpreting its value (including
// zero or a malformed table value) would require a header-aware encoding API.
func hasNationalLanguage(udh []byte) bool {
	for len(udh) >= 2 {
		if nationalLanguageIE(udh[0]) {
			return true
		}
		n := 2 + int(udh[1])
		if n > len(udh) {
			return false // defensive: ParseSegment rejects this before admission
		}
		udh = udh[n:]
	}
	return false
}
