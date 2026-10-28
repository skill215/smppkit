// A neutral private command pair can be registered without TLV registration.
package main

import (
	"errors"
	"fmt"
	"github.com/skill215/smppkit/codec"
	"github.com/skill215/smppkit/pdu"
	"log"
)

const echoID pdu.CommandID = 0x10200
const echoRespID pdu.CommandID = 0x80010200

type echo struct{ Value byte }

func (*echo) CommandID() pdu.CommandID { return echoID }

type echoResp struct{ Value byte }

func (*echoResp) CommandID() pdu.CommandID { return echoRespID }

type echoCodec struct{ response bool }

func (c echoCodec) DecodeBody(src []byte) (pdu.Body, error) {
	if len(src) != 1 {
		return nil, errors.New("echo: expected one octet")
	}
	if c.response {
		return &echoResp{src[0]}, nil
	}
	return &echo{src[0]}, nil
}
func (c echoCodec) AppendBody(dst []byte, body pdu.Body) ([]byte, error) {
	if c.response {
		if b, ok := body.(*echoResp); ok {
			return append(dst, b.Value), nil
		}
	} else {
		if b, ok := body.(*echo); ok {
			return append(dst, b.Value), nil
		}
	}
	return dst, errors.New("echo: unexpected body type")
}
func main() {
	c, e := codec.New(codec.Config{Version: pdu.V50, Commands: []codec.CommandDefinition{{Request: echoID, Response: echoRespID, Versions: []pdu.Version{pdu.V50}, RequestCodec: echoCodec{}, ResponseCodec: echoCodec{response: true}}}})
	if e != nil {
		log.Fatal(e)
	}
	b, e := c.Append(nil, pdu.PDU{Sequence: 1, Body: &echo{Value: 42}})
	if e != nil {
		log.Fatal(e)
	}
	fmt.Printf("%x\n", b)
}
