// SPDX-AI-Disclosure: ai-generated
// SPDX-AI-Model: GPT-6.0 Astra
// SPDX-AI-Provider: OpenAI
// SPDX-AI-Scope: Library code generated and revised with Codex under maintainer direction.
// SPDX-AI-Date: 2026-10-29

package smpp

import (
	"errors"
	"fmt"

	"github.com/skill215/smppkit/pdu"
)

// Internal identity preserves the existing timeout text while allowing safe
// diagnostic classification without parsing or printing arbitrary error text.
var errInboundTimeout = errors.New("smpp: inbound response deadline exceeded")

var (
	// ErrClosed is the terminal cause of an explicit local close.
	ErrClosed = errors.New("smpp: session closed")
	// ErrState rejects operations outside the current role, state or TLV policy,
	// and transaction submissions incompatible with automatic buffered ACKs.
	ErrState = errors.New("smpp: operation invalid for session state or role")
	// ErrOverloaded reports exhausted session admission or response capacity.
	ErrOverloaded = errors.New("smpp: session capacity exceeded")
	// ErrResponse indicates a command incompatible with its request.
	ErrResponse = errors.New("smpp: mismatched response command")
	// ErrResponded means the Inbound response handle has already been claimed.
	ErrResponded = errors.New("smpp: request already responded")
	// ErrEnquireLinkTimeout retires a bound Session when an automatic probe
	// started writing but did not finish its exchange before EnquireLinkTimeout.
	// A full request window or expiry before writing does not cause this error.
	ErrEnquireLinkTimeout = errors.New("smpp: enquire_link deadline exceeded")
)

// PeerError reports protocol rejection. Request's Result retains the complete
// correlated response; PeerError itself contains only its command and status.
type PeerError struct {
	Command pdu.CommandID
	Status  pdu.Status
}

func (e *PeerError) Error() string {
	return fmt.Sprintf("smpp: command 0x%08x rejected with status 0x%08x", e.Command, e.Status)
}

// Outcome describes protocol observation, not independently verified delivery.
// A transaction response can report the peer's delivery outcome; interpretation
// depends on the original request mode and remains the application's responsibility.
type Outcome uint8

const (
	NotAttempted     Outcome = iota // planned segment never passed to Request
	NotWritten                      // no frame bytes written; local validation may have failed
	PartlyWritten                   // some frame bytes written; delivery/acceptance is unknown
	Written                         // fully written, no correlated response: result is uncertain
	ResponseReceived                // inspect Response.Status and request-mode semantics
)

// Result remains meaningful when Request returns an error. Sequence is zero
// before sequence allocation; Response is populated only for ResponseReceived.
// BytesWritten counts the complete SMPP frame, including its 16-byte header.
// For submit_multi, Response.Status and SubmitMultiResp.Unsuccessful describe
// separate levels of acceptance; an OK header does not imply an empty list.
type Result struct {
	Sequence     uint32
	Outcome      Outcome
	BytesWritten int
	Response     pdu.PDU
}
