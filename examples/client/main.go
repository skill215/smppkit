// SPDX-AI-Disclosure: ai-generated
// SPDX-AI-Model: GPT-6.0 Astra
// SPDX-AI-Provider: OpenAI
// SPDX-AI-Scope: Example program generated and revised with Codex under maintainer direction.
// SPDX-AI-Date: 2026-10-28

// Connect, submit text, optionally receive one delivery, then unbind.
// See examples/README.md for a matching local server and multipart commands.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"time"

	smpp "github.com/skill215/smppkit"
	"github.com/skill215/smppkit/codec"
	"github.com/skill215/smppkit/message"
	"github.com/skill215/smppkit/pdu"
	"github.com/skill215/smppkit/textcodec"
)

func main() {
	// Return from run before exiting so every session/worker cleanup executes.
	if err := run(); err != nil {
		log.Print(err)
		os.Exit(1)
	}
}

func run() error {
	address := flag.String("address", "127.0.0.1:2775", "SMSC host:port")
	system := flag.String("system-id", "demo", "bind system ID")
	version := flag.String("version", "5.0", "SMPP version: 3.3, 3.4 or 5.0")
	from := flag.String("from", "example", "source address")
	to := flag.String("to", "12345", "destination address")
	text := flag.String("text", "Hello from smppkit", "message text; long text is automatically segmented")
	waitDelivery := flag.Bool("wait-delivery", false, "receive one complete deliver_sm message before unbind (3.4/5.0)")
	timeout := flag.Duration("timeout", 30*time.Second, "overall bind/send/receive deadline")
	flag.Parse()
	password := os.Getenv("SMPP_PASSWORD")
	if password == "" || len(password) > 8 {
		return errors.New("set SMPP_PASSWORD to 1..8 ASCII octets")
	}
	if *timeout <= 0 {
		return errors.New("timeout must be positive")
	}
	v, ok := map[string]pdu.Version{"3.3": pdu.V33, "3.4": pdu.V34, "5.0": pdu.V50}[*version]
	if !ok {
		return fmt.Errorf("unsupported version %q", *version)
	}
	if v == pdu.V33 && *waitDelivery {
		return errors.New("3.3 example uses transmitter bind; receive requires a separate receiver bind")
	}
	signalCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	ctx, cancel := context.WithTimeout(signalCtx, *timeout)
	defer cancel()
	c, err := codec.New(codec.Config{Version: v})
	if err != nil {
		return err
	}
	fields := pdu.BindFields{SystemID: *system, Password: password, InterfaceVersion: v}
	var bind pdu.Body = &pdu.BindTransceiver{BindFields: fields}
	if v == pdu.V33 {
		bind = &pdu.BindTransmitter{BindFields: fields}
	}
	s, err := smpp.Dial(ctx, *address, smpp.ClientConfig{Codec: c, Bind: bind})
	if err != nil {
		return fmt.Errorf("dial/bind: %w", err)
	}
	defer func() { s.Close(); s.Wait() }()
	log.Printf("Bound SMPP %s", *version)

	// The message reader must run while submitting: the peer can deliver before
	// SendText returns. ACK each segment on memory admission, not after assembly.
	var deliveries chan message.Message
	var receiveDone chan struct{}
	var receiveErr error // read only after receiveDone closes
	if v != pdu.V33 {
		receiver, err := message.NewReceiver(message.ReceiveConfig{
			AckPolicy: message.AckOnBuffered,
			Encodings: map[uint8]textcodec.Encoding{0: textcodec.GSM7, 3: textcodec.Latin1, 8: textcodec.UCS2},
		})
		if err != nil {
			return err
		}
		if *waitDelivery {
			deliveries = make(chan message.Message, 1)
		}
		receiveCtx, stopReceiver := context.WithCancel(ctx)
		receiveDone = make(chan struct{})
		go func() {
			defer close(receiveDone)
			receiveErr = readDeliveries(receiveCtx, s, receiver, deliveries)
		}()
		defer func() {
			s.Close()
			stopReceiver()
			<-receiveDone
		}()
	}
	result, err := s.SendText(ctx, smpp.TextMessage{
		Source: pdu.Address{Value: *from}, Destination: pdu.Address{TON: 1, NPI: 1, Value: *to}, Text: *text,
	}, message.GSMText())
	for _, part := range result.Segments {
		log.Printf("part=%d outcome=%d sequence=%d bytes_written=%d message_id=%q error=%v", part.Number, part.Result.Outcome, part.Result.Sequence, part.Result.BytesWritten, part.MessageID, part.Err)
	}
	if err != nil {
		return fmt.Errorf("submit (inspect segment outcomes; no retry): %w", err)
	}
	log.Print("All segments accepted; final delivery remains unknown.")
	if *waitDelivery {
		select {
		case msg := <-deliveries:
			if msg.DecodeError != nil {
				return fmt.Errorf("decode delivery: %w", msg.DecodeError)
			}
		case <-receiveDone:
			return fmt.Errorf("receive delivery: %w", receiveErr)
		case <-ctx.Done():
			return fmt.Errorf("receive delivery: %w", ctx.Err())
		}
	}
	// Use a separate bounded cleanup deadline; the operation context may expire
	// just as the last response arrives. Shutdown does not retry submissions.
	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancelShutdown()
	if err := s.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("unbind: %w", err)
	}
	log.Print("Unbound and closed.")
	return nil
}

// Sole inbound consumer. Publishing a completion happens AFTER Respond has
// joined the ACK write, so main cannot unbind while the last ACK is still pending.
func readDeliveries(ctx context.Context, s *smpp.Session, receiver *message.Receiver, deliveries chan<- message.Message) error {
	defer s.Close() // a receive/ACK failure also wakes any concurrent submission
	for {
		// Sweep under continuous traffic too: an idle-only sweep lets unrelated
		// incoming messages keep expired incomplete groups occupying capacity.
		if err := receiver.Expire(time.Now()); err != nil {
			return err
		}
		for _, event := range receiver.Events() {
			log.Printf("Receive event: %s missing=%v", event.Kind, event.Missing)
		}
		pollCtx, cancel := context.WithTimeout(ctx, time.Second)
		in, err := s.Receive(pollCtx)
		cancel()
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if errors.Is(err, context.DeadlineExceeded) {
				continue
			}
			return err
		}
		if err := smpp.AdmitMessage(ctx, receiver, in, ""); err != nil {
			return err
		}
		if receiver.Stats().Queued == 0 {
			continue
		}
		msg, err := receiver.Receive(ctx)
		if err != nil {
			return err
		}
		log.Printf("Received delivery: segments=%d bytes=%d receipt=%t text=%q", len(msg.Segments), len(msg.Payload), msg.Receipt, msg.Text)
		// An echoed deliver_sm is an incoming message, not a delivery receipt.
		if deliveries != nil {
			select {
			case deliveries <- msg:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}
}
