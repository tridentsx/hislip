// Copyright (c) 2026 The hislip developers. All rights reserved.
// Project site: https://github.com/tridentsx/hislip
// Use of this source code is governed by a MIT-style license that
// can be found in the LICENSE.txt file for the project.

package server

import (
	"errors"
	"testing"

	"github.com/tridentsx/hislip/protocol"
)

// TestTriggerIsOrderedWithData is the regression test for the defect this
// classification exists to fix. Revision 2 of the design specification ranked
// Trigger above data by urgency, which would let it overtake a queued write and
// corrupt the MessageID and RMT sequence.
func TestTriggerIsOrderedWithData(t *testing.T) {
	for _, typ := range []protocol.MessageType{
		protocol.Data, protocol.DataEnd, protocol.Trigger,
	} {
		class := ClassOf(typ)
		if class != ClassSyncOrdered {
			t.Errorf("ClassOf(%v) = %v, want ClassSyncOrdered", typ, class)
		}
		if !class.Ordered() {
			t.Errorf("%v is not ordered", typ)
		}
		if class.Preempts() {
			t.Errorf("%v may preempt, want not", typ)
		}
	}
}

// TestClassFollowsChannel is the invariant behind the whole design: ordering is a
// property of the channel. Every class that may be reordered or preempt must be
// an asynchronous-channel message, and every strictly ordered operation must be a
// synchronous-channel message.
func TestClassFollowsChannel(t *testing.T) {
	for i := 0; i < 256; i++ {
		typ := protocol.MessageType(i)
		if !typ.Defined() {
			continue
		}
		class := ClassOf(typ)
		switch class {
		case ClassNoBus, ClassPreemptAbandon, ClassPreemptResume, ClassBoundary:
			if typ.Channel() != protocol.ChannelAsynchronous {
				t.Errorf(
					"%v is class %v but its channel is %d; only asynchronous messages may be reordered",
					typ, class, typ.Channel(),
				)
			}
		case ClassSyncOrdered:
			if typ.Channel() != protocol.ChannelSynchronous {
				t.Errorf(
					"%v is class %v but its channel is %d; ordered operations are synchronous",
					typ, class, typ.Channel(),
				)
			}
		}
	}
}

func TestClassOfKnownMessages(t *testing.T) {
	tests := []struct {
		typ  protocol.MessageType
		want Class
	}{
		{protocol.AsyncLock, ClassNoBus},
		{protocol.AsyncLockInfo, ClassNoBus},
		{protocol.AsyncMaximumMessageSize, ClassNoBus},
		{protocol.AsyncDeviceClear, ClassPreemptAbandon},
		{protocol.AsyncStatusQuery, ClassPreemptResume},
		{protocol.AsyncRemoteLocalControl, ClassBoundary},
		{protocol.Data, ClassSyncOrdered},
		{protocol.DataEnd, ClassSyncOrdered},
		{protocol.Trigger, ClassSyncOrdered},
		{protocol.Initialize, ClassLifecycle},
		{protocol.AsyncInitialize, ClassLifecycle},
		{protocol.DeviceClearComplete, ClassLifecycle},
		{protocol.Error, ClassLifecycle},
		{protocol.FatalError, ClassLifecycle},
		{protocol.VendorSpecificMin, ClassSyncOrdered},
		{protocol.AsyncStatusResponse, ClassUndefined},
		{protocol.InitializeResponse, ClassUndefined},
		{50, ClassUndefined},
	}
	for _, tc := range tests {
		if got := ClassOf(tc.typ); got != tc.want {
			t.Errorf("ClassOf(%v) = %v, want %v", tc.typ, got, tc.want)
		}
	}
}

// TestClassZeroIsNeverQueued is the other half of the §47 fix. A lock request or
// a maximum message size negotiation needs no bus access, so queueing it behind
// instrument I/O delays it for no reason. IVI-6.1 requires these to complete even
// while a lock blocks the synchronous channel.
func TestClassZeroIsNeverQueued(t *testing.T) {
	for _, typ := range []protocol.MessageType{
		protocol.AsyncLock, protocol.AsyncLockInfo, protocol.AsyncMaximumMessageSize,
	} {
		class := ClassOf(typ)
		if class.Queued() {
			t.Errorf("%v is queued; class 0 operations must be answered directly", typ)
		}
		if class.Preempts() {
			t.Errorf("%v preempts; it needs no bus access so there is nothing to preempt", typ)
		}
	}
}

func TestPreemptingClasses(t *testing.T) {
	if !ClassOf(protocol.AsyncDeviceClear).Preempts() {
		t.Error("device clear must preempt; it is the operation an operator uses when the instrument is stuck")
	}
	if !ClassOf(protocol.AsyncStatusQuery).Preempts() {
		t.Error("status query must preempt; viReadSTB must be prompt while a transfer is stalled")
	}
	if ClassOf(protocol.AsyncRemoteLocalControl).Preempts() {
		t.Error("remote/local must not interrupt a byte transfer")
	}
	if !ClassOf(protocol.AsyncRemoteLocalControl).Queued() {
		t.Error("remote/local needs bus access and must be queued")
	}
}

func TestClassString(t *testing.T) {
	for c := ClassNoBus; c <= ClassUndefined; c++ {
		if c.String() == "" {
			t.Errorf("Class(%d).String() is empty", c)
		}
	}
	if got := Class(99).String(); got != "undefined" {
		t.Errorf("Class(99).String() = %q", got)
	}
}

func TestAcceptRejectsReservedAndUnknown(t *testing.T) {
	srv := newTestServer(t)
	for _, typ := range []protocol.MessageType{39, 60, 127} {
		h := protocol.Header{Type: typ}
		_, err := srv.Accept(nil, h, protocol.ChannelSynchronous)
		if !errors.Is(err, protocol.ErrReservedMessageType) {
			t.Errorf("Accept(type %d) error = %v, want ErrReservedMessageType", typ, err)
		}
	}
}

func TestAcceptRejectsWrongChannel(t *testing.T) {
	srv := newTestServer(t)
	// Data is synchronous only.
	_, err := srv.Accept(nil, protocol.Header{Type: protocol.Data}, protocol.ChannelAsynchronous)
	if !errors.Is(err, protocol.ErrWrongChannel) {
		t.Errorf("Data on the async channel: error = %v, want ErrWrongChannel", err)
	}
	// AsyncStatusQuery is asynchronous only.
	_, err = srv.Accept(nil, protocol.Header{Type: protocol.AsyncStatusQuery}, protocol.ChannelSynchronous)
	if !errors.Is(err, protocol.ErrWrongChannel) {
		t.Errorf("AsyncStatusQuery on the sync channel: error = %v, want ErrWrongChannel", err)
	}
}

func TestAcceptRejectsOversizedPayload(t *testing.T) {
	srv := newTestServer(t)
	h := protocol.Header{Type: protocol.Data, Length: srv.Config().MaxRxPayload + 1}
	_, err := srv.Accept(nil, h, protocol.ChannelSynchronous)
	if !errors.Is(err, protocol.ErrMessageTooLarge) {
		t.Errorf("oversized payload: error = %v, want ErrMessageTooLarge", err)
	}
	// A declared length of 2^64-1 must be refused by the length check, never by
	// attempting to allocate.
	h.Length = ^uint64(0)
	if _, err := srv.Accept(nil, h, protocol.ChannelSynchronous); !errors.Is(err, protocol.ErrMessageTooLarge) {
		t.Errorf("maximal payload: error = %v, want ErrMessageTooLarge", err)
	}
}

func TestAcceptRejectsVersionTooHigh(t *testing.T) {
	srv := newTestServer(t)
	sess := newSession(1, protocol.Version10, nil, srv.Config())
	// GetDescriptors requires 2.0; the session negotiated 1.0.
	h := protocol.Header{Type: protocol.GetDescriptors}
	_, err := srv.Accept(sess, h, protocol.ChannelSynchronous)
	if !errors.Is(err, protocol.ErrProtocolVersion) {
		t.Errorf("2.0 message on a 1.0 session: error = %v, want ErrProtocolVersion", err)
	}
}

// TestAcceptRequiresBothChannels covers fatal error 2, "attempt to use
// connection without both channels established".
func TestAcceptRequiresBothChannels(t *testing.T) {
	srv := newTestServer(t)
	sess := newSession(1, protocol.Version10, nil, srv.Config())
	if sess.State() != StatePairing {
		t.Fatalf("new session state = %v, want StatePairing", sess.State())
	}

	_, err := srv.Accept(sess, protocol.Header{Type: protocol.Data}, protocol.ChannelSynchronous)
	if !errors.Is(err, ErrSessionNotReady) {
		t.Errorf("Data before pairing: error = %v, want ErrSessionNotReady", err)
	}
	if got := WireErrorFor(err); !got.Fatal || got.FatalCode != protocol.FatalChannelsNotEstablished {
		t.Errorf("WireErrorFor() = %+v, want fatal code 2", got)
	}

	// Lifecycle messages are exempt, because they are how the session reaches
	// the ready state.
	if _, err := srv.Accept(sess, protocol.Header{Type: protocol.Initialize}, protocol.ChannelSynchronous); err != nil {
		t.Errorf("Initialize before pairing: error = %v, want nil", err)
	}
}

func TestAcceptAllowsNormalOperationWhenReady(t *testing.T) {
	srv := newTestServer(t)
	sess := newSession(1, protocol.Version10, nil, srv.Config())
	if err := sess.attachAsync(nil); err != nil {
		t.Fatalf("attachAsync() error = %v", err)
	}
	class, err := srv.Accept(sess, protocol.Header{Type: protocol.DataEnd}, protocol.ChannelSynchronous)
	if err != nil {
		t.Fatalf("Accept() error = %v", err)
	}
	if class != ClassSyncOrdered {
		t.Errorf("class = %v, want ClassSyncOrdered", class)
	}
}
