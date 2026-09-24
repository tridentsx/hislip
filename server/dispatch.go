// Copyright (c) 2026 The hislip developers. All rights reserved.
// Project site: https://github.com/tridentsx/hislip
// Use of this source code is governed by a MIT-style license that
// can be found in the LICENSE.txt file for the project.

package server

import "github.com/tridentsx/hislip/protocol"

// Class is the dispatch class of an incoming client message, per §47.1 of the
// design specification.
//
// Revision 2 of that specification ordered operations by perceived urgency and
// placed Trigger above data. That was a defect. Trigger arrives on the
// synchronous channel, takes a MessageID from the same counter as Data and
// DataEnd, and participates in the RMT check, so allowing it to overtake queued
// data corrupts the MessageID and RMT sequence. The rule is:
//
//	ordering is a property of the channel a message arrived on,
//	not of how urgent the operation feels.
//
// Synchronous-channel operations execute in strict arrival order relative to each
// other. Only asynchronous-channel operations may be reordered ahead of them.
type Class uint8

// Dispatch classes.
const (
	// ClassNoBus operations need no bus access and are answered directly by the
	// server layer. They MUST NOT be placed on the operation queue, so that they
	// are answered even while a transfer is in progress; see R-SRV-040.
	ClassNoBus Class = iota

	// ClassPreemptAbandon operations preempt an in-flight transfer at the next
	// byte boundary and abandon it. Device Clear and interface recovery.
	ClassPreemptAbandon

	// ClassPreemptResume operations preempt an in-flight transfer at the next
	// byte boundary, complete, and then resume it. Serial poll for a status
	// query.
	ClassPreemptResume

	// ClassBoundary operations must not interrupt a byte transfer and execute at
	// a transfer boundary.
	ClassBoundary

	// ClassSyncOrdered operations execute in strict synchronous-channel arrival
	// order. No ClassSyncOrdered operation may overtake another.
	ClassSyncOrdered

	// ClassLifecycle messages are handled by the session lifecycle rather than
	// by the operation dispatcher: initialization, the Device Clear handshake,
	// and error notifications.
	ClassLifecycle

	// ClassUndefined is a message this server does not dispatch, because it is
	// server-to-client, reserved, or not implemented.
	ClassUndefined
)

// String returns a short description of the class.
func (c Class) String() string {
	switch c {
	case ClassNoBus:
		return "no bus access"
	case ClassPreemptAbandon:
		return "preempt and abandon"
	case ClassPreemptResume:
		return "preempt and resume"
	case ClassBoundary:
		return "transfer boundary"
	case ClassSyncOrdered:
		return "synchronous, ordered"
	case ClassLifecycle:
		return "lifecycle"
	default:
		return "undefined"
	}
}

// Queued reports whether an operation of this class is placed on the serialized
// bus operation queue. Class 0 operations are not, which is the point of
// separating them.
func (c Class) Queued() bool {
	switch c {
	case ClassPreemptAbandon, ClassPreemptResume, ClassBoundary, ClassSyncOrdered:
		return true
	default:
		return false
	}
}

// Preempts reports whether an operation of this class may interrupt a transfer
// that is already in progress.
func (c Class) Preempts() bool {
	return c == ClassPreemptAbandon || c == ClassPreemptResume
}

// Ordered reports whether operations of this class must execute in arrival order
// relative to each other.
func (c Class) Ordered() bool {
	return c == ClassSyncOrdered
}

// ClassOf returns the dispatch class of a client-to-server message type.
//
// Vendor-specific types are classified as ClassSyncOrdered, because the only one
// this project defines that reaches the dispatcher is ExtReadRequest, which
// carries a MessageID and must keep its place in the synchronous sequence; see
// R-PROTO-050. A vendor message on the asynchronous channel would need its own
// case here.
func ClassOf(t protocol.MessageType) Class {
	switch t {
	// Class 0: no bus access.
	case protocol.AsyncLock,
		protocol.AsyncLockInfo,
		protocol.AsyncMaximumMessageSize:
		return ClassNoBus

	// Class 1: preempt and abandon.
	case protocol.AsyncDeviceClear:
		return ClassPreemptAbandon

	// Class 2: preempt and resume.
	case protocol.AsyncStatusQuery:
		return ClassPreemptResume

	// Class 3: transfer boundary.
	case protocol.AsyncRemoteLocalControl:
		return ClassBoundary

	// Class 4: synchronous, strictly ordered.
	case protocol.Data,
		protocol.DataEnd,
		protocol.Trigger:
		return ClassSyncOrdered

	// Lifecycle: handled by the session, not the dispatcher.
	case protocol.Initialize,
		protocol.AsyncInitialize,
		protocol.DeviceClearComplete,
		protocol.Error,
		protocol.FatalError:
		return ClassLifecycle
	}

	if t.VendorSpecific() {
		return ClassSyncOrdered
	}
	return ClassUndefined
}

// Accept validates an incoming message against the session state and the
// channel it arrived on, and returns its dispatch class.
//
// The checks are in the order a receiver must apply them. Note in particular
// that an unacceptable message still has a decoded header, so the caller can and
// must drain its payload before replying; rejecting a message without consuming
// its payload desynchronises the stream, which then presents as a cascade of
// invalid prologues.
func (s *Server) Accept(
	sess *Session,
	h protocol.Header,
	channel protocol.Channel,
) (Class, error) {
	if h.Type.Reserved() {
		return ClassUndefined, protocol.ErrReservedMessageType
	}
	if !h.Type.Defined() && !h.Type.VendorSpecific() {
		return ClassUndefined, protocol.ErrUnknownMessageType
	}
	if !h.Type.LegalOn(channel) {
		return ClassUndefined, protocol.ErrWrongChannel
	}
	if sess != nil && sess.version.Less(h.Type.MinVersion()) {
		return ClassUndefined, protocol.ErrProtocolVersion
	}

	class := ClassOf(h.Type)
	if class == ClassUndefined {
		return class, protocol.ErrUnknownMessageType
	}

	// Payload length is checked against the negotiated maximum, never used to
	// size a buffer.
	limit := s.cfg.MaxRxPayload
	if sess != nil {
		limit = sess.maxRx
	}
	if err := protocol.CheckLength(h.Length, limit); err != nil {
		return class, err
	}

	// Normal operations require both channels. Lifecycle messages are exempt,
	// since they are how the session reaches that state.
	if class != ClassLifecycle && sess != nil {
		if err := sess.requireReady(); err != nil {
			return class, err
		}
	}
	return class, nil
}
