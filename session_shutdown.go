package smpp

import (
	"errors"
	"io"
	"net"
)

// The peer can consume our ACK and close before Write returns or its completion
// hook runs. If all required incoming unbind information is already present,
// let the queued/active ACK writer report success/failure and close the Session.
// Do not suppress timeouts, malformed frames or EOF while our response is missing.
func (s *Session) finishUnbindOnWriter(err error) bool {
	if !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrClosedPipe) && !errors.Is(err, net.ErrClosed) {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state == Unbinding && s.peerUnbind && (!s.localUnbind || s.localUnbindDone)
}

// unbindCompleteLocked requires mu. Either endpoint may initiate unbind. Once
// both have done so, both response obligations must finish before closing.
// RequestTimeout/WriteTimeout still bound a missing response or blocked ACK.
func (s *Session) unbindCompleteLocked() bool {
	if s.localUnbind {
		return s.localUnbindDone && (!s.peerUnbind || s.peerUnbindDone)
	}
	return s.peerUnbind && s.peerUnbindDone
}

// Called only after the whole peer unbind ACK was written. Never close from
// inside mu: net.Conn.Close may block while interrupting transport operations.
func (s *Session) finishPeerUnbind() {
	s.mu.Lock()
	s.peerUnbindDone = true
	complete := s.unbindCompleteLocked()
	s.mu.Unlock()
	if complete {
		s.stop(ErrClosed)
	}
}
