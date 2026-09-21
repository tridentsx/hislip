// Copyright (c) 2026 The hislip developers. All rights reserved.
// Project site: https://github.com/gotmc/hislip
// Use of this source code is governed by a MIT-style license that
// can be found in the LICENSE.txt file for the project.

package server

import (
	"context"
	"testing"
	"time"
)

const (
	clientA uint16 = 1
	clientB uint16 = 2
	keyX           = "shared-key-x"
	keyY           = "shared-key-y"
)

// setup names the initial states of IVI-6.1 Table 22 and builds each one.
func setup(state LockState) *LockManager {
	m := &LockManager{}
	switch state {
	case Unlocked:
	case ExclusiveLocked:
		m.TryRequest(clientA, "")
	case SharedLocked:
		m.TryRequest(clientA, keyX)
	case BothLocked:
		m.TryRequest(clientA, keyX)
		m.TryRequest(clientA, "")
	}
	return m
}

// TestLockBehaviourTable is transcribed from IVI-6.1 Table 22. Every row is a
// case, and the distinction between "error" and "fails after lock timeout" is
// preserved: the first means waiting would not help, the second that it might.
func TestLockBehaviourTable(t *testing.T) {
	tests := []struct {
		initial   LockState
		request   string // "" exclusive, otherwise the shared key
		release   bool
		client    uint16
		want      LockResult
		wantState LockState
		note      string
	}{
		// Unlocked.
		{Unlocked, keyX, false, clientA, LockGranted, SharedLocked, "shared lock, any client"},
		{Unlocked, "", false, clientA, LockGranted, ExclusiveLocked, "exclusive lock, any client"},
		{Unlocked, "", true, clientA, LockError, Unlocked, "release with nothing held"},

		// Exclusive locked, held by clientA.
		{ExclusiveLocked, keyX, false, clientA, LockError, ExclusiveLocked, "shared lock by the exclusive holder"},
		{ExclusiveLocked, keyX, false, clientB, LockFailure, ExclusiveLocked, "shared lock by another client"},
		{ExclusiveLocked, "", false, clientB, LockFailure, ExclusiveLocked, "exclusive lock by a non-holder"},
		{ExclusiveLocked, "", false, clientA, LockError, ExclusiveLocked, "exclusive lock by the holder"},
		{ExclusiveLocked, "", true, clientA, LockGranted, Unlocked, "release by the exclusive holder"},
		{ExclusiveLocked, "", true, clientB, LockError, ExclusiveLocked, "release by a client without the lock"},

		// Shared locked, held by clientA with keyX.
		{SharedLocked, keyX, false, clientA, LockError, SharedLocked, "shared lock by a shared holder"},
		{SharedLocked, keyX, false, clientB, LockGranted, SharedLocked, "shared lock with the right key"},
		{SharedLocked, keyY, false, clientB, LockFailure, SharedLocked, "shared lock with the wrong key"},
		{SharedLocked, "", false, clientA, LockGranted, BothLocked, "exclusive lock by the shared holder"},
		{SharedLocked, "", false, clientB, LockFailure, SharedLocked, "exclusive lock by a non-holder"},
		{SharedLocked, "", true, clientA, LockReleasedShared, Unlocked, "release by the only shared holder"},
		{SharedLocked, "", true, clientB, LockError, SharedLocked, "release by a client holding nothing"},

		// Both locks, clientA holds shared and exclusive.
		{BothLocked, keyX, false, clientA, LockError, BothLocked, "shared lock by a shared holder"},
		{BothLocked, keyX, false, clientB, LockGranted, BothLocked, "shared lock with the right key"},
		{BothLocked, keyY, false, clientB, LockFailure, BothLocked, "shared lock with the wrong key"},
		{BothLocked, "", false, clientB, LockFailure, BothLocked, "exclusive lock by a non-holder"},
		{BothLocked, "", false, clientA, LockError, BothLocked, "exclusive lock by the exclusive holder"},
		{BothLocked, "", true, clientA, LockGranted, SharedLocked, "release by the holder of both"},
	}

	for _, tc := range tests {
		name := tc.initial.String() + ": " + tc.note
		t.Run(name, func(t *testing.T) {
			m := setup(tc.initial)
			if got := m.State(); got != tc.initial {
				t.Fatalf("setup produced %v, want %v", got, tc.initial)
			}

			var got LockResult
			if tc.release {
				got = m.Release(tc.client)
			} else {
				got = m.TryRequest(tc.client, tc.request)
			}
			if got != tc.want {
				t.Errorf("result = %v, want %v", got, tc.want)
			}
			if state := m.State(); state != tc.wantState {
				t.Errorf("new state = %v, want %v", state, tc.wantState)
			}
		})
	}
}

// TestSharedReleaseWithTwoHolders covers the row Table 22 distinguishes by holder
// count: releasing while others still hold leaves the shared lock in place.
func TestSharedReleaseWithTwoHolders(t *testing.T) {
	m := &LockManager{}
	if got := m.TryRequest(clientA, keyX); got != LockGranted {
		t.Fatalf("clientA request = %v", got)
	}
	if got := m.TryRequest(clientB, keyX); got != LockGranted {
		t.Fatalf("clientB request = %v", got)
	}

	if got := m.Release(clientA); got != LockReleasedShared {
		t.Errorf("release by one of two holders = %v, want LockReleasedShared", got)
	}
	if got := m.State(); got != SharedLocked {
		t.Errorf("state = %v, want SharedLocked while clientB still holds it", got)
	}
	if got := m.Release(clientB); got != LockReleasedShared {
		t.Errorf("release by the last holder = %v", got)
	}
	if got := m.State(); got != Unlocked {
		t.Errorf("state = %v, want Unlocked", got)
	}
	// The key is forgotten with the last holder, so a different key now succeeds.
	if got := m.TryRequest(clientA, keyY); got != LockGranted {
		t.Errorf("request with a new key after full release = %v", got)
	}
}

// TestZeroTimeoutDoesNotWait covers the explicit rule that a timeout of zero
// grants the lock only if it is available immediately.
func TestZeroTimeoutDoesNotWait(t *testing.T) {
	m := &LockManager{}
	m.TryRequest(clientA, "")

	start := time.Now()
	got := m.Request(context.Background(), clientB, "", 0)
	elapsed := time.Since(start)

	if got != LockFailure {
		t.Errorf("result = %v, want LockFailure", got)
	}
	if elapsed > 50*time.Millisecond {
		t.Errorf("a zero timeout waited %v", elapsed)
	}
}

// TestRequestWaitsAndSucceeds confirms a waiting request is woken by a release
// rather than polling for it.
func TestRequestWaitsAndSucceeds(t *testing.T) {
	m := &LockManager{}
	m.TryRequest(clientA, "")

	go func() {
		time.Sleep(20 * time.Millisecond)
		m.Release(clientA)
	}()

	start := time.Now()
	got := m.Request(context.Background(), clientB, "", 2*time.Second)
	elapsed := time.Since(start)

	if got != LockGranted {
		t.Fatalf("result = %v, want LockGranted", got)
	}
	if elapsed > time.Second {
		t.Errorf("the waiter took %v to notice the release", elapsed)
	}
	if m.State() != ExclusiveLocked {
		t.Errorf("state = %v, want ExclusiveLocked held by clientB", m.State())
	}
}

func TestRequestTimesOut(t *testing.T) {
	m := &LockManager{}
	m.TryRequest(clientA, "")

	start := time.Now()
	got := m.Request(context.Background(), clientB, "", 40*time.Millisecond)
	elapsed := time.Since(start)

	if got != LockFailure {
		t.Errorf("result = %v, want LockFailure", got)
	}
	if elapsed < 30*time.Millisecond {
		t.Errorf("returned after %v, before the timeout expired", elapsed)
	}
}

// TestRequestErrorDoesNotWait confirms that a nonsensical request fails at once.
// Waiting would be pointless, since the answer cannot change.
func TestRequestErrorDoesNotWait(t *testing.T) {
	m := &LockManager{}
	m.TryRequest(clientA, "")

	start := time.Now()
	got := m.Request(context.Background(), clientA, "", 5*time.Second)
	elapsed := time.Since(start)

	if got != LockError {
		t.Errorf("result = %v, want LockError", got)
	}
	if elapsed > 100*time.Millisecond {
		t.Errorf("an error result waited %v", elapsed)
	}
}

func TestRequestRespectsContext(t *testing.T) {
	m := &LockManager{}
	m.TryRequest(clientA, "")

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	got := m.Request(ctx, clientB, "", time.Minute)
	if got != LockFailure {
		t.Errorf("result = %v, want LockFailure", got)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("the cancelled request waited %v", elapsed)
	}
}

// TestReleaseAllOnDisconnect covers the requirement that a closing client's locks
// are released. A crashed client that kept its lock would leave the instrument
// indistinguishable from one legitimately in use.
func TestReleaseAllOnDisconnect(t *testing.T) {
	m := &LockManager{}
	m.TryRequest(clientA, keyX)
	m.TryRequest(clientA, "")
	if m.State() != BothLocked {
		t.Fatalf("state = %v, want BothLocked", m.State())
	}

	m.ReleaseAll(clientA)

	if got := m.State(); got != Unlocked {
		t.Errorf("state = %v, want Unlocked after the holder disconnected", got)
	}
	if m.Held(clientA) {
		t.Error("Held() = true after ReleaseAll")
	}
	// Releasing again is harmless.
	m.ReleaseAll(clientA)
}

func TestReleaseAllWakesWaiters(t *testing.T) {
	m := &LockManager{}
	m.TryRequest(clientA, "")

	go func() {
		time.Sleep(20 * time.Millisecond)
		m.ReleaseAll(clientA)
	}()

	if got := m.Request(context.Background(), clientB, "", 2*time.Second); got != LockGranted {
		t.Errorf("result = %v, want LockGranted after the holder disconnected", got)
	}
}

// TestLockInfoCountsClientsOnce covers the explicit rule that a client holding
// both a shared and an exclusive lock is counted once.
func TestLockInfoCountsClientsOnce(t *testing.T) {
	m := &LockManager{}

	if exclusive, holders := m.Info(); exclusive || holders != 0 {
		t.Errorf("Info() on an unlocked server = %v, %d", exclusive, holders)
	}

	m.TryRequest(clientA, keyX)
	if exclusive, holders := m.Info(); exclusive || holders != 1 {
		t.Errorf("Info() with one shared holder = %v, %d; want false, 1", exclusive, holders)
	}

	m.TryRequest(clientA, "")
	exclusive, holders := m.Info()
	if !exclusive {
		t.Error("Info() reports no exclusive lock when one is held")
	}
	if holders != 1 {
		t.Errorf("holders = %d, want 1; a client holding both counts once", holders)
	}

	m.TryRequest(clientB, keyX)
	if _, holders := m.Info(); holders != 2 {
		t.Errorf("holders = %d, want 2", holders)
	}
}

func TestLockResultAndStateStrings(t *testing.T) {
	for _, r := range []LockResult{LockFailure, LockGranted, LockReleasedShared, LockError} {
		if r.String() == "" {
			t.Errorf("LockResult(%d).String() is empty", r)
		}
	}
	if got := LockResult(9).String(); got != "unknown lock result" {
		t.Errorf("LockResult(9).String() = %q", got)
	}
	for s := Unlocked; s <= BothLocked; s++ {
		if s.String() == "" {
			t.Errorf("LockState(%d).String() is empty", s)
		}
	}
	if got := LockState(9).String(); got != "unknown lock state" {
		t.Errorf("LockState(9).String() = %q", got)
	}
}

// TestZeroLockManagerIsUnlocked covers the requirement that the server is in the
// Unlocked state when the first connection is initialized.
func TestZeroLockManagerIsUnlocked(t *testing.T) {
	var m LockManager
	if got := m.State(); got != Unlocked {
		t.Errorf("the zero LockManager is %v, want Unlocked", got)
	}
	if m.Held(clientA) {
		t.Error("Held() = true on a fresh manager")
	}
}

func TestConcurrentLockTraffic(t *testing.T) {
	m := &LockManager{}
	const clients = 8
	done := make(chan struct{})

	for i := 0; i < clients; i++ {
		go func(id uint16) {
			defer func() { done <- struct{}{} }()
			for j := 0; j < 50; j++ {
				if m.TryRequest(id, "") == LockGranted {
					m.Release(id)
				}
				m.Info()
				m.ReleaseAll(id)
			}
		}(uint16(i + 1))
	}
	for i := 0; i < clients; i++ {
		<-done
	}
	if got := m.State(); got != Unlocked {
		t.Errorf("state after concurrent traffic = %v, want Unlocked", got)
	}
}
