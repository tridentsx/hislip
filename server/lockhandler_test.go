// Copyright (c) 2026 The hislip developers. All rights reserved.
// Project site: https://github.com/gotmc/hislip
// Use of this source code is governed by a MIT-style license that
// can be found in the LICENSE.txt file for the project.

package server_test

import (
	"context"
	"errors"
	"testing"

	"github.com/gotmc/hislip/protocol"
	"github.com/gotmc/hislip/server"
)

func lockRequestHeader(timeoutMS uint32) protocol.Header {
	return protocol.Header{
		Type:      protocol.AsyncLock,
		Control:   1, // request
		Parameter: timeoutMS,
	}
}

func lockReleaseHeader(id protocol.MessageID) protocol.Header {
	return protocol.Header{
		Type:      protocol.AsyncLock,
		Control:   0, // release
		Parameter: uint32(id),
	}
}

func TestLockRequestAndRelease(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)

	// An exclusive lock is a zero-length lock string.
	resp, err := f.srv.Lock(ctx, f.sess, lockRequestHeader(0), "")
	if err != nil {
		t.Fatalf("Lock() error = %v", err)
	}
	if resp.Type != protocol.AsyncLockResponse {
		t.Errorf("type = %v, want AsyncLockResponse", resp.Type)
	}
	if server.LockResult(resp.Control) != server.LockGranted {
		t.Errorf("control = %d, want LockGranted", resp.Control)
	}
	if f.srv.Locks().State() != server.ExclusiveLocked {
		t.Errorf("state = %v, want ExclusiveLocked", f.srv.Locks().State())
	}

	// Releasing an exclusive lock answers with code 1.
	resp, err = f.srv.Lock(ctx, f.sess, lockReleaseHeader(protocol.InitialMessageID), "")
	if err != nil {
		t.Fatalf("Lock() release error = %v", err)
	}
	if server.LockResult(resp.Control) != server.LockGranted {
		t.Errorf("release control = %d, want 1 for an exclusive release", resp.Control)
	}

	// A shared release answers with code 2, which is why the two are separate
	// constants despite a request only ever using code 1 for success.
	if _, err := f.srv.Lock(ctx, f.sess, lockRequestHeader(0), "key"); err != nil {
		t.Fatalf("shared Lock() error = %v", err)
	}
	resp, err = f.srv.Lock(ctx, f.sess, lockReleaseHeader(protocol.InitialMessageID), "")
	if err != nil {
		t.Fatalf("shared release error = %v", err)
	}
	if server.LockResult(resp.Control) != server.LockReleasedShared {
		t.Errorf("release control = %d, want 2 for a shared release", resp.Control)
	}
}

func TestLockRejectsUnknownControlCode(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)

	for _, control := range []uint8{2, 3, 255} {
		h := protocol.Header{Type: protocol.AsyncLock, Control: control}
		_, err := f.srv.Lock(ctx, f.sess, h, "")
		if !errors.Is(err, server.ErrUnrecognizedControlCode) {
			t.Errorf("control %d: error = %v, want ErrUnrecognizedControlCode", control, err)
		}
		if wire := server.WireErrorFor(err); wire.Fatal {
			t.Errorf("control %d was treated as fatal", control)
		}
	}
}

// TestLockTimeoutFromParameter confirms the request timeout is read from the
// message parameter, the field the standard otherwise calls MessageID.
func TestLockTimeoutFromParameter(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)

	// Another session takes the lock first.
	other := f.srv.Locks()
	if got := other.TryRequest(9999, ""); got != server.LockGranted {
		t.Fatalf("setup request = %v", got)
	}

	// A zero timeout must fail immediately rather than blocking the handler.
	resp, err := f.srv.Lock(ctx, f.sess, lockRequestHeader(0), "")
	if err != nil {
		t.Fatalf("Lock() error = %v", err)
	}
	if server.LockResult(resp.Control) != server.LockFailure {
		t.Errorf("control = %d, want LockFailure", resp.Control)
	}
}

func TestLockInfoReportsGrants(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)

	resp, err := f.srv.LockInfo(f.sess, protocol.Header{Type: protocol.AsyncLockInfo})
	if err != nil {
		t.Fatalf("LockInfo() error = %v", err)
	}
	if resp.Type != protocol.AsyncLockInfoResponse {
		t.Errorf("type = %v, want AsyncLockInfoResponse", resp.Type)
	}
	if resp.Control != 0 {
		t.Errorf("exclusive-locks-granted = %d, want 0", resp.Control)
	}
	if resp.Parameter != 0 {
		t.Errorf("locks-granted = %d, want 0", resp.Parameter)
	}

	if _, err := f.srv.Lock(ctx, f.sess, lockRequestHeader(0), ""); err != nil {
		t.Fatalf("Lock() error = %v", err)
	}
	resp, err = f.srv.LockInfo(f.sess, protocol.Header{Type: protocol.AsyncLockInfo})
	if err != nil {
		t.Fatalf("LockInfo() error = %v", err)
	}
	if resp.Control != 1 {
		t.Errorf("exclusive-locks-granted = %d, want 1", resp.Control)
	}
	if resp.Parameter != 1 {
		t.Errorf("locks-granted = %d, want 1", resp.Parameter)
	}
}

// TestLockInfoWorksWithoutHoldingALock covers the explicit rule that the
// transaction is processed whether or not the client holds a lock.
func TestLockInfoWorksWithoutHoldingALock(t *testing.T) {
	f := newFixture(t)
	f.srv.Locks().TryRequest(9999, "") // someone else holds it

	resp, err := f.srv.LockInfo(f.sess, protocol.Header{Type: protocol.AsyncLockInfo})
	if err != nil {
		t.Fatalf("LockInfo() error = %v", err)
	}
	if resp.Control != 1 {
		t.Errorf("exclusive-locks-granted = %d, want 1", resp.Control)
	}
}

// TestClosingSessionReleasesLocks is the end-to-end form of the requirement. A
// lock held by a dead session is indistinguishable from one held by a working
// one, and is the failure that most often forces a power cycle.
func TestClosingSessionReleasesLocks(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)

	if _, err := f.srv.Lock(ctx, f.sess, lockRequestHeader(0), ""); err != nil {
		t.Fatalf("Lock() error = %v", err)
	}
	if f.srv.Locks().State() != server.ExclusiveLocked {
		t.Fatalf("state = %v, want ExclusiveLocked", f.srv.Locks().State())
	}

	if err := f.srv.CloseAll(); err != nil {
		t.Fatalf("CloseAll() error = %v", err)
	}
	if got := f.srv.Locks().State(); got != server.Unlocked {
		t.Errorf("state = %v, want Unlocked after the session closed", got)
	}
}

func TestLockHandlersRejectWrongMessageType(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)

	if _, err := f.srv.Lock(ctx, f.sess, protocol.Header{Type: protocol.Data}, ""); !errors.Is(err, protocol.ErrUnknownMessageType) {
		t.Errorf("Lock() with the wrong type = %v", err)
	}
	if _, err := f.srv.LockInfo(f.sess, protocol.Header{Type: protocol.Data}); !errors.Is(err, protocol.ErrUnknownMessageType) {
		t.Errorf("LockInfo() with the wrong type = %v", err)
	}
}

// TestLockOperationsAreClassZero records why lock handling takes no bus access
// path: IVI-6.1 requires lock transactions to complete even while a lock blocks
// the synchronous channel, so they must never be queued behind instrument I/O.
func TestLockOperationsAreClassZero(t *testing.T) {
	for _, typ := range []protocol.MessageType{protocol.AsyncLock, protocol.AsyncLockInfo} {
		class := server.ClassOf(typ)
		if class != server.ClassNoBus {
			t.Errorf("ClassOf(%v) = %v, want ClassNoBus", typ, class)
		}
		if class.Queued() {
			t.Errorf("%v is queued behind bus operations", typ)
		}
	}
}
