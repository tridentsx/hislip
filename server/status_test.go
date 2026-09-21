// Copyright (c) 2026 The hislip developers. All rights reserved.
// Project site: https://github.com/gotmc/hislip
// Use of this source code is governed by a MIT-style license that
// can be found in the LICENSE.txt file for the project.

package server

import (
	"testing"

	"github.com/gotmc/hislip/protocol"
)

func TestStatusMAVBitPosition(t *testing.T) {
	// MAV is bit position 4 and RQS is bit position 6.
	if StatusMAV != 0x10 {
		t.Errorf("StatusMAV = %#x, want 0x10", StatusMAV)
	}
	if StatusRQS != 0x40 {
		t.Errorf("StatusRQS = %#x, want 0x40", StatusRQS)
	}
}

// TestStatusMAVRules covers the three rules of §11.4.1 in precedence order.
func TestStatusMAVRules(t *testing.T) {
	const active = protocol.MessageID(0xffffff04)

	tests := []struct {
		name         string
		lastReceived protocol.MessageID
		pending      bool
		queryID      protocol.MessageID
		rmtDelivered bool
		want         bool
	}{
		{
			name:         "data held and IDs agree",
			lastReceived: active,
			pending:      true,
			queryID:      active,
			want:         true,
		},
		{
			name:         "no data held and IDs agree",
			lastReceived: active,
			pending:      false,
			queryID:      active,
			want:         false,
		},
		{
			// R-SYNC-030. This is the rule Revision 2 of the design
			// specification missed entirely: buffer occupancy does not decide.
			name:         "data held but the query names an older message",
			lastReceived: active,
			pending:      true,
			queryID:      active.Previous(),
			want:         false,
		},
		{
			name:         "data held but the query names a newer message",
			lastReceived: active,
			pending:      true,
			queryID:      active.Next(),
			want:         false,
		},
		{
			// R-SYNC-031.
			name:         "data held, IDs agree, but RMT-delivered is set",
			lastReceived: active,
			pending:      true,
			queryID:      active,
			rmtDelivered: true,
			want:         false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := NewStatus()
			s.ReceivedSyncMessage(tc.lastReceived)
			s.SetResponsePending(tc.pending)
			got := s.MAV(tc.queryID, tc.rmtDelivered)
			if got != tc.want {
				t.Errorf("MAV() = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestStatusAtSessionStart confirms that a status query issued before any
// synchronous message needs no special case. A conforming client sends
// 0xfffffefe, and Reset positions lastReceived at exactly that value.
func TestStatusAtSessionStart(t *testing.T) {
	s := NewStatus()
	if got := s.LastReceived(); got != protocol.StatusQueryInitialMessageID {
		t.Errorf(
			"LastReceived() at session start = %#x, want %#x",
			uint32(got), uint32(protocol.StatusQueryInitialMessageID),
		)
	}
	// The IDs agree, so MAV reduces to the output state, which is empty.
	if s.MAV(protocol.StatusQueryInitialMessageID, false) {
		t.Error("MAV() = true at session start with no data held")
	}
	// A client that wrongly sends the initial MessageID instead gets MAV false
	// from the inequality rule rather than an error.
	if s.MAV(protocol.InitialMessageID, false) {
		t.Error("MAV() = true for a mismatched session-start query")
	}
}

func TestZeroStatusSelfInitialises(t *testing.T) {
	// The zero value has lastReceived 0, which is a legal MessageID and would
	// silently produce wrong comparisons. Reset must happen implicitly.
	var s Status
	if got := s.LastReceived(); got != protocol.StatusQueryInitialMessageID {
		t.Errorf(
			"zero Status LastReceived() = %#x, want %#x",
			uint32(got), uint32(protocol.StatusQueryInitialMessageID),
		)
	}
}

// TestStatusBytePreservesOtherBits checks that merging MAV leaves every other
// bit alone, RQS above all. A bridge must not clear bits it does not own.
func TestStatusBytePreservesOtherBits(t *testing.T) {
	const active = protocol.MessageID(0xffffff06)
	s := NewStatus()
	s.ReceivedSyncMessage(active)

	// Device reports RQS and ESB set, MAV clear. The server holds data, so MAV
	// must come back set with everything else intact.
	const device = StatusRQS | 0x20
	s.SetResponsePending(true)
	got := s.Byte(device, active, false)
	if got&StatusMAV == 0 {
		t.Error("MAV not set when data is held")
	}
	if got&StatusRQS == 0 {
		t.Error("RQS was cleared; the server must never clear RQS")
	}
	if got&0x20 == 0 {
		t.Error("ESB was cleared")
	}

	// Now with no data held, MAV must be cleared even though the device set it.
	s.SetResponsePending(false)
	got = s.Byte(device|StatusMAV, active, false)
	if got&StatusMAV != 0 {
		t.Error("MAV set when no data is held; the device's bit must not pass through")
	}
	if got&StatusRQS == 0 {
		t.Error("RQS was cleared")
	}
}

func TestStatusReset(t *testing.T) {
	s := NewStatus()
	s.ReceivedSyncMessage(protocol.MessageID(0xffffff20))
	s.SetResponsePending(true)

	s.Reset()

	if got := s.LastReceived(); got != protocol.StatusQueryInitialMessageID {
		t.Errorf("LastReceived() after Reset = %#x", uint32(got))
	}
	if s.ResponsePending() {
		t.Error("ResponsePending() = true after Reset; a device clear discards held data")
	}
}

// TestStatusTrackingFollowsTheSequence walks a short exchange to confirm that
// MAV follows the active transaction rather than lagging behind it.
func TestStatusTrackingFollowsTheSequence(t *testing.T) {
	s := NewStatus()
	c := protocol.NewCounter()

	first := c.Take()
	s.ReceivedSyncMessage(first)
	s.SetResponsePending(true)
	if !s.MAV(first, false) {
		t.Fatal("MAV false for the active transaction with data held")
	}

	// The client sends the next message. The response to the previous one is no
	// longer attributable, so a status query naming the old ID reports false.
	second := c.Take()
	s.ReceivedSyncMessage(second)
	if s.MAV(first, false) {
		t.Error("MAV true for a superseded transaction")
	}
	if !s.MAV(second, false) {
		t.Error("MAV false for the new active transaction while data is still held")
	}
}
