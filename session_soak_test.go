package smpp

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"runtime"
	"runtime/pprof"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/skill215/smppkit/codec"
	"github.com/skill215/smppkit/message"
	"github.com/skill215/smppkit/pdu"
)

// Run explicitly with SMPPKIT_SOAK_DURATION=60s. Ordinary test runs retain the
// deterministic lifecycle/boundary suite and do not pay for a timed soak.
// This is a resource regression guard, not a throughput benchmark: deliberate
// slow consumers, race instrumentation and periodic GC all change throughput.
func TestSoakLocalResources(t *testing.T) {
	setting := os.Getenv("SMPPKIT_SOAK_DURATION")
	if setting == "" {
		t.Skip("set SMPPKIT_SOAK_DURATION, e.g. 60s, for the local TCP resource soak")
	}
	duration, err := time.ParseDuration(setting)
	if err != nil || duration < time.Second || duration > 30*time.Minute {
		t.Fatal("duration must be 1s..30m", err)
	}
	c := testCodec(t)
	batch := func() {
		errs := make(chan error, 4)
		for range 4 {
			go func() { errs <- soakConnection(t.Context(), c) }()
		}
		var batchErr error
		for range 4 {
			if err := <-errs; err != nil && batchErr == nil {
				batchErr = err
			}
		}
		if batchErr != nil {
			t.Fatal(batchErr)
		} // join every connection even on failure
		if err := soakReceiverPressure(); err != nil {
			t.Fatal(err)
		}
	}
	// Warm up network pollers, goroutine stacks and code paths before comparing
	// live allocations. No t.Cleanup retains completed sessions between cycles.
	batch()
	baseline := soakSnapshot()
	t.Logf("Go=%s OS=%s arch=%s GOMAXPROCS=%d; TCP pairs/batch=4 window=4 inbound=8 notifications=8; baseline %+v", runtime.Version(), runtime.GOOS, runtime.GOARCH, runtime.GOMAXPROCS(0), baseline)
	start := time.Now()
	nextSample := start.Add(duration / 6)
	batches := 0
	for time.Since(start) < duration {
		batch()
		batches++
		if !time.Now().Before(nextSample) {
			sample := soakSnapshot()
			t.Logf("elapsed=%s batches=%d connections=%d submits/deliveries=%d notifications=%d controls=%d snapshot=%+v", time.Since(start).Round(time.Millisecond), batches, batches*4, batches*4*64, batches*4*8, batches*4*16, sample)
			// Exact queue/map/slot cleanup is checked per connection below. This
			// generous heap delta catches growth missed by those counters without
			// pretending runtime allocator noise is a portable byte-exact budget.
			if sample.HeapAlloc > baseline.HeapAlloc+(8<<20) {
				t.Fatalf("post-GC live heap grew by >8 MiB: baseline=%+v current=%+v", baseline, sample)
			}
			if sample.SessionLoops != baseline.SessionLoops {
				t.Fatalf("Session goroutine growth: %+v -> %+v", baseline, sample)
			}
			if baseline.FDs >= 0 && sample.FDs > baseline.FDs+2 {
				t.Fatalf("descriptor growth: %+v -> %+v", baseline, sample)
			}
			nextSample = time.Now().Add(duration / 6)
		}
	}
	final := soakSnapshot()
	t.Logf("completed %d batches / %d TCP connection pairs in %s; final %+v", batches, batches*4, time.Since(start).Round(time.Millisecond), final)
	if final.HeapAlloc > baseline.HeapAlloc+(8<<20) || final.SessionLoops != baseline.SessionLoops || baseline.FDs >= 0 && final.FDs > baseline.FDs+2 {
		t.Fatal("final resources did not recover", baseline, final)
	}
}

type soakResources struct {
	HeapAlloc, HeapInuse, HeapObjects uint64
	SessionLoops, FDs                 int
}

func soakSnapshot() soakResources {
	runtime.GC()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	var stacks strings.Builder
	_ = pprof.Lookup("goroutine").WriteTo(&stacks, 2)
	text := stacks.String()
	loops := strings.Count(text, "smppkit.(*Session).readLoop(") + strings.Count(text, "smppkit.(*Session).writeLoop(") + strings.Count(text, "smppkit.(*Session).expiryLoop(")
	fds := -1 // descriptor assertion is available on Linux only
	if entries, err := os.ReadDir("/proc/self/fd"); err == nil {
		fds = len(entries)
	}
	return soakResources{m.HeapAlloc, m.HeapInuse, m.HeapObjects, loops, fds}
}

// Each cycle has a real TCP bind, simultaneous submit/deliver with four callers
// per side, two slow application consumers, notifications and control traffic.
// Every request/response is counted and checked before graceful shutdown.
func soakConnection(parent context.Context, c *codec.Codec) error {
	ctx, cancel := context.WithTimeout(parent, 5*time.Second)
	defer cancel()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	defer ln.Close()
	// Notifications have no protocol ACK/backpressure. Reserve the whole burst
	// so scheduler delays in their consumer do not intentionally overflow it.
	cfg := Config{Window: 4, InboundLimit: 8, NotificationLimit: 8, RequestTimeout: 3 * time.Second, InboundTimeout: 3 * time.Second, ReadTimeout: 3 * time.Second, WriteTimeout: 3 * time.Second}
	srv, err := NewServer(ServerConfig{Profiles: []*codec.Codec{c}, Session: cfg, Authenticate: func(context.Context, BindInfo) (BindDecision, error) { return BindDecision{SystemID: "soak"}, nil }})
	if err != nil {
		return err
	}
	type accepted struct {
		s   *Session
		err error
	}
	ready := make(chan accepted, 1)
	go func() {
		conn, e := ln.Accept()
		if e != nil {
			ready <- accepted{err: e}
			return
		}
		s, e := srv.Accept(ctx, conn)
		ready <- accepted{s, e}
	}()
	client, err := Dial(ctx, ln.Addr().String(), ClientConfig{Codec: c, Session: cfg, Bind: &pdu.BindTransceiver{BindFields: pdu.BindFields{InterfaceVersion: pdu.V50}}})
	if err != nil {
		ln.Close()
		a := <-ready
		if a.s != nil {
			a.s.Close()
			a.s.Wait()
		}
		return err
	}
	defer func() { client.Close(); client.Wait() }()
	a := <-ready
	if a.err != nil {
		return a.err
	}
	server := a.s
	defer func() { server.Close(); server.Wait() }()
	var wg sync.WaitGroup
	errs := make(chan error, 16)
	spawn := func(fn func() error) {
		wg.Go(func() {
			if e := fn(); e != nil {
				errs <- e
				cancel()
				client.Close()
				server.Close()
			}
		})
	}
	for _, endpoint := range []*Session{client, server} {
		spawn(func() error {
			seen := make(map[int]bool, 32)
			for i := range 32 {
				in, e := endpoint.Receive(ctx)
				if e != nil {
					return e
				}
				var payload []byte
				var response pdu.Body
				switch b := in.PDU.Body.(type) {
				case *pdu.SubmitSM:
					payload = b.ShortMessage
					response = &pdu.SubmitSMResp{}
				case *pdu.DeliverSM:
					payload = b.ShortMessage
					response = &pdu.DeliverSMResp{}
				default:
					return fmt.Errorf("unexpected inbound %T", in.PDU.Body)
				}
				if len(payload) != 144 || string(payload[3:]) != ":"+strings.Repeat("x", 140) {
					return errors.New("payload changed")
				}
				if submit, ok := response.(*pdu.SubmitSMResp); ok {
					submit.MessageID = string(payload[:3])
				}
				n, e := strconv.Atoi(string(payload[:3]))
				if e != nil || n < 0 || n >= 32 || seen[n] {
					return fmt.Errorf("duplicate/invalid payload %q", payload[:3])
				}
				seen[n] = true
				if i%4 == 0 {
					time.Sleep(100 * time.Microsecond)
				}
				if e = in.Respond(ctx, pdu.OK, response); e != nil {
					return e
				}
			}
			return nil
		})
		for worker := range 4 {
			spawn(func() error {
				for i := worker * 8; i < (worker+1)*8; i++ {
					fields := pdu.SMFields{ShortMessage: fmt.Appendf(nil, "%03d:%s", i, strings.Repeat("x", 140))}
					var body pdu.Body = &pdu.SubmitSM{SMFields: fields}
					if endpoint == server {
						body = &pdu.DeliverSM{SMFields: fields}
					}
					r, e := endpoint.Request(ctx, body)
					if e != nil {
						return e
					}
					if r.Outcome != ResponseReceived {
						return errors.New("missing response")
					}
					if endpoint == client && r.Response.Body.(*pdu.SubmitSMResp).MessageID != fmt.Sprintf("%03d", i) {
						return errors.New("wrong correlation")
					}
				}
				return nil
			})
		}
		spawn(func() error {
			for range 8 {
				if _, e := endpoint.Request(ctx, &pdu.EnquireLink{}); e != nil {
					return e
				}
			}
			return nil
		})
	}
	spawn(func() error {
		for range 8 {
			if _, e := server.Notify(ctx, &pdu.AlertNotification{Source: pdu.Address{Value: "soak"}}); e != nil {
				return e
			}
		}
		return nil
	})
	spawn(func() error {
		for range 8 {
			n, e := client.ReceiveNotification(ctx)
			if e != nil {
				return e
			}
			if n.Body.(*pdu.AlertNotification).Source.Value != "soak" {
				return errors.New("notification changed")
			}
		}
		return nil
	})
	wg.Wait()
	close(errs)
	for e := range errs {
		if e != nil {
			return e
		}
	}
	if err = client.Shutdown(ctx); err != nil {
		return err
	}
	client.Wait()
	server.Wait()
	for _, s := range []*Session{client, server} {
		s.mu.Lock()
		retained := len(s.pending) + len(s.inbound)
		s.mu.Unlock()
		retained += len(s.requests) + len(s.responses) + len(s.notifications) + len(s.incoming) + len(s.notices) + len(s.slots) + len(s.notifySlots)
		if retained != 0 {
			return fmt.Errorf("retired session retains %d entries", retained)
		}
	}
	return nil
}

// Independently constructed UDH8 parts flood the bounded receiver, including
// duplicate traffic after saturation and expiry-event backpressure. Synthetic
// time makes this repeatable without slowing each soak batch by a real TTL.
func soakReceiverPressure() error {
	r, err := message.NewReceiver(message.ReceiveConfig{AckPolicy: message.AckOnBuffered, MaxGroups: 16, MaxBytes: 16 << 10, MaxMessageBytes: 2 << 10, QueueSize: 4, MaxTombstones: 16, EventLimit: 4, Lifetime: time.Second})
	if err != nil {
		return err
	}
	now := time.Unix(100, 0)
	part := func(ref byte) pdu.SMFields {
		return pdu.SMFields{ESMClass: 0x40, ShortMessage: []byte{5, 0, 3, ref, 2, 1, 'x'}}
	}
	for i := range 16 {
		if _, err = r.Admit("soak", part(byte(i)), now); err != nil {
			return err
		}
	}
	full := r.Stats()
	for i := range 256 {
		a, e := r.Admit("soak", part(byte(i%16)), now.Add(500*time.Millisecond))
		if e != nil || !a.Duplicate {
			return fmt.Errorf("duplicate at capacity: %+v %v", a, e)
		}
		if _, e = r.Admit("soak", part(byte(16+i%240)), now); !errors.Is(e, message.ErrCapacity) {
			return fmt.Errorf("flood admitted: %v", e)
		}
		if r.Stats() != full {
			return errors.New("rejected/duplicate flood changed occupancy")
		}
	}
	count := 0
	for r.Stats().Groups > 0 {
		e := r.Expire(now.Add(2 * time.Second))
		if e != nil && !errors.Is(e, message.ErrCapacity) {
			return e
		}
		events := r.Events()
		if len(events) == 0 || len(events) > 4 {
			return errors.New("expiry lost events or exceeded capacity")
		}
		for _, event := range events {
			if len(event.Missing) != 1 || event.Missing[0] != 2 {
				return errors.New("wrong missing-part evidence")
			}
		}
		count += len(events)
	}
	if count != 16 || r.Stats() != (message.Stats{}) {
		return fmt.Errorf("receiver did not recover: events=%d stats=%+v", count, r.Stats())
	}
	// A slow completed-message consumer must reject a final segment atomically,
	// keep its earlier segment and permit an explicit peer retry after draining.
	last := func(ref byte) pdu.SMFields {
		return pdu.SMFields{ESMClass: 0x40, ShortMessage: []byte{5, 0, 3, ref, 2, 2, 'y'}}
	}
	now = now.Add(3 * time.Second)
	for i := range 8 {
		if _, err = r.Admit("soak", part(byte(i)), now); err != nil {
			return err
		}
		before := r.Stats()
		admitted, e := r.Admit("soak", last(byte(i)), now)
		if i < 4 {
			if e != nil || !admitted.Complete {
				return fmt.Errorf("completion: %+v %v", admitted, e)
			}
		} else if !errors.Is(e, message.ErrCapacity) || r.Stats() != before {
			return fmt.Errorf("completion backpressure was not atomic: %v", e)
		}
	}
	seen := make(map[string]bool, 8)
	drain := func() error {
		// Cancelled context prevents a bug from hanging on an empty queue; the
		// receiver contract gives already queued messages precedence over ctx.
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		for range 4 {
			m, e := r.Receive(ctx)
			if e != nil || string(m.Payload) != "xy" || len(m.Segments) != 2 || seen[m.ID] {
				return fmt.Errorf("completion lost/duplicated: %v", e)
			}
			seen[m.ID] = true
		}
		return nil
	}
	if err = drain(); err != nil {
		return err
	}
	for i := 4; i < 8; i++ {
		if a, e := r.Admit("soak", last(byte(i)), now); e != nil || !a.Complete {
			return fmt.Errorf("explicit final-part retry: %+v %v", a, e)
		}
	}
	if err = drain(); err != nil {
		return err
	}
	if err = r.Expire(now.Add(10 * time.Minute)); err != nil || r.Stats() != (message.Stats{}) {
		return fmt.Errorf("completion resources retained: %+v %v", r.Stats(), err)
	}
	return nil
}

func TestReceiverFloodResourceRecovery(t *testing.T) {
	for range 8 {
		if err := soakReceiverPressure(); err != nil {
			t.Fatal(err)
		}
	}
}
