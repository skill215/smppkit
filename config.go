// SPDX-AI-Disclosure: ai-generated
// SPDX-AI-Model: GPT-6.0 Astra
// SPDX-AI-Provider: OpenAI
// SPDX-AI-Scope: Library code generated and revised with Codex under maintainer direction.
// SPDX-AI-Date: 2026-10-29

package smpp

import (
	"errors"
	"log/slog"
	"time"
)

// Config bounds one connection generation. Zero values use the documented
// defaults. An inbound request consumer must respond before InboundTimeout;
// notifications use a separate bounded queue and have no ACK timer.
type Config struct {
	// Logger receives optional structured diagnostics. Nil discards all logs;
	// the package never uses slog.Default. Client and Server setup also use it.
	// Handlers are called synchronously, outside Session locks, and must be
	// concurrency-safe and return promptly. They must not wait for operations
	// on the emitting Session (including Wait), since runtime loops emit logs.
	// The caller owns the logger and its output; the library never closes it.
	Logger            *slog.Logger
	Window            int           // 16 outbound requests, including queued writes
	InboundLimit      int           // 32 outstanding requests and queued responses
	NotificationLimit int           // 32 queued inbound notifications and 32 concurrent outbound Notify calls
	RequestTimeout    time.Duration // 30 seconds, includes Request/Notify admission
	InboundTimeout    time.Duration // 30 seconds, closes unanswered sessions
	ReadTimeout       time.Duration // 2 minutes, includes partial frame acquisition
	WriteTimeout      time.Duration // 10 seconds per complete frame
	// EnquireLinkInterval enables periodic probes after successful bind. Zero
	// disables scheduling; incoming enquiries are always answered while bound.
	// The interval starts again after each probe completes or is skipped because
	// the request window is full. Traffic does not reset it.
	EnquireLinkInterval time.Duration
	// EnquireLinkTimeout bounds an automatic probe from window admission through
	// writing and receiving its response. Zero uses RequestTimeout. ReadTimeout
	// and WriteTimeout remain independent and may retire the connection sooner.
	EnquireLinkTimeout time.Duration
}

func (c Config) normalized() (Config, error) {
	if c.Logger == nil {
		c.Logger = discardLogger
	}
	if c.Window == 0 {
		c.Window = 16
	}
	if c.InboundLimit == 0 {
		c.InboundLimit = 32
	}
	if c.NotificationLimit == 0 {
		c.NotificationLimit = 32
	}
	if c.RequestTimeout == 0 {
		c.RequestTimeout = 30 * time.Second
	}
	if c.InboundTimeout == 0 {
		c.InboundTimeout = 30 * time.Second
	}
	if c.ReadTimeout == 0 {
		c.ReadTimeout = 2 * time.Minute
	}
	if c.WriteTimeout == 0 {
		c.WriteTimeout = 10 * time.Second
	}
	if c.EnquireLinkTimeout == 0 {
		c.EnquireLinkTimeout = c.RequestTimeout
	}
	if c.Window < 1 || c.Window > 65536 || c.InboundLimit < 1 || c.InboundLimit > 65536 || c.NotificationLimit < 1 || c.NotificationLimit > 65536 || c.RequestTimeout < 0 || c.InboundTimeout < 0 || c.ReadTimeout < 0 || c.WriteTimeout < 0 || c.EnquireLinkInterval < 0 || c.EnquireLinkTimeout < 0 {
		return c, errors.New("smpp: invalid session limits")
	}
	return c, nil
}
