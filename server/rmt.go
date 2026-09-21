// Copyright (c) 2026 The hislip developers. All rights reserved.
// Project site: https://github.com/gotmc/hislip
// Use of this source code is governed by a MIT-style license that
// can be found in the LICENSE.txt file for the project.

package server

// Interrupted is the kind of interrupted protocol error a Synchronized Mode
// server has detected.
//
// IVI-6.1 section 3.1.1 defines two interrupted errors with different
// consequences, and conflating them is the predictable implementation mistake:
// one is reported to the client and one is not. They are separate values here so
// that a caller cannot handle them together by accident.
type Interrupted uint8

// Interrupted kinds.
const (
	// NotInterrupted means no interrupted error was detected.
	NotInterrupted Interrupted = iota

	// InterruptedRMTMismatch is the error of IVI-6.1 section 3.1.1 rule 2: a
	// Data, DataEnd or Trigger arrived whose RMT-delivered flag disagreed with
	// the server's RMT-expected state.
	//
	// This error is reported only through the server's internal error
	// mechanism. No indication is sent to the client. Sending an Interrupted
	// transaction here would be a protocol violation.
	InterruptedRMTMismatch

	// InterruptedInputQueued is the error of IVI-6.1 section 3.1.1 rule 1: the
	// application layer offered a response message terminator while unread
	// client input was still queued.
	//
	// This error requires the Interrupted transaction: the buffered response is
	// discarded and both AsyncInterrupted and Interrupted are sent. See §11.3.2.
	InterruptedInputQueued
)

// Notifies reports whether this interrupted error is signalled to the client.
// Only InterruptedInputQueued is.
func (i Interrupted) Notifies() bool {
	return i == InterruptedInputQueued
}

// String returns a short description.
func (i Interrupted) String() string {
	switch i {
	case NotInterrupted:
		return "not interrupted"
	case InterruptedRMTMismatch:
		return "interrupted: RMT-delivered disagreed with RMT-expected"
	case InterruptedInputQueued:
		return "interrupted: response terminator offered with input queued"
	default:
		return "unknown interrupted kind"
	}
}

// RMT tracks the RMT-expected state of one session.
//
// RMT stands for response message terminator. The mechanism is how a
// Synchronized Mode server detects that a client sent a new program message
// without having read the response to the previous one, which IEEE 488.2 calls
// the interrupted condition. It is mandatory for a Synchronized Mode server and
// is the substance of what Synchronized Mode consists of.
//
// The client's side of the mechanism is a single flag in control code bit 0 of
// Data, DataEnd, Trigger, AsyncStatusQuery, AsyncStartTLS and AsyncEndTLS,
// which it sets on the first such message after delivering a terminator to its
// application layer, and only once per terminator.
//
// The zero RMT is ready for use: RMT-expected starts false, which is correct at
// session start because no DataEnd has been sent.
type RMT struct {
	expected bool
}

// Expected reports the current RMT-expected state. Exposed for diagnostics and
// tests; the session logic uses the methods below.
func (r *RMT) Expected() bool {
	return r.expected
}

// SentDataEnd records that the server has sent a DataEnd, which is to say that
// it has delivered a response message terminator. RMT-expected becomes true.
//
// IVI-6.1 section 3.1.1: "RMT-expected shall be set true when the server sends
// a DataEND message (that is, when it sends an RMT)."
func (r *RMT) SentDataEnd() {
	r.expected = true
}

// ReceivedSyncMessage records the arrival of a Data, DataEnd or Trigger message
// carrying rmtDelivered, and reports whether an interrupted error is declared.
//
// IVI-6.1 section 3.1.1:
//
//   - when RMT-expected and RMT-delivered are both true or both false, clear
//     RMT-expected;
//   - when they differ, declare an interrupted error, reported internally only.
//
// Interpretation: the standard does not say what becomes of RMT-expected in the
// mismatch case. This implementation clears it, so that one client mistake
// produces one error rather than an error on every subsequent message. The
// alternative, leaving it set, turns a single missed read into an unbounded
// cascade and contradicts "the server resumes normal operation". This choice is
// checked by TestRMTMismatchDoesNotCascade and should be confirmed against a
// third-party client during interoperability testing.
func (r *RMT) ReceivedSyncMessage(rmtDelivered bool) Interrupted {
	mismatch := r.expected != rmtDelivered
	r.expected = false
	if mismatch {
		return InterruptedRMTMismatch
	}
	return NotInterrupted
}

// ReceivedStatusQuery records the arrival of an AsyncStatusQuery, AsyncStartTLS
// or AsyncEndTLS carrying rmtDelivered. When the flag is set, RMT-expected is
// cleared; otherwise nothing changes.
//
// IVI-6.1 section 3.1.1: "The RMT-expected bit shall be cleared when the server
// receives AsyncStatusQuery, AsyncStartTLS, or AsyncEndTLS with RMT-delivered
// flag set to true."
//
// Unlike ReceivedSyncMessage this cannot declare an interrupted error. A status
// query is an asynchronous-channel operation and does not participate in the
// synchronous message sequence.
func (r *RMT) ReceivedStatusQuery(rmtDelivered bool) {
	if rmtDelivered {
		r.expected = false
	}
}

// Reset returns the tracker to its session-start state. Called on
// initialization and after Device Clear completes.
func (r *RMT) Reset() {
	r.expected = false
}
