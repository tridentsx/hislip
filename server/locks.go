// Copyright (c) 2026 The hislip developers. All rights reserved.
// Project site: https://github.com/gotmc/hislip
// Use of this source code is governed by a MIT-style license that
// can be found in the LICENSE.txt file for the project.

package server

import (
	"context"
	"sync"
	"time"
)

// LockResult is the control code of an AsyncLockResponse.
//
// The same numeric value means different things for a request and a release,
// which is why the constants are named for the transaction they belong to rather
// than for the number. Code 1 answering a request means the lock was granted;
// code 1 answering a release means an exclusive lock was released.
type LockResult uint8

// Lock response control codes, from IVI-6.1 Tables 19 and 20.
const (
	// LockFailure answers a request that could not be granted before the
	// timeout expired. The lock is genuinely held elsewhere.
	LockFailure LockResult = 0

	// LockGranted answers a successful request. It is also the code for the
	// release of an exclusive lock.
	LockGranted LockResult = 1

	// LockReleasedShared answers the successful release of a shared lock.
	LockReleasedShared LockResult = 2

	// LockError answers a request that makes no sense, such as asking for a
	// lock already held by the asking client, or releasing one not held. It is
	// distinct from LockFailure: waiting would not help.
	LockError LockResult = 3
)

// String returns a short description.
func (r LockResult) String() string {
	switch r {
	case LockFailure:
		return "failure"
	case LockGranted:
		return "granted, or exclusive released"
	case LockReleasedShared:
		return "shared released"
	case LockError:
		return "error"
	default:
		return "unknown lock result"
	}
}

// LockState is the server's lock state, from IVI-6.1 Table 22.
type LockState uint8

// Lock states.
const (
	// Unlocked is the initial state and the state after the last holder
	// releases.
	Unlocked LockState = iota

	// ExclusiveLocked means one client holds the exclusive lock.
	ExclusiveLocked

	// SharedLocked means one or more clients hold the shared lock.
	SharedLocked

	// BothLocked means a shared lock is held and someone also holds the
	// exclusive lock. A single client may hold both.
	BothLocked
)

// String returns a short description.
func (s LockState) String() string {
	switch s {
	case Unlocked:
		return "unlocked"
	case ExclusiveLocked:
		return "exclusive locked"
	case SharedLocked:
		return "shared locked"
	case BothLocked:
		return "both locks"
	default:
		return "unknown lock state"
	}
}

// LockManager implements the HiSLIP locking mechanism.
//
// Locking belongs in the server layer, not in the instrument backend. A
// single-session device profile can never see contention, but standards-compliant
// VISA clients still issue lock operations, so the protocol must be answered
// correctly even when the answer is always "granted". See §12.
//
// The zero LockManager is ready for use and is Unlocked, which is the state
// IVI-6.1 requires when the first connection is initialized.
type LockManager struct {
	mu sync.Mutex

	// exclusive is the session holding the exclusive lock.
	exclusive    uint16
	hasExclusive bool

	// key is the shared lock string, meaningful only when shared is non-empty.
	key    string
	shared map[uint16]bool

	// changed is closed and replaced whenever the lock state changes, so that
	// waiting requests wake without polling and without a goroutine each.
	changed chan struct{}
}

// State returns the current lock state.
func (m *LockManager) State() LockState {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.stateLocked()
}

func (m *LockManager) stateLocked() LockState {
	switch {
	case m.hasExclusive && len(m.shared) > 0:
		return BothLocked
	case m.hasExclusive:
		return ExclusiveLocked
	case len(m.shared) > 0:
		return SharedLocked
	default:
		return Unlocked
	}
}

// notifyLocked wakes everything waiting for a state change. The caller holds
// m.mu.
func (m *LockManager) notifyLocked() {
	if m.changed != nil {
		close(m.changed)
		m.changed = nil
	}
}

// waiterLocked returns a channel closed on the next state change. The caller
// holds m.mu.
func (m *LockManager) waiterLocked() chan struct{} {
	if m.changed == nil {
		m.changed = make(chan struct{})
	}
	return m.changed
}

// TryRequest attempts to acquire a lock without waiting.
//
// An empty lockString requests the exclusive lock; a non-empty one requests the
// shared lock identified by that string.
//
// The three outcomes are distinct and must not be collapsed. LockError means the
// request is nonsensical, such as a client asking again for a lock it already
// holds, and waiting would not help. LockFailure means the lock is held
// elsewhere, and waiting might. LockGranted means it was acquired.
func (m *LockManager) TryRequest(sessionID uint16, lockString string) LockResult {
	m.mu.Lock()
	defer m.mu.Unlock()

	if lockString == "" {
		return m.tryExclusiveLocked(sessionID)
	}
	return m.trySharedLocked(sessionID, lockString)
}

// tryExclusiveLocked implements the "Exclusive lock" rows of Table 22.
func (m *LockManager) tryExclusiveLocked(sessionID uint16) LockResult {
	if m.hasExclusive {
		if m.exclusive == sessionID {
			// A client asking again for the lock it already holds. Table 22 gives
			// this as an error rather than a failure, because no amount of
			// waiting would change the answer.
			return LockError
		}
		return LockFailure
	}
	// From Unlocked, or from SharedLocked when the requester holds the shared
	// lock, an exclusive request succeeds. A client not holding the shared lock
	// must wait.
	if len(m.shared) > 0 && !m.shared[sessionID] {
		return LockFailure
	}
	m.hasExclusive = true
	m.exclusive = sessionID
	m.notifyLocked()
	return LockGranted
}

// trySharedLocked implements the "Shared lock" rows of Table 22.
func (m *LockManager) trySharedLocked(sessionID uint16, key string) LockResult {
	if m.shared[sessionID] {
		// Already holding the shared lock.
		return LockError
	}
	if len(m.shared) > 0 {
		if m.key != key {
			// The wrong key. Table 22 gives this as a failure after the timeout
			// rather than an error, because the holders might release.
			return LockFailure
		}
		m.shared[sessionID] = true
		m.notifyLocked()
		return LockGranted
	}
	// No shared lock yet. An exclusive lock held by another client blocks it.
	if m.hasExclusive && m.exclusive != sessionID {
		return LockFailure
	}
	if m.hasExclusive && m.exclusive == sessionID {
		// The holder of the exclusive lock asking for a shared lock is an error
		// in Table 22, in every state where it appears.
		return LockError
	}
	if m.shared == nil {
		m.shared = make(map[uint16]bool)
	}
	m.key = key
	m.shared[sessionID] = true
	m.notifyLocked()
	return LockGranted
}

// Request acquires a lock, waiting up to timeout for it to become available.
//
// A timeout of zero means the lock is granted only if it is available
// immediately, which IVI-6.1 states explicitly. A request that returns LockError
// does not wait, because the answer cannot change.
//
// The context bounds the wait independently of the timeout, so that a session
// teardown does not leave a request blocked.
func (m *LockManager) Request(
	ctx context.Context,
	sessionID uint16,
	lockString string,
	timeout time.Duration,
) LockResult {
	result := m.TryRequest(sessionID, lockString)
	if result != LockFailure || timeout <= 0 {
		return result
	}

	deadline := time.Now().Add(timeout)
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	for {
		m.mu.Lock()
		wait := m.waiterLocked()
		m.mu.Unlock()

		select {
		case <-wait:
			// The state changed; try again.
		case <-timer.C:
			return LockFailure
		case <-ctx.Done():
			return LockFailure
		}

		if result := m.TryRequest(sessionID, lockString); result != LockFailure {
			return result
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return LockFailure
		}
		timer.Reset(remaining)
	}
}

// Release gives up a lock held by the session.
//
// A client holding both locks releases the exclusive one first, which is what
// Table 22 means by "holder of exclusive and shared lock succeeds" moving Both
// locks to Shared locked.
//
// On the MessageID that a release message carries: it designates the last Data,
// DataEnd or Trigger to complete before the lock is released. This implementation
// processes the synchronous channel serially, so any message transmitted before
// the AsyncLock has already completed by the time the release is handled, and no
// additional wait is required. A server with an overlapped pipeline would have to
// wait here.
func (m *LockManager) Release(sessionID uint16) LockResult {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.hasExclusive && m.exclusive == sessionID {
		m.hasExclusive = false
		m.exclusive = 0
		m.notifyLocked()
		return LockGranted
	}
	if m.shared[sessionID] {
		delete(m.shared, sessionID)
		if len(m.shared) == 0 {
			m.key = ""
		}
		m.notifyLocked()
		return LockReleasedShared
	}
	// Releasing a lock that is not held. Table 22 gives this as an error in
	// every state, including Unlocked.
	return LockError
}

// ReleaseAll drops every lock held by a session.
//
// IVI-6.1 requires a server to release all locks assigned to a client when that
// client's connection closes. Without this, a crashed client leaves the
// instrument locked and indistinguishable from one legitimately in use, which is
// the failure that most often forces a power cycle in LAN instrument
// deployments. See R-FW-049.
func (m *LockManager) ReleaseAll(sessionID uint16) {
	m.mu.Lock()
	defer m.mu.Unlock()

	changed := false
	if m.hasExclusive && m.exclusive == sessionID {
		m.hasExclusive = false
		m.exclusive = 0
		changed = true
	}
	if m.shared[sessionID] {
		delete(m.shared, sessionID)
		if len(m.shared) == 0 {
			m.key = ""
		}
		changed = true
	}
	if changed {
		m.notifyLocked()
	}
}

// Info returns the values an AsyncLockInfoResponse carries: whether an exclusive
// lock has been granted, and how many clients hold locks.
//
// A client holding both a shared and an exclusive lock is counted once, which
// IVI-6.1 states explicitly.
func (m *LockManager) Info() (exclusiveGranted bool, clientsHolding int) {
	m.mu.Lock()
	defer m.mu.Unlock()

	holders := make(map[uint16]bool, len(m.shared)+1)
	for id := range m.shared {
		holders[id] = true
	}
	if m.hasExclusive {
		holders[m.exclusive] = true
	}
	return m.hasExclusive, len(holders)
}

// Held reports whether the session holds any lock.
func (m *LockManager) Held(sessionID uint16) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return (m.hasExclusive && m.exclusive == sessionID) || m.shared[sessionID]
}
