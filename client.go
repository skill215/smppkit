// SPDX-AI-Disclosure: ai-generated
// SPDX-AI-Model: GPT-6.0 Astra
// SPDX-AI-Provider: OpenAI
// SPDX-AI-Scope: Library code generated and revised with Codex under maintainer direction.
// SPDX-AI-Date: 2026-10-29

package smpp

import (
	"context"
	"crypto/tls"
	"net"

	"github.com/skill215/smppkit/codec"
	"github.com/skill215/smppkit/pdu"
)

// ClientConfig fixes transport security, the protocol profile and bind identity.
// Bind.InterfaceVersion must match Codec; no version fallback is attempted.
type ClientConfig struct {
	Codec   *codec.Codec
	Session Config
	Bind    pdu.Body    // *BindTransmitter, *BindReceiver, or *BindTransceiver
	TLS     *tls.Config // nil for plain TCP; configuration is cloned
}

// Dial connects and binds. It never reconnects or retries requests.
// On success the caller owns the Session and should Close then Wait. Any
// transport opened by Dial is closed if connection setup or bind fails.
func Dial(ctx context.Context, address string, cfg ClientConfig) (*Session, error) {
	if cfg.Codec == nil || cfg.Bind == nil {
		return nil, ErrState
	}
	var e error
	cfg.Session, e = cfg.Session.normalized()
	if e != nil {
		return nil, e
	}
	var conn net.Conn
	if cfg.TLS != nil {
		d := tls.Dialer{Config: cfg.TLS.Clone()}
		conn, e = d.DialContext(ctx, "tcp", address)
	} else {
		d := net.Dialer{}
		conn, e = d.DialContext(ctx, "tcp", address)
	}
	if e != nil {
		logSetup(ctx, cfg.Session.Logger, "client", "connect", e)
		return nil, e
	}
	s, e := NewSession(conn, cfg.Codec, cfg.Session)
	if e != nil {
		conn.Close()
		return nil, e
	}
	if _, e = s.Bind(ctx, cfg.Bind); e != nil {
		s.Close()
		s.Wait()
		return nil, e
	}
	return s, nil
}
