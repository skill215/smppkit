// SPDX-AI-Disclosure: ai-generated
// SPDX-AI-Model: GPT-6.0 Astra
// SPDX-AI-Provider: OpenAI
// SPDX-AI-Scope: Example program generated and revised with Codex under maintainer direction.
// SPDX-AI-Date: 2026-10-29

// Bridge slog to an application-owned standard logger without a private or
// third-party dependency. Run with go run ./examples/logging; no network needed.
package main

import (
	"context"
	"log"
	"log/slog"
	"os"
	"strings"

	smpp "github.com/skill215/smppkit"
	"github.com/skill215/smppkit/codec"
	"github.com/skill215/smppkit/pdu"
)

// TextHandler handles levels, groups, LogValuer resolution and concurrency.
// The bridge receives complete formatted records, not sensitive raw PDUs.
// A structured application backend should instead preserve Record attributes
// through its own Handler; this text bridge intentionally emits one text message.
type logWriter struct{ logger *log.Logger }

func (w logWriter) Write(p []byte) (int, error) {
	w.logger.Print(strings.TrimSuffix(string(p), "\n"))
	return len(p), nil
}

func main() {
	backend := log.New(os.Stdout, "application: ", 0)
	logger := slog.New(slog.NewTextHandler(logWriter{backend}, &slog.HandlerOptions{Level: slog.LevelDebug})).With("service", "example")
	c, err := codec.New(codec.Config{Version: pdu.V50})
	if err != nil {
		panic(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Exercise a deterministic setup failure without opening a socket.
	_, err = smpp.Dial(ctx, "127.0.0.1:2775", smpp.ClientConfig{
		Codec: c, Bind: &pdu.BindTransceiver{BindFields: pdu.BindFields{InterfaceVersion: pdu.V50}},
		Session: smpp.Config{Logger: logger},
	})
	if err == nil {
		panic("expected canceled setup")
	}
	// Do not call slog.SetDefault or log.SetOutput here: the backend must not
	// feed back into this slog logger and create an output recursion loop.
}
