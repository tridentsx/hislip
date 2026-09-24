// Copyright (c) 2026 The hislip developers. All rights reserved.
// Project site: https://github.com/tridentsx/hislip
// Use of this source code is governed by a MIT-style license that
// can be found in the LICENSE.txt file for the project.

package server

import (
	"errors"

	"github.com/tridentsx/hislip/protocol"
)

// Sentinel errors returned by the server.
//
// Codec-level errors come from the protocol package; these are the conditions
// the session layer adds. The mapping to wire error codes is in §21.1 and is
// implemented by WireError.
var (
	// ErrSessionNotReady indicates an operation attempted before both channels
	// were associated. It is fatal, code 2.
	ErrSessionNotReady = errors.New("hislip: session not ready, both channels required")

	// ErrInvalidSession indicates an AsyncInitialize naming a session ID that
	// does not exist or is already paired.
	ErrInvalidSession = errors.New("hislip: unknown or already paired session ID")

	// ErrInvalidInitSequence indicates the first message on a channel was not
	// the one initialization requires: Initialize on the synchronous channel,
	// AsyncInitialize on the asynchronous channel. It is fatal, code 3.
	ErrInvalidInitSequence = errors.New("hislip: invalid initialization sequence")

	// ErrInvalidSubAddress indicates an Initialize naming a sub-address this
	// server does not serve.
	ErrInvalidSubAddress = errors.New("hislip: unknown sub-address")

	// ErrMaxClients indicates a second logical session was attempted. It is
	// fatal, code 4, and must be reported rather than silently stalled.
	ErrMaxClients = errors.New("hislip: maximum number of clients exceeded")

	// ErrLocked indicates an operation blocked by a lock held by another
	// session. It is answered through AsyncLockResponse, not an Error message.
	ErrLocked = errors.New("hislip: locked by another session")

	// ErrUnsupportedMode indicates a client requested Overlap Mode from a
	// profile that does not support it. The server answers by returning
	// Synchronized in the feature bitmap; this error is for internal accounting.
	ErrUnsupportedMode = errors.New("hislip: requested mode not supported")

	// ErrUnrecognizedControlCode indicates a control code the standard does not
	// define for that message, such as a remote/local mode above 6. It is
	// non-fatal, code 2.
	ErrUnrecognizedControlCode = errors.New("hislip: unrecognized control code")
)

// Bridge-specific device-defined error codes, from the 128 to 255 range that
// IVI-6.1 reserves for the device. See §21.1.
const (
	// CodeHandshakeTimeout is a GPIB three-wire handshake timeout.
	CodeHandshakeTimeout protocol.ErrorCode = 128

	// CodeResponseTimeout is a read that produced no response when one was
	// expected. See §17.3 and R-DEV-010.
	CodeResponseTimeout protocol.ErrorCode = 129

	// CodeNoListener means no device acknowledged addressing on the bus.
	CodeNoListener protocol.ErrorCode = 130

	// CodeNoSerialPollResponse means the instrument did not answer a serial poll.
	CodeNoSerialPollResponse protocol.ErrorCode = 131

	// CodeUndeterminedDisposition means the read policy could not decide
	// whether a response was expected and is configured to fail.
	CodeUndeterminedDisposition protocol.ErrorCode = 132

	// CodeInterfaceRecovering means an operation was rejected because the
	// interface is recovering.
	CodeInterfaceRecovering protocol.ErrorCode = 133
)

// WireError describes how a condition is reported on the wire.
type WireError struct {
	// Fatal reports whether a FatalError message is sent and the session closed.
	Fatal bool

	// FatalCode is meaningful when Fatal is true.
	FatalCode protocol.FatalErrorCode

	// Code is meaningful when Fatal is false.
	Code protocol.ErrorCode
}

// WireErrorFor maps an error to its wire representation.
//
// A GPIB timeout is an operation error and must leave the session usable; it is
// never reported as fatal. See R-SRV-032. ErrWrongChannel is likewise non-fatal
// so that a confused but recoverable client is not disconnected, though the
// session should count these and escalate past a threshold; see R-SRV-031.
func WireErrorFor(err error) WireError {
	if code, fatal := protocol.FatalCodeFor(err); fatal {
		return WireError{Fatal: true, FatalCode: code}
	}
	switch {
	case errors.Is(err, ErrSessionNotReady):
		return WireError{Fatal: true, FatalCode: protocol.FatalChannelsNotEstablished}
	case errors.Is(err, ErrInvalidSession), errors.Is(err, ErrInvalidSubAddress),
		errors.Is(err, ErrInvalidInitSequence):
		return WireError{Fatal: true, FatalCode: protocol.FatalInvalidInitSequence}
	case errors.Is(err, ErrMaxClients):
		return WireError{Fatal: true, FatalCode: protocol.FatalMaxClientsExceeded}
	case errors.Is(err, ErrUnrecognizedControlCode):
		return WireError{Code: protocol.NonFatalUnrecognizedControlCode}
	}
	return WireError{Code: protocol.ErrorCodeFor(err)}
}
