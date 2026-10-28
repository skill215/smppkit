// A self-contained submit_multi exchange over net.Pipe. The synthetic SMSC
// rejects one SME to demonstrate partial acceptance; it does not deliver SMS.
package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"time"

	smpp "github.com/skill215/smppkit"
	"github.com/skill215/smppkit/codec"
	"github.com/skill215/smppkit/pdu"
	"github.com/skill215/smppkit/tlv"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, err := codec.New(codec.Config{Version: pdu.V50})
	if err != nil {
		return err
	}
	srv, err := smpp.NewServer(smpp.ServerConfig{Profiles: []*codec.Codec{c},
		Authenticate: func(context.Context, smpp.BindInfo) (smpp.BindDecision, error) {
			// Demo only: real servers must authenticate credentials here.
			return smpp.BindDecision{SystemID: "demo"}, nil
		}})
	if err != nil {
		return err
	}
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	finished := make(chan error, 1)
	go func() {
		s, err := srv.Accept(ctx, b)
		if err != nil {
			finished <- err
			return
		}
		defer func() { s.Close(); s.Wait() }()
		in, err := s.Receive(ctx)
		if err == nil {
			_, ok := in.PDU.Body.(*pdu.SubmitMulti)
			if !ok {
				finished <- fmt.Errorf("expected submit_multi")
				return
			}
			// A real SMSC owns list expansion and per-destination acceptance policy.
			err = in.Respond(ctx, pdu.OK, &pdu.SubmitMultiResp{MessageID: "demo:1",
				Unsuccessful: []pdu.UnsuccessfulSME{{Address: pdu.Address{TON: 1, NPI: 1, Value: "10002"}, Status: pdu.Throttled}},
				Optional:     tlv.Fields{{Tag: tlv.CongestionState, Value: []byte{80}}}})
		}
		finished <- err
	}()
	client, err := smpp.NewSession(a, c, smpp.Config{})
	if err != nil {
		return err
	}
	defer func() { client.Close(); client.Wait() }()
	if _, err := client.Bind(ctx, &pdu.BindTransmitter{BindFields: pdu.BindFields{SystemID: "example", InterfaceVersion: pdu.V50}}); err != nil {
		return err
	}
	result, err := client.Request(ctx, &pdu.SubmitMulti{
		Source: pdu.Address{Value: "example"}, DataCoding: 3, ShortMessage: []byte("Hello"),
		Destinations: []pdu.MultiDestination{
			{Kind: pdu.SMEAddress, Address: pdu.Address{TON: 1, NPI: 1, Value: "10001"}},
			{Kind: pdu.SMEAddress, Address: pdu.Address{TON: 1, NPI: 1, Value: "10002"}},
		},
	})
	if err != nil {
		// Written/PartlyWritten without a response is uncertain: do not replay
		// the destination list automatically. Result remains useful on errors.
		return fmt.Errorf("submit_multi outcome=%d: %w", result.Outcome, err)
	}
	response := result.Response.Body.(*pdu.SubmitMultiResp)
	fmt.Printf("message_id=%s unsuccessful=%d\n", response.MessageID, len(response.Unsuccessful))
	for _, failure := range response.Unsuccessful {
		fmt.Printf("destination=%s status=0x%08x\n", failure.Address.Value, failure.Status)
	}
	// An OK response can contain failures. Acceptance is not final delivery.
	return <-finished
}
