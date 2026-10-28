// SPDX-AI-Disclosure: ai-generated
// SPDX-AI-Model: GPT-6.0 Astra
// SPDX-AI-Provider: OpenAI
// SPDX-AI-Scope: Library code generated and revised with Codex under maintainer direction.
// SPDX-AI-Date: 2026-10-28

package codec

import "github.com/skill215/smppkit/pdu"

// validTime checks the wire representation without consulting the wall clock,
// assigning a century, converting zones or rewriting the caller's string.
// Empty schedule/validity/final_date retain their command-defined meanings.
func (c *Codec) validTime(s string, final bool) bool {
	if s == "" {
		return true
	}
	// 3.3 §7.5 and 3.4 §7.1.1 explicitly describe local response dates without
	// the UTC suffix. Also accept their full absolute form; 5.0 requires it.
	local := final && c.version != pdu.V50 && len(s) == 12
	if !local && len(s) != 16 {
		return false
	}
	n := len(s)
	if !local {
		n--
	}
	for i := 0; i < n; i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	two := func(i int) int { return int(s[i]-'0')*10 + int(s[i+1]-'0') }
	year, month, day := two(0), two(2), two(4)
	if two(6) > 23 || two(8) > 59 || two(10) > 59 {
		return false
	}
	if !local && s[15] == 'R' {
		if final || c.version == pdu.V33 {
			return false
		}
		// Relative components are offsets: zero months/days must be valid for
		// e.g. a one-hour expiry. t/nn are explicitly ignored by the protocol;
		// preserve them rather than silently normalizing their numeric bytes.
		return month <= 12 && day <= 31
	}
	if !local && (s[15] != '+' && s[15] != '-' || two(13) > 48) {
		return false
	}
	if month < 1 || month > 12 || day < 1 {
		return false
	}
	days := [...]int{31, 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31}
	if month == 2 && year%4 == 0 {
		// YY=00 may denote a leap century (e.g. 2000). Rejecting it would choose
		// a century the wire does not carry; the peer owns that interpretation.
		return day <= 29
	}
	return day <= days[month-1]
}
