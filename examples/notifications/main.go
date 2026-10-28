// A synthetic MC-initiated connection: outbind -> bind_receiver -> alert.
// Credentials are demo constants; production applications supply their own
// authentication and TLS policy. This example performs no handset delivery.
package main

import (
	"context"
	"errors"
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
	srv, err := smpp.NewServer(smpp.ServerConfig{Profiles: []*codec.Codec{c}, Authenticate: func(_ context.Context, info smpp.BindInfo) (smpp.BindDecision, error) {
		if info.Fields.SystemID != "demo-esme" || info.Fields.Password != "esme-pw" {
			return smpp.BindDecision{Status: pdu.BindFailed}, nil
		}
		return smpp.BindDecision{SystemID: "demo-mc"}, nil
	}})
	if err != nil {
		return err
	}
	// MC normally dials an ESME listener. Transport direction does not reverse
	// SMPP roles: Server.Outbind still creates the MC side of the Session.
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	done := make(chan error, 1)
	go func() {
		s, err := srv.Outbind(ctx, b, pdu.V50, &pdu.Outbind{SystemID: "demo-mc", Password: "mc-pw"})
		if err != nil {
			done <- err
			return
		}
		defer func() { s.Close(); s.Wait() }()
		r, err := s.Notify(ctx, &pdu.AlertNotification{Source: pdu.Address{TON: 1, NPI: 1, Value: "12345"}, ESME: pdu.Address{TON: 5, Value: "demo-esme"}, Optional: tlv.Fields{{Tag: tlv.MSAvailabilityStatus, Value: []byte{0}}}})
		if err != nil {
			done <- err
			return
		}
		if r.Outcome != smpp.Written {
			done <- errors.New("notification not fully written")
			return
		}
		// Keep the connection until the ESME explicitly unbinds. Written proves
		// transport completion only; notifications have no response to wait for.
		select {
		case <-s.Done():
			done <- nil
		case <-ctx.Done():
			done <- ctx.Err()
		}
	}()
	client, err := smpp.NewSession(a, c, smpp.Config{NotificationLimit: 8})
	if err != nil {
		return err
	}
	defer func() { client.Close(); client.Wait() }()
	packet, err := client.ReceiveNotification(ctx)
	if err != nil {
		return err
	}
	out, ok := packet.Body.(*pdu.Outbind)
	if !ok || out.SystemID != "demo-mc" || out.Password != "mc-pw" {
		return errors.New("outbind authentication failed")
	}
	// Explicit application decision; do not echo MC credentials as ESME credentials.
	if _, err = client.Bind(ctx, &pdu.BindReceiver{BindFields: pdu.BindFields{SystemID: "demo-esme", Password: "esme-pw", InterfaceVersion: pdu.V50}}); err != nil {
		return err
	}
	packet, err = client.ReceiveNotification(ctx)
	if err != nil {
		return err
	}
	alert, ok := packet.Body.(*pdu.AlertNotification)
	if !ok {
		return errors.New("expected alert_notification")
	}
	fmt.Printf("availability notification for %s; sequence=%d; no ACK\n", alert.Source.Value, packet.Sequence)
	// In a messaging application, run this consumer alongside Receive/RunReceiver.
	if err = client.Shutdown(ctx); err != nil {
		return err
	}
	return <-done
}
