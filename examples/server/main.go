// SPDX-AI-Disclosure: ai-generated
// SPDX-AI-Model: GPT-6.0 Astra
// SPDX-AI-Provider: OpenAI
// SPDX-AI-Scope: Example program generated and revised with Codex under maintainer direction.
// SPDX-AI-Date: 2026-10-28

// Run a bounded in-memory SMSC demo. Optional echo sends deliver_sm messages;
// it is not routing, a delivery receipt, or a promise of handset delivery.
package main

import (
	"context"
	"crypto/subtle"
	"errors"
	"flag"
	"log"
	"net"
	"os"
	"os/signal"
	"strings"
	"time"

	smpp "github.com/skill215/smppkit"
	"github.com/skill215/smppkit/codec"
	"github.com/skill215/smppkit/message"
	"github.com/skill215/smppkit/pdu"
	"github.com/skill215/smppkit/textcodec"
)

func main() {
	if err := run(); err != nil {
		log.Print(err)
		os.Exit(1)
	}
}

func run() error {
	address := flag.String("address", "127.0.0.1:2775", "listen address; port 0 selects a free port")
	system := flag.String("system-id", "demo", "accepted ESME system ID")
	echo := flag.Bool("echo", false, "echo decoded submissions as deliver_sm; requires a 3.4/5.0 transceiver bind")
	flag.Parse()
	password := os.Getenv("SMPP_PASSWORD")
	if password == "" || len(password) > 8 || strings.IndexByte(password, 0) >= 0 {
		return errors.New("set SMPP_PASSWORD to 1..8 ASCII octets without NUL")
	}
	for _, ch := range []byte(password) {
		if ch > 127 {
			return errors.New("SMPP_PASSWORD must be ASCII")
		}
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	var profiles []*codec.Codec
	for _, version := range []pdu.Version{pdu.V33, pdu.V34, pdu.V50} {
		c, err := codec.New(codec.Config{Version: version})
		if err != nil {
			return err
		}
		profiles = append(profiles, c)
	}
	srv, err := smpp.NewServer(smpp.ServerConfig{
		Profiles: profiles, MaxConnections: 32,
		Authenticate: func(_ context.Context, info smpp.BindInfo) (smpp.BindDecision, error) {
			status := pdu.OK
			if info.Fields.SystemID != *system || subtle.ConstantTimeCompare([]byte(info.Fields.Password), []byte(password)) != 1 {
				status = pdu.BindFailed
			}
			// The demo's echo uses the same bind for submission and delivery. A real
			// application may instead manage independent TX/RX sessions itself.
			if *echo && info.Role != smpp.Transceiver {
				status = pdu.BindFailed
			}
			return smpp.BindDecision{Status: status, SystemID: "example"}, nil
		},
	})
	if err != nil {
		return err
	}
	ln, err := net.Listen("tcp", *address)
	if err != nil {
		return err
	}
	defer ln.Close()
	log.Printf("Listening on %s (echo=%t)", ln.Addr(), *echo)
	err = srv.Serve(ctx, ln, func(ctx context.Context, s *smpp.Session) { handleSession(ctx, s, *echo) })
	if ctx.Err() != nil {
		return nil
	} // Ctrl-C: Serve has joined its handlers
	return err
}

func handleSession(ctx context.Context, s *smpp.Session, echo bool) {
	encodings := map[uint8]textcodec.Encoding{0: textcodec.GSM7, 8: textcodec.UCS2}
	if s.Codec().Version() != pdu.V33 {
		encodings[3] = textcodec.Latin1
	}
	receiver, err := message.NewReceiver(message.ReceiveConfig{AckPolicy: message.AckOnBuffered, Encodings: encodings})
	if err != nil {
		log.Print(err)
		return
	}
	receiveCtx, stop := context.WithCancel(ctx)
	defer stop()
	completed := make(chan struct{})
	go func() {
		defer close(completed)
		for {
			// Drain bounded expiry events even while no complete message is available.
			pollCtx, cancelPoll := context.WithTimeout(receiveCtx, time.Second)
			msg, err := receiver.Receive(pollCtx)
			cancelPoll()
			for _, event := range receiver.Events() {
				log.Printf("Receive event: %s missing=%v", event.Kind, event.Missing)
			}
			if err != nil {
				if receiveCtx.Err() != nil {
					return
				}
				if errors.Is(err, context.DeadlineExceeded) {
					continue
				}
				log.Printf("Receive: %v", err)
				return
			}
			log.Printf("Complete message: segments=%d bytes=%d receipt=%t decoded=%t", len(msg.Segments), len(msg.Payload), msg.Receipt, msg.Decoded)
			if msg.DecodeError != nil {
				log.Printf("Decode: %v", msg.DecodeError)
			}
			if echo && msg.Decoded && !msg.Receipt {
				// This application choice runs outside Session's reader. The peer must
				// keep its receiver active while waiting for submission responses.
				result, err := s.SendText(receiveCtx, smpp.TextMessage{Source: msg.Destination, Destination: msg.Source, Text: msg.Text}, message.GSMText())
				for _, part := range result.Segments {
					log.Printf("Echo part=%d outcome=%d error=%v", part.Number, part.Result.Outcome, part.Err)
				}
				if err != nil {
					log.Printf("Echo failed (no retry): %v", err)
				}
			}
		}
	}()
	// One inbound consumer acknowledges each admitted segment; assembly and the
	// optional echo run separately so multipart with window=1 can make progress.
	// This demo uses a non-transaction default for mode 0. RunReceiver rejects
	// explicit transaction submissions; echo is not synchronous handset delivery.
	err = s.RunReceiver(receiveCtx, receiver, "")
	stop()
	<-completed
	if err != nil && !errors.Is(err, smpp.ErrClosed) && ctx.Err() == nil {
		log.Printf("Session ended: %v", err)
	}
}
