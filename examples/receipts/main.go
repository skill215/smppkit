// SPDX-AI-Disclosure: ai-generated
// SPDX-AI-Model: GPT-6.0 Astra
// SPDX-AI-Provider: OpenAI
// SPDX-AI-Scope: Example program generated and revised with Codex under maintainer direction.
// SPDX-AI-Date: 2026-10-29

// An explicit synthetic report over a bound Server/Client pair. The report is
// caller-provided evidence, not a consequence inferred from a submission ACK.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"sync"
	"time"

	smpp "github.com/skill215/smppkit"
	"github.com/skill215/smppkit/codec"
	"github.com/skill215/smppkit/message"
	"github.com/skill215/smppkit/pdu"
)

func main() {
	version := flag.String("version", "5.0", "3.3, 3.4 or 5.0")
	data := flag.Bool("data-sm", false, "send data_sm instead of deliver_sm (3.4/5.0)")
	flag.Parse()
	v, ok := map[string]pdu.Version{"3.3": pdu.V33, "3.4": pdu.V34, "5.0": pdu.V50}[*version]
	if !ok {
		log.Fatal("unsupported version")
	}
	command := pdu.DeliverSMID
	if *data {
		command = pdu.DataSMID
	}
	if err := run(v, command); err != nil {
		log.Fatal(err)
	}
}

func run(v pdu.Version, command pdu.CommandID) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	// A fixture says the original message expired. A real application supplies
	// its observed result and opaque submission ID; smppkit supplies no tracking.
	preview := "demo"
	spec := message.ReceiptSpec{
		Kind: message.MCReceipt, MessageID: "demo-0001", State: 3,
		Source: pdu.Address{TON: 1, NPI: 1, Value: "12345"}, Destination: pdu.Address{TON: 5, Value: "example"},
		Text: &message.ReceiptTextSpec{Submitted: "001", Delivered: "000", SubmitDate: "2610291100", DoneDate: "2610291105", Error: "051", Text: &preview},
	}
	format := message.ReceiptTextAndTLV
	if v == pdu.V33 {
		format = message.ReceiptTextOnly
	} else {
		// This network-specific error has a different namespace from text err:051.
		spec.NetworkError = &message.ReceiptNetworkError{Type: 3, Code: 0x0042}
	}
	body, err := message.BuildReceipt(spec, message.ReceiptBuildOptions{Version: v, Command: command, Format: format})
	if err != nil {
		return err
	}
	c, err := codec.New(codec.Config{Version: v})
	if err != nil {
		return err
	}
	server, err := smpp.NewServer(smpp.ServerConfig{Profiles: []*codec.Codec{c}, Authenticate: func(_ context.Context, info smpp.BindInfo) (smpp.BindDecision, error) {
		if info.Fields.SystemID != "demo" || info.Fields.Password != "demo" {
			return smpp.BindDecision{Status: pdu.BindFailed}, nil
		}
		return smpp.BindDecision{SystemID: "example"}, nil
	}})
	if err != nil {
		return err
	}
	a, b := net.Pipe()
	var workers sync.WaitGroup
	defer func() { a.Close(); b.Close(); workers.Wait() }()
	done := make(chan error, 1)
	workers.Go(func() {
		s, err := server.Accept(ctx, b)
		if err != nil {
			done <- err
			return
		}
		defer func() { s.Close(); s.Wait() }()
		result, err := s.Request(ctx, body)
		if err != nil {
			done <- err
			return
		}
		if result.Outcome != smpp.ResponseReceived {
			done <- errors.New("report was not acknowledged")
			return
		}
		// The ACK means the ESME accepted this report. It does not change the report
		// state and is separate from the original message submission response.
		select {
		case <-s.Done():
			done <- nil
		case <-ctx.Done():
			done <- ctx.Err()
		}
	})
	client, err := smpp.NewSession(a, c, smpp.Config{})
	if err != nil {
		return err
	}
	defer func() { client.Close(); client.Wait() }()
	if _, err = client.Bind(ctx, &pdu.BindReceiver{BindFields: pdu.BindFields{SystemID: "demo", Password: "demo", InterfaceVersion: v}}); err != nil {
		return err
	}
	in, err := client.Receive(ctx)
	if err != nil {
		return err
	}
	report, err := message.ParseReceipt(in.PDU.Body, message.ReceiptParseOptions{Version: v, Dialect: message.ReceiptKeyValue})
	if err != nil {
		return err
	}
	// This demo requires its complete, consistent fixture. Other applications
	// may retain partial evidence and choose a different explicit ACK policy.
	if len(report.Diagnostics) != 0 {
		return fmt.Errorf("receipt diagnostics: %v", report.Diagnostics)
	}
	id, ok := report.Texts[0].Value("id")
	if !ok {
		return errors.New("missing unique receipt ID")
	}
	state, ok := report.Texts[0].Value("stat")
	if !ok {
		return errors.New("missing unique text state")
	}
	fmt.Printf("receipt id=%s kind=0x%02x text_state=%s tlv_state_present=%t\n", id, report.Kind, state, report.TLV.HasState)
	var ack pdu.Body = &pdu.DeliverSMResp{}
	if command == pdu.DataSMID {
		ack = &pdu.DataSMResp{}
	}
	if err = in.Respond(ctx, pdu.OK, ack); err != nil {
		return err
	}
	if err = client.Shutdown(ctx); err != nil {
		return err
	}
	return <-done
}
