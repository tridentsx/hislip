// Copyright (c) 2026 The hislip developers. All rights reserved.
// Project site: https://github.com/tridentsx/hislip
// Use of this source code is governed by a MIT-style license that
// can be found in the LICENSE.txt file for the project.

package server

import (
	"context"
	"time"

	"github.com/tridentsx/hislip/protocol"
)

// StatusQuery handles an AsyncStatusQuery message and returns the
// AsyncStatusResponse to send.
//
// Both fields of the request are used, which Revision 2 of the design
// specification overlooked by treating this message as parameterless:
//
//   - the RMT-delivered flag in control code bit 0 clears RMT-expected, one of the
//     two ways that state is cleared (§11.3.1);
//   - the MessageID in the message parameter decides MAV (§11.4.1).
//
// The status byte travels in the control code of the response, not in a payload.
//
// This is a class 2 operation: it needs the bus, for a serial poll, but it may
// preempt a transfer in progress and resume it. A status query that waited for a
// stalled transfer would be useless, because a stalled transfer is when a client
// asks. See §47.2.
func (s *Server) StatusQuery(
	ctx context.Context,
	sess *Session,
	h protocol.Header,
) (protocol.Header, error) {
	if h.Type != protocol.AsyncStatusQuery {
		return protocol.Header{}, protocol.ErrUnknownMessageType
	}
	if err := sess.requireReady(); err != nil {
		return protocol.Header{}, err
	}

	rmtDelivered := h.RMTDelivered()
	sess.RMT().ReceivedStatusQuery(rmtDelivered)

	device, err := s.dev.ReadStatusByte(ctx)
	if err != nil {
		return protocol.Header{}, err
	}

	stb := sess.Status().Byte(device, h.MessageID(), rmtDelivered)

	// The bridge's RQS latch outlives its own serial poll and is cleared only
	// here, by the client's status query. Without this, a bridge that polled the
	// instrument to discover why SRQ was asserted would have consumed the
	// instrument's RQS and the client would never see it. See §16.2.
	if sess.Status().TakeServiceRequest() {
		stb |= StatusRQS
	}

	return protocol.Header{
		Type:    protocol.AsyncStatusResponse,
		Control: stb,
	}, nil
}

// ServiceRequest builds an AsyncServiceRequest for the given status byte, and
// reports whether it should be sent.
//
// A false result means a service request is already outstanding and this event
// coalesces into it. IVI-6.1 section 6.13 requires that: no bit of the reported
// register is cleared, and the client clears RQS by performing AsyncStatusQuery,
// so sending another before it has would be wrong as well as wasteful.
//
// The message is not acknowledged and must not be retried; see R-SRV-021.
//
// A fidelity limit worth recording in the product manual: two instrument events
// occurring before the client issues its status query collapse into one reported
// status byte, and the second is lost. That is inherent to bridging a
// level-signalled bus onto a message protocol, not a defect in this code.
func (s *Server) ServiceRequest(
	sess *Session,
	status byte,
) (protocol.Header, bool) {
	if sess == nil || !sess.Ready() {
		return protocol.Header{}, false
	}
	if !sess.Status().LatchServiceRequest(status) {
		return protocol.Header{}, false
	}
	return protocol.Header{
		Type:    protocol.AsyncServiceRequest,
		Control: status,
	}, true
}

// RemoteLocal handles an AsyncRemoteLocalControl message and returns the
// AsyncRemoteLocalResponse to send.
//
// This is a class 3 operation: it needs the bus but must not interrupt a byte
// transfer, so it executes at a transfer boundary. See §47.1.
//
// An undefined mode is answered with a non-fatal Error, control code 2,
// "Unrecognized control code", rather than being passed to the backend. The
// effects table in device.go is what a GPIB backend implements against; passing an
// undefined mode through would leave the backend deciding what to do with a
// request the standard does not define.
func (s *Server) RemoteLocal(
	ctx context.Context,
	sess *Session,
	h protocol.Header,
) (protocol.Header, error) {
	if h.Type != protocol.AsyncRemoteLocalControl {
		return protocol.Header{}, protocol.ErrUnknownMessageType
	}
	if err := sess.requireReady(); err != nil {
		return protocol.Header{}, err
	}

	mode := RemoteLocalMode(h.Control)
	if !mode.Valid() {
		return protocol.Header{}, ErrUnrecognizedControlCode
	}
	if err := s.dev.RemoteLocal(ctx, mode); err != nil {
		return protocol.Header{}, err
	}
	return protocol.Header{Type: protocol.AsyncRemoteLocalResponse}, nil
}

// Lock control codes carried by an AsyncLock message.
const (
	lockRelease uint8 = 0
	lockRequest uint8 = 1
)

// Lock handles an AsyncLock message and returns the AsyncLockResponse to send.
//
// The lockString is the message payload interpreted as ASCII, which the caller
// supplies separately because the payload is streamed rather than buffered. It is
// meaningful only for a request; a zero-length string requests the exclusive lock.
//
// This is a class 0 operation. It needs no bus access, so it must be answered
// even while a transfer is in progress, and it must not be placed on the bus
// queue. IVI-6.1 is explicit that lock transactions complete even while a lock
// held by another client blocks the synchronous channel. See R-SRV-040.
//
// The server always replies, whatever the outcome.
func (s *Server) Lock(
	ctx context.Context,
	sess *Session,
	h protocol.Header,
	lockString string,
) (protocol.Header, error) {
	if h.Type != protocol.AsyncLock {
		return protocol.Header{}, protocol.ErrUnknownMessageType
	}
	if err := sess.requireReady(); err != nil {
		return protocol.Header{}, err
	}

	var result LockResult
	switch h.Control {
	case lockRequest:
		// The 32-bit timeout in milliseconds travels in the message parameter,
		// the field the standard otherwise calls MessageID.
		timeout := time.Duration(h.Parameter) * time.Millisecond
		result = s.locks.Request(ctx, sess.ID(), lockString, timeout)
	case lockRelease:
		result = s.locks.Release(sess.ID())
	default:
		return protocol.Header{}, ErrUnrecognizedControlCode
	}

	return protocol.Header{
		Type:    protocol.AsyncLockResponse,
		Control: uint8(result),
	}, nil
}

// LockInfo handles an AsyncLockInfo message and returns the
// AsyncLockInfoResponse.
//
// IVI-6.1 requires this transaction to be processed whether or not the client
// holds a lock, so it does not consult the lock state for permission. The values
// are sampled and may be stale by the time the client reads them, which the
// standard also says; they need only be self-consistent at the moment of
// sampling.
func (s *Server) LockInfo(
	sess *Session,
	h protocol.Header,
) (protocol.Header, error) {
	if h.Type != protocol.AsyncLockInfo {
		return protocol.Header{}, protocol.ErrUnknownMessageType
	}
	if err := sess.requireReady(); err != nil {
		return protocol.Header{}, err
	}

	exclusive, holders := s.locks.Info()
	var control uint8
	if exclusive {
		control = 1
	}
	return protocol.Header{
		Type:      protocol.AsyncLockInfoResponse,
		Control:   control,
		Parameter: uint32(holders),
	}, nil
}

// Locks returns the server's lock manager, for diagnostics and tests.
func (s *Server) Locks() *LockManager { return &s.locks }
