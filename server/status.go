// Copyright (c) 2026 The hislip developers. All rights reserved.
// Project site: https://github.com/tridentsx/hislip
// Use of this source code is governed by a MIT-style license that
// can be found in the LICENSE.txt file for the project.

package server

import "github.com/tridentsx/hislip/protocol"

// StatusMAV is the message-available bit of the status byte, bit position 4.
const StatusMAV byte = 1 << 4

// StatusRQS is the request-service bit of the status byte, bit position 6. The
// server does not clear it; the client clears RQS by performing
// AsyncStatusQuery. See IVI-6.1 section 6.13 and R-SRV-022.
const StatusRQS byte = 1 << 6

// Status computes the status byte a Synchronized Mode server reports.
//
// MAV is the subtle part of bridging, and Revision 2 of the design
// specification got it wrong by describing it purely as a function of whether
// the bridge holds response bytes. In Synchronized Mode it is primarily a
// function of MessageID equality. IVI-6.1 section 6.14.1:
//
//	"In synchronized mode, the server should consult the MessageID provided
//	with the AsyncStatusQuery. If this MessageID is not equal to the MessageID
//	of the last received Data, DataEnd, or Trigger message then MAV shall be
//	set false."
//
// The zero Status is not ready for use, because the initial value of
// lastReceived is not zero. Use NewStatus, or call Reset.
type Status struct {
	// lastReceived is the MessageID of the most recent Data, DataEnd or Trigger.
	lastReceived protocol.MessageID

	// responsePending records whether the server holds response data for the
	// active transaction at the HiSLIP boundary. For a bridge this is not the
	// same as the instrument asserting MAV, because bytes may already have been
	// read out of the instrument into an adapter buffer. See §16.3.
	responsePending bool

	// srqLatched and srqStatus hold the bridge-side RQS latch. See
	// LatchServiceRequest.
	srqLatched bool
	srqStatus  byte

	started bool
}

// NewStatus returns a Status positioned for the start of a session.
func NewStatus() *Status {
	s := &Status{}
	s.Reset()
	return s
}

// Reset returns the tracker to its session-start state. Called on
// initialization and after Device Clear completes.
//
// lastReceived is set to the value a conforming client sends in an
// AsyncStatusQuery issued before it has sent any Data, DataEnd or Trigger,
// which IVI-6.1 section 6.14 gives as 0xffffff00-2. Initialising it this way
// means the MessageID comparison below needs no special case for the start of a
// session.
func (s *Status) Reset() {
	s.lastReceived = protocol.StatusQueryInitialMessageID
	s.responsePending = false
	s.srqLatched = false
	s.srqStatus = 0
	s.started = true
}

// ReceivedSyncMessage records the MessageID of an arriving Data, DataEnd or
// Trigger message.
func (s *Status) ReceivedSyncMessage(id protocol.MessageID) {
	if !s.started {
		s.Reset()
	}
	s.lastReceived = id
}

// LastReceived returns the MessageID of the most recent Data, DataEnd or
// Trigger, or the session-start value if none has arrived.
func (s *Status) LastReceived() protocol.MessageID {
	if !s.started {
		s.Reset()
	}
	return s.lastReceived
}

// SetResponsePending records whether the server holds response data for the
// active synchronized transaction.
func (s *Status) SetResponsePending(pending bool) {
	s.responsePending = pending
}

// ResponsePending reports whether the server holds response data.
func (s *Status) ResponsePending() bool {
	return s.responsePending
}

// LatchServiceRequest records a service request carrying the given status byte,
// and reports whether it is a new event.
//
// A false result means one is already outstanding and this event coalesces into
// it. That is not a memory optimisation but a protocol requirement: IVI-6.1
// section 6.13 says no bit of the reported status register is cleared and the
// client clears RQS by performing AsyncStatusQuery, so a further
// AsyncServiceRequest must not be sent until it has. See R-SRV-023.
func (s *Status) LatchServiceRequest(status byte) bool {
	if s.srqLatched {
		return false
	}
	s.srqLatched = true
	s.srqStatus = status
	return true
}

// ServiceRequestOutstanding reports whether a service request is awaiting
// acknowledgement by an AsyncStatusQuery.
func (s *Status) ServiceRequestOutstanding() bool {
	return s.srqLatched
}

// ServiceRequestStatus returns the status byte captured when the outstanding
// service request was latched.
func (s *Status) ServiceRequestStatus() byte {
	return s.srqStatus
}

// TakeServiceRequest reports whether a service request was outstanding and
// clears the latch.
//
// The latch exists because a GPIB bridge must serial-poll the instrument to
// discover why SRQ was asserted, and that poll may clear the instrument's own RQS
// indication. Holding the bit on the HiSLIP side until the client asks preserves
// the externally visible behaviour despite the bridge having consumed the
// instrument's. See §16.2.
func (s *Status) TakeServiceRequest() bool {
	latched := s.srqLatched
	s.srqLatched = false
	s.srqStatus = 0
	return latched
}

// MAV computes the message-available bit for an AsyncStatusQuery carrying
// queryID and rmtDelivered.
//
// Three rules apply in order, per §11.4.1:
//
//   - R-SYNC-030: a queryID unequal to the last received synchronous MessageID
//     forces MAV false. The server either has no data to deliver or is about to
//     be interrupted by a pending synchronous message. It need not detect and
//     report that interrupted error here; normal interrupted processing will.
//   - R-SYNC-031: RMT-delivered forces MAV false.
//   - R-SYNC-032: otherwise the server's own output state decides.
func (s *Status) MAV(queryID protocol.MessageID, rmtDelivered bool) bool {
	if queryID != s.LastReceived() {
		return false
	}
	if rmtDelivered {
		return false
	}
	return s.responsePending
}

// Byte merges the MAV bit computed by MAV into a status byte obtained from the
// device, and returns the value to report in AsyncStatusResponse.
//
// Every other bit is passed through untouched, including RQS. A bridge must not
// clear bits it does not own, and it must not clear RQS at all.
//
// A consequence worth documenting in the product manual: the byte returned here
// and the value an instrument gives to a *STB? query travelling through the
// normal data path can legitimately differ in bit 4, because this one is subject
// to the MessageID rule and that one is not. That is inherent to bridging.
func (s *Status) Byte(
	device byte,
	queryID protocol.MessageID,
	rmtDelivered bool,
) byte {
	if s.MAV(queryID, rmtDelivered) {
		return device | StatusMAV
	}
	return device &^ StatusMAV
}
