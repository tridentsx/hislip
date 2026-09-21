// Copyright (c) 2026 The hislip developers. All rights reserved.
// Project site: https://github.com/gotmc/hislip
// Use of this source code is governed by a MIT-style license that
// can be found in the LICENSE.txt file for the project.

package server

import (
	"context"

	"github.com/gotmc/hislip/protocol"
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
