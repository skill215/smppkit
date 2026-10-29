// SPDX-AI-Disclosure: ai-generated
// SPDX-AI-Model: GPT-6.0 Astra
// SPDX-AI-Provider: OpenAI
// SPDX-AI-Scope: Library code generated and revised with Codex under maintainer direction.
// SPDX-AI-Date: 2026-10-29

package codec

import (
	"io"

	"github.com/skill215/smppkit/internal/wire"
	"github.com/skill215/smppkit/pdu"
)

// ReadBind is the bounded server bootstrap. It accepts only bind requests and
// exact version bytes 0x33, 0x34, 0x50; no compatibility alias is inferred.
// Profiles are selected by the server after this operation, before any response.
func ReadBind(r io.Reader, maxFrame int) (pdu.PDU, pdu.Version, error) {
	b, e := ReadFrame(r, maxFrame)
	if e != nil {
		return pdu.PDU{}, 0, e
	}
	limit, _ := frameLimit(maxFrame) // already validated without consuming on failure
	p, v, e := decodeBind(b, limit)
	if e != nil {
		e = readFailure(e, len(b), true)
	}
	return p, v, e
}

func decodeBind(b []byte, limit int) (packet pdu.PDU, version pdu.Version, err error) {
	defer func() {
		if err != nil {
			if _, wrapped := err.(*DecodeError); !wrapped {
				err = decodeFailure(err, 0, b, limit)
			}
		}
	}()
	scan := wire.Reader{Data: b[16:]}
	scan.C(16, "system_id")
	scan.C(9, "password")
	scan.C(13, "system_type")
	v := pdu.Version(scan.U8("interface_version"))
	if scan.Err != nil {
		return pdu.PDU{}, v, scan.Err
	}
	c, e := New(Config{Version: v, MaxFrame: limit})
	if e != nil {
		return pdu.PDU{}, v, e
	}
	p, e := c.Decode(b)
	if e != nil {
		return p, v, e
	}
	switch p.Body.(type) {
	case *pdu.BindReceiver, *pdu.BindTransmitter, *pdu.BindTransceiver:
		return p, v, nil
	}
	return p, v, ErrValidation
}
