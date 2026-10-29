// SPDX-AI-Disclosure: ai-generated
// SPDX-AI-Model: GPT-6.0 Astra
// SPDX-AI-Provider: OpenAI
// SPDX-AI-Scope: Library code generated and revised with Codex under maintainer direction.
// SPDX-AI-Date: 2026-10-29

package message

import (
	"strings"

	"github.com/skill215/smppkit/pdu"
	"github.com/skill215/smppkit/textcodec"
)

var receiptLabels = [...]string{"id", "sub", "dlvrd", "submit date", "done date", "stat", "err", "text"}
var receiptStates = [...]string{"", "ENROUTE", "DELIVRD", "EXPIRED", "DELETED", "UNDELIV", "ACCEPTD", "UNKNOWN", "REJECTD"}

func receiptTextState(s string) (byte, bool) {
	for n := 1; n < len(receiptStates); n++ {
		if receiptStates[n] == s {
			return byte(n), true
		}
	}
	return 0, false
}
func receiptLabel(s string) (name string, n int) {
	for _, label := range receiptLabels {
		if strings.HasPrefix(s, label+":") {
			return label, len(label) + 1
		}
	}
	if strings.HasPrefix(s, "Text:") {
		return "text", 5
	}
	return "", 0
}
func receiptSpace(c byte) bool { return c == ' ' || c == '\t' }
func receiptToken(s string, limit int) bool {
	if len(s) == 0 || len(s) > limit {
		return false
	}
	for i := range len(s) {
		if s[i] <= 32 || s[i] >= 127 || s[i] == ':' {
			return false
		}
	}
	return true
}
func receiptDigits(s string, n int) bool {
	if len(s) != n {
		return false
	}
	for i := range len(s) {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// Validate only the supplied ten-digit civil components. There is no century,
// time zone or conversion to time.Time; February 29 in year 00 cannot resolve
// whether the unspecified century is a leap century.
func receiptDate(s string) bool {
	if !receiptDigits(s, 10) {
		return false
	}
	number := func(i int) int { return int(s[i]-'0')*10 + int(s[i+1]-'0') }
	month, day := number(2), number(4)
	if month < 1 || month > 12 || day < 1 || number(6) > 23 || number(8) > 59 {
		return false
	}
	if month == 2 && day == 29 && number(0)%4 != 0 {
		return false
	}
	days := [...]int{0, 31, 29, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31}
	return day <= days[month]
}

func (r *Receipt) parseReceiptText(index int, version pdu.Version) error {
	t := &r.Texts[index]
	if r.ESMClass&0x40 != 0 {
		r.issue(ReceiptUnsupported, t.Source, t.TLVIndex, "udh")
		return nil
	}
	var decoded string
	var err error
	switch {
	case r.DataCoding == 0:
		// Selecting this dialect explicitly opts into unpacked GSM default alphabet
		// for DCS 0. Other SMSC-default alphabets need an application-owned dialect.
		decoded, err = textcodec.GSM7.Decode(t.Raw)
	case version != pdu.V33 && (r.DataCoding == 1 || r.DataCoding == 3):
		decoded = string(t.Raw) // this dialect is ASCII-only, including Latin-1's subset
	case r.DataCoding == 8:
		decoded, err = textcodec.UCS2.Decode(t.Raw)
	default:
		r.issue(ReceiptUnsupported, t.Source, t.TLVIndex, "data_coding")
		return nil
	}
	if err != nil {
		r.issue(ReceiptMalformed, t.Source, t.TLVIndex, "payload")
		return nil
	}
	if !receiptASCII([]byte(decoded), false) {
		r.issue(ReceiptUnsupported, t.Source, t.TLVIndex, "payload")
		return nil
	}
	counts := map[string]int{}
	for pos, steps := 0, 0; pos < len(decoded); steps++ {
		for pos < len(decoded) && receiptSpace(decoded[pos]) {
			pos++
		}
		if pos == len(decoded) {
			break
		}
		if steps == receiptMaxFields {
			return ErrCapacity
		}
		label, n := receiptLabel(decoded[pos:])
		if n == 0 {
			r.issue(ReceiptUnsupported, t.Source, t.TLVIndex, "syntax")
			for pos < len(decoded) && !receiptSpace(decoded[pos]) {
				pos++
			}
			continue
		}
		pos += n
		start := pos
		if label == "text" {
			pos = len(decoded)
		} else {
			for pos < len(decoded) && receiptSpace(decoded[pos]) {
				pos++
			}
			start = pos
			// An immediately following label is a missing value, not this field's ID.
			if _, next := receiptLabel(decoded[pos:]); next == 0 {
				for pos < len(decoded) && !receiptSpace(decoded[pos]) {
					pos++
				}
			}
		}
		value := decoded[start:pos]
		t.Fields = append(t.Fields, ReceiptField{label, value})
		counts[label]++
		if counts[label] > 1 {
			r.issue(ReceiptDuplicate, t.Source, t.TLVIndex, label)
		}
		valid := true
		switch label {
		case "id":
			valid = receiptToken(value, 64)
		case "sub", "dlvrd":
			valid = receiptDigits(value, 3)
		case "submit date", "done date":
			valid = receiptDate(value)
		case "stat":
			valid = receiptToken(value, 32)
			if _, ok := receiptTextState(value); valid && !ok {
				r.issue(ReceiptUnsupported, t.Source, t.TLVIndex, label)
			}
		case "err":
			valid = receiptToken(value, 32)
		case "text":
			valid = len(value) <= 20 && receiptASCII([]byte(value), true)
		}
		if !valid {
			r.issue(ReceiptMalformed, t.Source, t.TLVIndex, label)
		}
	}
	for _, label := range receiptLabels {
		if counts[label] == 0 {
			r.issue(ReceiptMissing, t.Source, t.TLVIndex, label)
		}
	}
	return nil
}
