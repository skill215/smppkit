// A simulator can own its transport and use only the standalone codec.
package main

import (
	"encoding/hex"
	"fmt"
	"github.com/skill215/smppkit/codec"
	"github.com/skill215/smppkit/pdu"
	"log"
)

func main() {
	c, e := codec.New(codec.Config{Version: pdu.V34})
	if e != nil {
		log.Fatal(e)
	}
	wire, _ := hex.DecodeString("0000001000000015000000000000002a")
	packet, e := c.Decode(wire)
	if e != nil {
		log.Fatal(e)
	}
	response, e := c.Append(nil, pdu.PDU{Sequence: packet.Sequence, Body: &pdu.EnquireLinkResp{}})
	if e != nil {
		log.Fatal(e)
	}
	fmt.Printf("%x\n", response)
}
