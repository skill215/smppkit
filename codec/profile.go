// Package codec implements bounded, version-specific SMPP encoding independent
// of transport or session ownership. Codecs are immutable and safe to share.
package codec

import (
	"errors"
	"fmt"
	"slices"

	"github.com/skill215/smppkit/pdu"
)

var (
	ErrUnsupported = errors.New("smpp: unsupported standard command or version")
	ErrValidation  = errors.New("smpp: protocol validation failed")
)

// CommandCodec is trusted extension code. It must be concurrency safe, return
// owned data and never retain/mutate input. AppendBody must not change dst on
// failure. The enclosing Codec checks command identity and frame limits.
// Calls are synchronous and may run on Session's reader. Bound their work and
// allocation; never wait for traffic from that Session inside an extension.
type CommandCodec interface {
	DecodeBody([]byte) (pdu.Body, error)
	AppendBody([]byte, pdu.Body) ([]byte, error)
}

// CommandDefinition registers a request/response pair. Both codecs are required;
// commands are allowed in bound sessions, in either direction.
type CommandDefinition struct {
	Request, Response           pdu.CommandID
	Versions                    []pdu.Version
	RequestCodec, ResponseCodec CommandCodec
}

// TLVDefinition constrains a customer tag (0x1400..0x3fff) for this Codec only.
// Standard/reserved definitions cannot be replaced. Unknown tags remain raw.
type TLVDefinition struct {
	Tag                  uint16
	MinLength, MaxLength int
	Commands             []pdu.CommandID
	Versions             []pdu.Version
	Repeatable           bool
	// Validate runs after declared bounds, version, command and cardinality checks.
	// It must be concurrency safe and must not retain or mutate its input.
	// It runs synchronously, including on Session's reader; return promptly.
	Validate func([]byte) error
}

// Config selects one wire version and optional trusted extension definitions.
// Version is required; an omitted or unknown version is never guessed.
type Config struct {
	Version pdu.Version
	// MaxFrame defaults to 64 KiB and is capped at 16 MiB.
	MaxFrame int
	Commands []CommandDefinition
	TLVs     []TLVDefinition
}

// Codec is an immutable wire profile, safe to share between sessions and
// standalone simulators. It owns no transport, sequencing or bind state.
type Codec struct {
	version   pdu.Version
	max       int
	commands  map[pdu.CommandID]CommandCodec
	responses map[pdu.CommandID]pdu.CommandID
	tlvs      map[uint16]TLVDefinition
}

// New validates and copies registration metadata. Callback implementations
// remain shared and must satisfy their concurrency and input-ownership contracts.
func New(cfg Config) (*Codec, error) {
	if !cfg.Version.Valid() {
		return nil, fmt.Errorf("%w: interface_version", ErrUnsupported)
	}
	if cfg.MaxFrame == 0 {
		cfg.MaxFrame = 64 << 10
	}
	if cfg.MaxFrame < 16 || cfg.MaxFrame > 16<<20 {
		return nil, fmt.Errorf("%w: MaxFrame", ErrValidation)
	}
	c := &Codec{cfg.Version, cfg.MaxFrame, make(map[pdu.CommandID]CommandCodec), make(map[pdu.CommandID]pdu.CommandID), make(map[uint16]TLVDefinition)}
	for _, d := range cfg.Commands {
		if d.Request == 0 || d.Request.IsResponse() || !d.Response.IsResponse() || d.Response == pdu.GenericNACKID || standard(d.Request) || standard(d.Response) || d.RequestCodec == nil || d.ResponseCodec == nil || len(d.Versions) == 0 {
			return nil, fmt.Errorf("%w: private command definition", ErrValidation)
		}
		for _, v := range d.Versions {
			if !v.Valid() {
				return nil, ErrValidation
			}
		}
		if _, ok := c.commands[d.Request]; ok {
			return nil, ErrValidation
		}
		if _, ok := c.commands[d.Response]; ok {
			return nil, ErrValidation
		}
		// Reserve conflicting IDs even when this version does not activate them.
		c.commands[d.Request] = d.RequestCodec
		c.commands[d.Response] = d.ResponseCodec
		if slices.Contains(d.Versions, c.version) {
			c.responses[d.Request] = d.Response
		} else {
			c.commands[d.Request] = nil
			c.commands[d.Response] = nil
		}
	}
	for _, d := range cfg.TLVs {
		if standardTag(d.Tag) || d.MinLength < 0 || d.MaxLength < d.MinLength || d.MaxLength > 65535 || len(d.Commands) == 0 || len(d.Versions) == 0 {
			return nil, fmt.Errorf("%w: customer TLV definition", ErrValidation)
		}
		if _, ok := c.tlvs[d.Tag]; ok {
			return nil, ErrValidation
		}
		for _, v := range d.Versions {
			if !v.Valid() || v == pdu.V33 {
				return nil, ErrValidation
			}
		}
		d.Commands = slices.Clone(d.Commands)
		d.Versions = slices.Clone(d.Versions)
		c.tlvs[d.Tag] = d
	}
	return c, nil
}

// Version returns the fixed wire profile, not a negotiated session capability.
func (c *Codec) Version() pdu.Version { return c.version }

// MaxFrame includes the 16-byte SMPP header and bounds both encoding and reading.
func (c *Codec) MaxFrame() int { return c.max }

// The standard and reserved command range is never an opaque/private bypass.
func standard(id pdu.CommandID) bool { return id&^pdu.ResponseBit <= 0x1ff }

func (c *Codec) supported(id pdu.CommandID) bool {
	base := id &^ pdu.ResponseBit
	if base == 0 {
		return id == pdu.GenericNACKID
	}
	switch base {
	case 1, 2, 3, 4, 5, 6, 7, 8, 0x15, 0x21:
		return true
	case 9, 0xb, 0x102, 0x103:
		return c.version != pdu.V33 && (!id.IsResponse() || base != 0xb && base != 0x102)
	}
	return false
}

// ResponseCommand returns the expected response only for implemented operations.
func (c *Codec) ResponseCommand(id pdu.CommandID) (pdu.CommandID, bool) {
	if id.IsResponse() {
		return 0, false
	}
	if resp, ok := c.responses[id]; ok {
		return resp, true
	}
	if c.supported(id) && id != pdu.OutbindID && id != pdu.AlertNotificationID {
		return id | pdu.ResponseBit, true
	}
	return 0, false
}
func standardTag(t uint16) bool {
	// Standard/reserved ranges cannot be replaced by customer definitions.
	return t < 0x1400 || t > 0x3fff
}
