package message

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"maps"
	"sync"
	"time"

	"github.com/skill215/smppkit/pdu"
	"github.com/skill215/smppkit/textcodec"
)

// AckPolicy states what admission promises before an SMPP success response.
type AckPolicy uint8

// AckOnBuffered acknowledges bounded memory admission. It cannot survive a
// process crash; the caller must send an SMPP response only after Admit succeeds.
const AckOnBuffered AckPolicy = 1

// ReceiveConfig bounds collecting and completed messages. Zero numeric limits
// use defaults; AckPolicy must always be explicit. Byte charges are accounting
// estimates, not a process heap ceiling, and exclude data handed to the caller.
type ReceiveConfig struct {
	AckPolicy         AckPolicy     // must be AckOnBuffered; persistence belongs to the application
	MaxGroups         int           // incomplete groups, default 1024
	MaxBytes          int           // retained segment and assembled-payload charges, default 8 MiB
	MaxMessageBytes   int           // segment-storage charges per message, default 128 KiB
	MaxSegments       int           // declared concatenation total, default 255
	QueueSize         int           // completed messages waiting for Receive, default 32
	MaxTombstones     int           // retained completed-message signatures, default 1024
	EventLimit        int           // expiry events waiting for Events, default 128
	Lifetime          time.Duration // from first segment admission, default 5 minutes
	TombstoneLifetime time.Duration // from completion, default 5 minutes
	// DCS interpretation is explicit. Nil retains binary content without decoding.
	Encodings map[uint8]textcodec.Encoding
}

func (c ReceiveConfig) normalized() (ReceiveConfig, error) {
	if c.AckPolicy != AckOnBuffered {
		return c, ErrMetadata
	}
	if c.MaxGroups == 0 {
		c.MaxGroups = 1024
	}
	if c.MaxBytes == 0 {
		c.MaxBytes = 8 << 20
	}
	if c.MaxMessageBytes == 0 {
		c.MaxMessageBytes = 128 << 10
	}
	if c.MaxSegments == 0 {
		c.MaxSegments = 255
	}
	if c.QueueSize == 0 {
		c.QueueSize = 32
	}
	if c.MaxTombstones == 0 {
		c.MaxTombstones = 1024
	}
	if c.EventLimit == 0 {
		c.EventLimit = 128
	}
	if c.Lifetime == 0 {
		c.Lifetime = 5 * time.Minute
	}
	if c.TombstoneLifetime == 0 {
		c.TombstoneLifetime = 5 * time.Minute
	}
	if c.MaxGroups < 1 || c.MaxGroups > 65536 || c.MaxBytes < 1 || c.MaxBytes > 1<<30 || c.MaxMessageBytes < 1 || c.MaxMessageBytes > c.MaxBytes || c.MaxSegments < 1 || c.MaxSegments > 255 || c.QueueSize < 1 || c.QueueSize > 65536 || c.MaxTombstones < 1 || c.MaxTombstones > 65536 || c.EventLimit < 1 || c.EventLimit > 65536 || c.Lifetime <= 0 || c.TombstoneLifetime <= 0 {
		return c, ErrCapacity
	}
	enc := make(map[uint8]textcodec.Encoding, len(c.Encodings))
	for d, e := range c.Encodings {
		if e == nil || e.DataCoding() != d {
			return c, ErrMetadata
		}
		enc[d] = e
	}
	c.Encodings = enc
	return c, nil
}

// groupKey separates tenant/session scope, full addresses (including TON/NPI),
// concatenation mechanism and application ports. A reference alone is not unique.
type groupKey struct {
	namespace           string
	source, destination pdu.Address
	mode                Mode
	ref                 uint16
	ports               string
}
type group struct {
	segments      map[uint8]Segment
	signatures    map[uint8][32]byte
	first         Segment
	deadline      time.Time
	size, payload int
	id            string
}

// tombstone retains fingerprints, not message payloads, after completion. It
// suppresses retransmissions only for its bounded lifetime; this is not a durable
// exactly-once guarantee across process restarts or reference reuse.
type tombstone struct {
	id         string
	signatures map[uint8][32]byte
	deadline   time.Time
}

// Event reports an expired group; no payload or credential values are included.
type Event struct {
	Kind                string
	Namespace, ID       string
	Source, Destination pdu.Address
	Missing             []uint8
}

// Message owns its assembled payload and per-segment provenance after Receive.
// DecodeError does not discard raw content or undo previously sent segment ACKs.
// ID is local; it is not a peer message ID or proof of final delivery.
type Message struct {
	ID, Namespace       string
	Source, Destination pdu.Address
	DataCoding          uint8
	Payload             []byte
	Text                string
	Decoded             bool
	DecodeError         error
	Segments            []Segment
	Receipt             bool
	size                int
}

// Admission describes one segment's bounded memory acceptance. Complete means
// a new logical message was queued by this admission; duplicates do not requeue.
// Neither Complete nor Duplicate implies application consumption or durability.
type Admission struct {
	Duplicate, Complete bool
	ID                  string
}

// Stats is a synchronized accounting snapshot, not a reservation of capacity.
type Stats struct{ Groups, BufferedBytes, Queued, Tombstones, Events int }

// Receiver is a bounded, concurrency-safe memory store. Admit atomically
// commits segment and completion queue admission. Receive transfers ownership
// out of its resource budget. Expire must be called periodically; event pressure
// retains state and returns ErrCapacity instead of silently dropping evidence.
// Conflicts/capacity/malformed inputs are returned synchronously by Admit.
type Receiver struct {
	mu     sync.Mutex
	cfg    ReceiveConfig
	groups map[groupKey]*group
	tombs  map[groupKey]tombstone
	queue  []Message
	events []Event
	used   int
	ready  chan struct{}
}

// NewReceiver starts no goroutines. Its owner must call Expire and drain Events;
// the smpp Session.RunReceiver adapter supplies the periodic expiry sweep.
// The encoding map is copied; Encoding implementations remain shared trusted
// objects. Receive invokes Decode outside the receiver lock after transferring
// the message out of the budget, so consumers must bound their own retained data.
func NewReceiver(cfg ReceiveConfig) (*Receiver, error) {
	cfg, e := cfg.normalized()
	if e != nil {
		return nil, e
	}
	return &Receiver{cfg: cfg, groups: make(map[groupKey]*group), tombs: make(map[groupKey]tombstone), ready: make(chan struct{}, 1)}, nil
}
func newID() string {
	var b [16]byte
	if _, e := rand.Read(b[:]); e != nil {
		panic("smpp: system entropy unavailable")
	}
	return hex.EncodeToString(b[:])
}

// signal is a coalescing wakeup, not a message counter. Receive always checks
// the queue under mu and signals another waiter when more messages remain.
func (r *Receiver) signal() {
	select {
	case r.ready <- struct{}{}:
	default:
	}
}

// Stats reports current bounded-store occupancy under the receiver lock.
func (r *Receiver) Stats() Stats {
	r.mu.Lock()
	defer r.mu.Unlock()
	return Stats{len(r.groups), r.used, len(r.queue), len(r.tombs), len(r.events)}
}
func consistent(a, b Segment) bool {
	return a.Fields.DataCoding == b.Fields.DataCoding && a.Concat.Total == b.Concat.Total && a.Fields.ServiceType == b.Fields.ServiceType && a.Fields.ProtocolID == b.Fields.ProtocolID && a.Fields.ESMClass == b.Fields.ESMClass && bytes.Equal(a.UDH, b.UDH)
}

// Admit requires an explicit namespace, normally one session generation. Share
// a namespace and Receiver across binds only when their tenant scope permits it.
func (r *Receiver) Admit(namespace string, fields pdu.SMFields, now time.Time) (Admission, error) {
	var result Admission
	if namespace == "" || len(namespace) > 128 {
		return result, ErrMetadata
	}
	s, e := ParseSegment(fields)
	if e != nil {
		return result, e
	}
	cost := segmentBytes(s)
	r.mu.Lock()
	defer r.mu.Unlock()
	if cost > r.cfg.MaxMessageBytes {
		return result, ErrCapacity
	}
	if s.Concat == nil || s.Receipt {
		if len(r.queue) >= r.cfg.QueueSize {
			return result, ErrCapacity
		}
		m := Message{ID: newID(), Namespace: namespace, Source: fields.Source, Destination: fields.Destination, DataCoding: fields.DataCoding, Payload: append([]byte(nil), s.Payload...), Segments: []Segment{s}, Receipt: s.Receipt, size: cost + len(s.Payload)}
		if r.used+m.size > r.cfg.MaxBytes {
			return result, ErrCapacity
		}
		r.queue = append(r.queue, m)
		r.used += m.size
		r.signal()
		return Admission{Complete: true, ID: m.ID}, nil
	}
	concat := s.Concat
	if int(concat.Total) > r.cfg.MaxSegments {
		return result, ErrCapacity
	}
	key := groupKey{namespace, fields.Source, fields.Destination, concat.Mode, concat.Reference, string(s.Ports)}
	sig := signature(s)
	if tomb, ok := r.tombs[key]; ok {
		if now.Before(tomb.deadline) {
			if tomb.signatures[concat.Number] != sig {
				return result, ErrConflict
			}
			return Admission{Duplicate: true, ID: tomb.id}, nil
		}
		delete(r.tombs, key)
	}
	g, exists := r.groups[key]
	if exists {
		result.ID = g.id
		if !now.Before(g.deadline) {
			return result, ErrCapacity
		}
		if !consistent(g.first, s) {
			return result, ErrConflict
		}
		if old, ok := g.signatures[concat.Number]; ok {
			if old != sig {
				return result, ErrConflict
			}
			result.Duplicate = true
			return result, nil
		}
	} else {
		if len(r.groups) >= r.cfg.MaxGroups {
			return result, ErrCapacity
		}
		g = &group{segments: make(map[uint8]Segment), signatures: make(map[uint8][32]byte), first: s, deadline: now.Add(r.cfg.Lifetime), id: newID()}
		result.ID = g.id
	}
	if g.size+cost > r.cfg.MaxMessageBytes || r.used+cost > r.cfg.MaxBytes {
		return result, ErrCapacity
	}
	complete := len(g.segments)+1 == int(concat.Total)
	// Final admission reserves the assembled payload and queue/tombstone slots
	// before mutating the group, so a rejected segment can be retried by the peer.
	if complete && (len(r.queue) >= r.cfg.QueueSize || len(r.tombs) >= r.cfg.MaxTombstones || r.used+cost+g.payload+len(s.Payload) > r.cfg.MaxBytes) {
		return result, ErrCapacity
	}
	g.segments[concat.Number] = s
	g.signatures[concat.Number] = sig
	g.size += cost
	g.payload += len(s.Payload)
	r.used += cost
	r.groups[key] = g
	if !complete {
		return result, nil
	}
	m := Message{ID: g.id, Namespace: namespace, Source: fields.Source, Destination: fields.Destination, DataCoding: fields.DataCoding, Payload: make([]byte, 0, g.payload), Segments: make([]Segment, 0, int(concat.Total)), size: g.size + g.payload}
	for i := 1; i <= int(concat.Total); i++ {
		part := g.segments[uint8(i)]
		m.Payload = append(m.Payload, part.Payload...)
		m.Segments = append(m.Segments, part)
	}
	delete(r.groups, key)
	r.tombs[key] = tombstone{id: g.id, signatures: g.signatures, deadline: now.Add(r.cfg.TombstoneLifetime)}
	r.queue = append(r.queue, m)
	r.used += g.payload
	result.Complete = true
	r.signal()
	return result, nil
}

// Receive transfers the next complete message and releases its store charges.
// Optional text decoding runs outside the store lock; decoder failure is retained
// in Message.DecodeError. An available queued message takes precedence over ctx.
func (r *Receiver) Receive(ctx context.Context) (Message, error) {
	for {
		r.mu.Lock()
		if len(r.queue) > 0 {
			m := r.queue[0]
			r.queue[0] = Message{}
			r.queue = r.queue[1:]
			r.used -= m.size
			enc := r.cfg.Encodings[m.DataCoding]
			if len(r.queue) > 0 {
				r.signal()
			}
			r.mu.Unlock()
			if enc != nil && !m.Receipt {
				m.Text, m.DecodeError = enc.Decode(m.Payload)
				m.Decoded = m.DecodeError == nil
			}
			return m, nil
		}
		r.mu.Unlock()
		select {
		case <-ctx.Done():
			return Message{}, ctx.Err()
		case <-r.ready:
		}
	}
}

// Expire records bounded expiry events before releasing incomplete groups. The
// deadline is fixed by first admission; duplicate traffic cannot extend it.
func (r *Receiver) Expire(now time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	maps.DeleteFunc(r.tombs, func(_ groupKey, t tombstone) bool {
		return !now.Before(t.deadline)
	})
	for key, g := range r.groups {
		if now.Before(g.deadline) {
			continue
		}
		if len(r.events) >= r.cfg.EventLimit {
			return ErrCapacity
		}
		event := Event{Kind: "expired", Namespace: key.namespace, ID: g.id, Source: key.source, Destination: key.destination}
		for i := 1; i <= int(g.first.Concat.Total); i++ {
			if _, ok := g.segments[uint8(i)]; !ok {
				event.Missing = append(event.Missing, uint8(i))
			}
		}
		r.events = append(r.events, event)
		r.used -= g.size
		delete(r.groups, key)
	}
	return nil
}

// Events transfers all currently recorded events to the caller.
func (r *Receiver) Events() []Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := r.events
	r.events = nil
	return out
}
