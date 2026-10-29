// SPDX-AI-Disclosure: ai-generated
// SPDX-AI-Model: GPT-6.0 Astra
// SPDX-AI-Provider: OpenAI
// SPDX-AI-Scope: Library code generated and revised with Codex under maintainer direction.
// SPDX-AI-Date: 2026-10-29

package smpp

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/skill215/smppkit/pdu"
)

// stopHeartbeatLocked is irreversible within this connection generation. It is
// called only after unbind admission succeeds, or when the Session closes.
func (s *Session) stopHeartbeatLocked() {
	if s.heartbeatStop != nil && !s.heartbeatStopping {
		s.heartbeatStopping = true
		close(s.heartbeatStop)
	}
}

// heartbeatLoop owns one timer and performs one request at a time. Resetting
// after completion (rather than ticking) prevents a slow exchange or full window
// from accumulating work. No scheduler or channels are allocated when disabled.
func (s *Session) heartbeatLoop() {
	select {
	case <-s.heartbeatReady:
	case <-s.heartbeatStop:
		return
	}
	timer := time.NewTimer(s.cfg.EnquireLinkInterval)
	defer timer.Stop()
	for {
		select {
		case <-s.heartbeatStop:
			return
		case <-timer.C:
		}
		result, err := s.request(context.Background(), &pdu.EnquireLink{}, true)
		if s.State() != Bound {
			return
		}
		// Local pressure is not evidence of an unresponsive peer. A queued
		// probe that expired before writing is equally inconclusive.
		if errors.Is(err, ErrOverloaded) || result.Outcome == NotWritten && errors.Is(err, context.DeadlineExceeded) {
			s.logEvent(context.Background(), slog.LevelDebug, "enquire_link_skipped", slog.String("reason", diagnosticReason(err)))
		} else {
			if err == nil && (result.Response.Body == nil || result.Response.Body.CommandID() != pdu.EnquireLinkRespID) {
				err = ErrResponse
			}
			if err != nil {
				// First terminal cause wins; a concurrent unbind suppresses
				// heartbeat retirement. No reconnect or business replay follows.
				s.stopWithState(err, true)
				return
			}
			s.logRequest(context.Background(), pdu.EnquireLinkID, result, nil)
		}
		timer.Reset(s.cfg.EnquireLinkInterval)
	}
}

// cancelHeartbeatWrite retires a bound connection on an incomplete probe's
// deadline. During unbinding it cancels queued work but lets an active frame
// finish under WriteTimeout so the unbind ACK can follow on the same writer.
// The caller joins j.done before inspecting counters or releasing its window slot.
func (s *Session) cancelHeartbeatWrite(j *writeJob, ctx context.Context, received bool) {
	if j.cancelQueued(ctx.Err()) {
		return
	}
	if received {
		// Completion and the deadline can both be ready in the caller's select.
		// A correlated response plus a finished write completes the exchange;
		// scheduler delay alone must not retire a healthy connection. Without a
		// response a finished write is still subject to the probe deadline, and
		// a response must not exempt a still-blocked write from that deadline.
		select {
		case <-j.done:
			return
		default:
		}
	}
	if ctx.Err() != nil {
		s.stopWithState(ErrEnquireLinkTimeout, true)
	}
}
