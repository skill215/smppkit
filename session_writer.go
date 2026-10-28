// SPDX-AI-Disclosure: ai-generated
// SPDX-AI-Model: GPT-6.0 Astra
// SPDX-AI-Provider: OpenAI
// SPDX-AI-Scope: Library code generated and revised with Codex under maintainer direction.
// SPDX-AI-Date: 2026-10-28

package smpp

import (
	"context"
	"io"
	"sync"
	"time"
)

// writeJob transfers an immutable frame to the writer. mu protects only the
// queued-to-started decision; n and err become readable after done closes.
// after is an internal successful-write hook, never an application callback.
type writeJob struct {
	ctx                context.Context // checked by the writer before starting queued requests
	data               []byte
	mu                 sync.Mutex // protects cancellation versus starting a write
	started, cancelled bool
	n                  int
	err                error
	done               chan struct{}
	complete           sync.Once
	after              func()
}

func job(data []byte) *writeJob { return &writeJob{data: data, done: make(chan struct{})} }
func (j *writeJob) finish() {
	j.complete.Do(func() {
		// A completed/cancelled queue entry can remain reachable until the
		// writer polls again. Release its frame and captured inbound/context
		// objects now; result observers use byte counts, not this buffer.
		j.data, j.after, j.ctx = nil, nil, nil
		close(j.done)
	})
}

// cancelQueued finishes only jobs which have not started. No writer can mutate
// their observations after done closes, allowing prompt queue cancellation.
func (j *writeJob) cancelQueued(err error) bool {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.started {
		return false
	}
	if !j.cancelled {
		j.cancelled = true
		j.err = err
		j.finish()
	}
	return true
}

// writeLoop is the only goroutine allowed to write frames. Responses have a
// bounded priority burst so peer deadlines do not starve outbound requests or
// notifications. The two non-response queues alternate when both have work.
func (s *Session) writeLoop() {
	// Enqueue paths hold Session.mu and reject Closed, so shutdown can drain
	// the remaining jobs without racing a producer that publishes afterwards.
	defer func() {
		for {
			select {
			case j := <-s.requests:
				j.cancelQueued(s.Err())
			case j := <-s.responses:
				j.cancelQueued(s.Err())
			case j := <-s.notifications:
				j.cancelQueued(s.Err())
			default:
				return
			}
		}
	}()
	const responseBurstLimit = 8
	burst := 0
	preferNotice := false
	for {
		var j *writeJob
		if burst < responseBurstLimit {
			select {
			case j = <-s.responses:
				burst++
			default:
			}
		}
		if j == nil {
			// Alternate non-response queues when both are busy. Notifications
			// must not consume request slots or starve requests (or vice versa).
			first, second := s.requests, s.notifications
			if preferNotice {
				first, second = second, first
			}
			select {
			case j = <-first:
				preferNotice = !preferNotice
			default:
			}
			if j == nil {
				select {
				case j = <-second:
				default:
				}
			}
			if j != nil {
				burst = 0
			}
		}
		if j == nil {
			select {
			case <-s.done:
				return
			case j = <-s.responses:
				burst++
			case j = <-s.requests:
				burst = 0
				preferNotice = true
			case j = <-s.notifications:
				burst = 0
				preferNotice = false
			}
		}
		select {
		case <-s.done:
			j.cancelQueued(s.Err())
			return
		default:
		}
		// Cancellation can finish a queued job, but cannot alter an active write.
		// Once started, aborting the transport is the only safe way to stop it.
		j.mu.Lock()
		if !j.cancelled && j.ctx != nil && j.ctx.Err() != nil {
			j.cancelled = true
			j.err = j.ctx.Err()
			j.finish()
		}
		skip := j.cancelled
		if !skip {
			j.started = true
		}
		j.mu.Unlock()
		if skip {
			continue
		}
		e := s.conn.SetWriteDeadline(time.Now().Add(s.cfg.WriteTimeout))
		for e == nil && j.n < len(j.data) {
			var n int
			n, e = s.conn.Write(j.data[j.n:])
			if n < 0 || n > len(j.data)-j.n {
				e = io.ErrShortWrite
				break
			}
			j.n += n
			if n == 0 && e == nil {
				e = io.ErrNoProgress
			}
		}
		j.err = e
		if e == nil && j.after != nil {
			j.after()
		}
		j.finish()
		if e != nil {
			s.stop(e)
			return
		}
	}
}
