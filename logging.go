// SPDX-AI-Disclosure: ai-generated
// SPDX-AI-Model: GPT-6.0 Astra
// SPDX-AI-Provider: OpenAI
// SPDX-AI-Scope: Library code generated and revised with Codex under maintainer direction.
// SPDX-AI-Date: 2026-10-29

package smpp

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"

	"github.com/skill215/smppkit/codec"
	"github.com/skill215/smppkit/pdu"
)

var discardLogger = slog.New(slog.DiscardHandler)

// diagnosticReason deliberately never formats err: network errors, custom
// codecs and authentication callbacks can embed credentials or message bytes.
// Categories are diagnostic hints, not a replacement for returned errors.
func diagnosticReason(err error) string {
	switch {
	case err == nil:
		return "ok"
	case errors.Is(err, ErrClosed):
		return "closed"
	case errors.Is(err, context.Canceled):
		return "canceled"
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	case errors.Is(err, errInboundTimeout):
		return "inbound_timeout"
	case errors.Is(err, ErrEnquireLinkTimeout):
		return "enquire_link_timeout"
	case errors.Is(err, ErrOverloaded):
		return "overloaded"
	case errors.Is(err, ErrState):
		return "invalid_state"
	case errors.Is(err, ErrResponse):
		return "unexpected_response"
	case errors.Is(err, codec.ErrValidation):
		return "invalid_pdu"
	case errors.Is(err, codec.ErrUnsupported):
		return "unsupported"
	case errors.Is(err, io.EOF):
		return "eof"
	case errors.Is(err, io.ErrUnexpectedEOF):
		return "truncated_frame"
	case errors.Is(err, net.ErrClosed), errors.Is(err, io.ErrClosedPipe):
		return "transport_closed"
	}
	if _, ok := errors.AsType[*PeerError](err); ok {
		return "peer_rejected"
	}
	if _, ok := errors.AsType[*codec.FieldError](err); ok {
		return "invalid_pdu"
	}
	if transport, ok := errors.AsType[net.Error](err); ok && transport.Timeout() {
		return "timeout"
	}
	return "other"
}

func diagnosticLevel(err error) slog.Level {
	if err == nil || errors.Is(err, ErrClosed) || errors.Is(err, context.Canceled) {
		return slog.LevelDebug
	}
	return slog.LevelWarn
}

// Setup failures have no Session yet. Keep even addresses and identities out
// of the record; applications can attach their own safe correlation attributes.
func logSetup(ctx context.Context, logger *slog.Logger, endpoint, stage string, err error) {
	if logger == discardLogger {
		return // default diagnostics must not even inspect application errors
	}
	level := diagnosticLevel(err)
	if !logger.Enabled(ctx, level) {
		return
	}
	logger.LogAttrs(ctx, level, "smpp setup failed",
		slog.String("component", "smppkit"), slog.String("event", "setup_failed"),
		slog.String("endpoint", endpoint), slog.String("stage", stage),
		slog.String("reason", diagnosticReason(err)))
}

// Only immutable Session fields are read here. Callers never hold Session.mu
// or writeJob.mu when invoking this helper, including Enabled itself.
func (s *Session) logEvent(ctx context.Context, level slog.Level, event string, attrs ...slog.Attr) {
	logger := s.cfg.Logger
	if !logger.Enabled(ctx, level) {
		return
	}
	endpoint := "client"
	if s.server {
		endpoint = "server"
	}
	// Allocate/construct the record only after the level gate. The session ID is
	// an opaque library-generated identifier, never a peer's bind system_id.
	// Current events fit this stack buffer. append remains safe if a future
	// event adds more fields; only enabled logging can then allocate the buffer.
	fields := make([]slog.Attr, 0, 12)
	fields = append(fields, slog.String("component", "smppkit"),
		slog.String("event", event), slog.String("session_id", s.namespace),
		slog.String("endpoint", endpoint), slog.Int("version", int(s.codec.Version())))
	fields = append(fields, attrs...)
	logger.LogAttrs(ctx, level, event, fields...)
}

func (s *Session) logRequest(ctx context.Context, command pdu.CommandID, result Result, err error) {
	if s.cfg.Logger == discardLogger {
		return
	}
	level := diagnosticLevel(err)
	if !s.cfg.Logger.Enabled(ctx, level) {
		return
	}
	s.logEvent(ctx, level, "request_completed",
		slog.Uint64("command_id", uint64(command)), slog.Uint64("sequence", uint64(result.Sequence)),
		slog.Int("outcome", int(result.Outcome)), slog.Int("bytes_written", result.BytesWritten),
		slog.Uint64("status", uint64(result.Response.Status)), slog.String("reason", diagnosticReason(err)))
}

// The expiry loop owns this one terminal record. Close itself never calls a
// handler, and Wait joins this logging work along with that runtime loop.
func (s *Session) logClosed() {
	if s.cfg.Logger == discardLogger {
		return
	}
	err := s.Err()
	level := diagnosticLevel(err)
	ctx := context.Background()
	if s.cfg.Logger.Enabled(ctx, level) {
		s.logEvent(ctx, level, "session_closed", slog.String("reason", diagnosticReason(err)))
	}
}
