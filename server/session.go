// Copyright (c) 2026 The hislip developers. All rights reserved.
// Project site: https://github.com/gotmc/hislip
// Use of this source code is governed by a MIT-style license that
// can be found in the LICENSE.txt file for the project.

package server

import (
	"context"
	"sync"

	"github.com/gotmc/hislip/protocol"
)

// State is the lifecycle state of a session.
type State uint8

// Session lifecycle states.
const (
	// StatePairing means the synchronous channel has completed the
	// Initialize transaction but the asynchronous channel has not yet been
	// associated. Normal operations must be refused in this state.
	StatePairing State = iota

	// StateReady means both channels are associated and the session is usable.
	StateReady

	// StateClearing means a Device Clear transaction is in progress.
	// Synchronous messages are discarded until DeviceClearComplete arrives.
	StateClearing

	// StateClosed means the session has ended.
	StateClosed
)

// String returns a short description of the state.
func (s State) String() string {
	switch s {
	case StatePairing:
		return "pairing"
	case StateReady:
		return "ready"
	case StateClearing:
		return "clearing"
	case StateClosed:
		return "closed"
	default:
		return "unknown state"
	}
}

// Session is one logical HiSLIP session: a pair of TCP connections that share a
// session ID, a negotiated protocol version and a negotiated mode.
//
// A session becomes usable only after both channels have been associated. The
// server must refuse normal operations before that, per §9, with fatal error 2.
type Session struct {
	mu sync.Mutex

	id      uint16
	version protocol.Version
	overlap bool
	state   State

	sync  Stream
	async Stream

	// rmt and status hold the Synchronized Mode machinery. See rmt.go and
	// status.go; these are the two mechanisms that Synchronized Mode consists
	// of, as distinct from the ordering a well-behaved client happens to use.
	rmt    RMT
	status *Status

	// lastTx is the MessageID the server most recently sent in a DataEnd. It is
	// kept so that a response can be correlated after internal chunking: the
	// intermediate Data packets may carry NoMessageID while the terminating
	// DataEnd carries this.
	lastTx protocol.MessageID

	maxRx uint64
	maxTx uint64

	// opCancel abandons the in-flight bus operation. See Operation.
	opCancel context.CancelFunc
}

// newSession returns a session in StatePairing holding the synchronous channel.
func newSession(
	id uint16,
	version protocol.Version,
	syncStream Stream,
	cfg Config,
) *Session {
	return &Session{
		id:      id,
		version: version,
		state:   StatePairing,
		sync:    syncStream,
		status:  NewStatus(),
		lastTx:  protocol.NoMessageID,
		maxRx:   cfg.MaxRxPayload,
		maxTx:   cfg.MaxTxPayload,
	}
}

// ID returns the session ID allocated at initialization.
func (s *Session) ID() uint16 { return s.id }

// Version returns the negotiated protocol version.
func (s *Session) Version() protocol.Version { return s.version }

// Overlap reports whether Overlap Mode was negotiated. The PoE-to-GPIB profile
// always reports false.
func (s *Session) Overlap() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.overlap
}

// State returns the lifecycle state.
func (s *Session) State() State {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state
}

// MaxRxPayload and MaxTxPayload return the negotiated maximum payload sizes.
func (s *Session) MaxRxPayload() uint64 { return s.maxRx }
func (s *Session) MaxTxPayload() uint64 { return s.maxTx }

// RMT returns the session's RMT tracker.
func (s *Session) RMT() *RMT { return &s.rmt }

// Status returns the session's status and MAV tracker.
func (s *Session) Status() *Status { return s.status }

// LastTxMessageID returns the MessageID the server most recently sent in a
// DataEnd, or NoMessageID if it has sent none in this session.
//
// The response path needs this because a large instrument response is chunked:
// the intermediate Data packets may carry NoMessageID, but the terminating
// DataEnd must carry the MessageID of the client message that produced the
// response. See R-PROTO-022 and R-PROTO-023.
func (s *Session) LastTxMessageID() protocol.MessageID {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastTx
}

// setLastTxMessageID records the MessageID carried by a DataEnd the server has
// sent.
func (s *Session) setLastTxMessageID(id protocol.MessageID) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastTx = id
}

// attachAsync associates the asynchronous channel and moves the session to
// StateReady.
func (s *Session) attachAsync(stream Stream) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state != StatePairing {
		return ErrInvalidSession
	}
	if s.async != nil {
		return ErrInvalidSession
	}
	s.async = stream
	s.state = StateReady
	return nil
}

// Ready reports whether both channels are associated.
func (s *Session) Ready() bool {
	return s.State() == StateReady
}

// requireReady returns ErrSessionNotReady unless both channels are associated.
//
// This is the guard for "attempt to use connection without both channels
// established", fatal error code 2. It exists as a named method rather than an
// inline check so that every operation path is visibly subject to it.
func (s *Session) requireReady() error {
	switch s.State() {
	case StateReady, StateClearing:
		return nil
	case StateClosed:
		return ErrInvalidSession
	default:
		return ErrSessionNotReady
	}
}

// beginClear moves the session into StateClearing.
func (s *Session) beginClear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state == StateReady {
		s.state = StateClearing
	}
}

// completeClear resets the per-session protocol state and returns the session to
// StateReady with the given negotiated mode.
//
// Everything reset here is required by §13: MessageID tracking returns to its
// initial value, the RMT state clears, and any held response is discarded. The
// TCP session stays usable, which is the point of Device Clear as opposed to a
// fatal error.
func (s *Session) completeClear(overlap bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.overlap = overlap
	s.rmt.Reset()
	s.status.Reset()
	s.lastTx = protocol.NoMessageID
	if s.state == StateClearing {
		s.state = StateReady
	}
}

// close marks the session closed and closes both streams. Closing an already
// closed session is harmless.
func (s *Session) close() error {
	s.mu.Lock()
	streams := []Stream{s.sync, s.async}
	s.sync, s.async = nil, nil
	s.state = StateClosed
	cancel := s.opCancel
	s.opCancel = nil
	s.mu.Unlock()

	if cancel != nil {
		cancel()
	}

	var firstErr error
	for _, st := range streams {
		if st == nil {
			continue
		}
		if err := st.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// Operation returns a context for a bus operation, together with the function
// that releases it.
//
// The returned context is cancelled when the operation completes, when the
// session closes, or when a Device Clear abandons the operation. That last case
// is the mechanism behind R-SRV-016: a Device Clear that cannot interrupt a stuck
// instrument is useless, because a stuck instrument is the condition an operator
// issues one for.
//
// Only one operation may be outstanding per session, which the serialized bus
// engine of §46 guarantees. Registering a second replaces the first, so a leaked
// operation cannot make a later Device Clear abort the wrong work.
func (s *Session) Operation(parent context.Context) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(parent)
	s.mu.Lock()
	s.opCancel = cancel
	s.mu.Unlock()
	return ctx, func() {
		s.mu.Lock()
		if s.opCancel != nil {
			s.opCancel = nil
		}
		s.mu.Unlock()
		cancel()
	}
}

// abortOperation cancels the in-flight bus operation, if any.
func (s *Session) abortOperation() {
	s.mu.Lock()
	cancel := s.opCancel
	s.opCancel = nil
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}
