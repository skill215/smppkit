package message

import (
	"maps"
	"sync"
	"time"
)

// References reserves concatenation references until a first-allocation based
// retention deadline. Share one allocator across sessions in the same sending
// identity/domain if collisions across those connections must be prevented.
// At most MaxLeases live values exist; exhaustion never silently reuses one.
type References struct {
	mu        sync.Mutex
	bits, max int
	retention time.Duration
	next      uint32
	leases    map[uint16]time.Time
}

// NewReferences creates a concurrent allocator for an 8- or 16-bit reference
// space. Retention must cover the peer's expected reassembly/retransmission time.
func NewReferences(bits, maxLeases int, retention time.Duration) (*References, error) {
	if bits != 8 && bits != 16 || maxLeases < 1 || maxLeases > 1<<bits || retention <= 0 {
		return nil, ErrCapacity
	}
	return &References{bits: bits, max: maxLeases, retention: retention, leases: make(map[uint16]time.Time)}, nil
}

// Bits returns the immutable reference width.
func (r *References) Bits() int { return r.bits }

// Reserve expires old leases and reserves a free value without blocking. Zero
// is a valid reference. There is deliberately no early release: a submission
// timeout or even a positive response does not prove peer reassembly has ended.
// Callers supplying time must use a consistent clock for this allocator.
func (r *References) Reserve(now time.Time) (uint16, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	maps.DeleteFunc(r.leases, func(_ uint16, deadline time.Time) bool {
		return !now.Before(deadline)
	})
	if len(r.leases) >= r.max {
		return 0, ErrCapacity
	}
	space := uint32(1) << r.bits
	for range space {
		r.next = (r.next + 1) % space
		ref := uint16(r.next)
		if _, ok := r.leases[ref]; !ok {
			r.leases[ref] = now.Add(r.retention)
			return ref, nil
		}
	}
	return 0, ErrCapacity
}
