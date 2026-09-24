// Copyright (c) 2026 The hislip developers. All rights reserved.
// Project site: https://github.com/tridentsx/hislip
// Use of this source code is governed by a MIT-style license that
// can be found in the LICENSE.txt file for the project.

package protocol

import "errors"

// Sentinel errors returned by the codec.
var (
	// ErrShortBuffer indicates a buffer too small to hold a header, or a
	// zero-length payload scratch buffer.
	ErrShortBuffer = errors.New("hislip: buffer too short")

	// ErrInvalidPrologue indicates the two-byte "HS" signature was absent.
	// This is a fatal condition: the stream is not a HiSLIP stream, or
	// synchronisation has been lost and cannot be recovered.
	ErrInvalidPrologue = errors.New("hislip: invalid message prologue")

	// ErrUnknownMessageType indicates a message type that is neither defined
	// by IVI-6.1 nor vendor-specific.
	ErrUnknownMessageType = errors.New("hislip: unknown message type")

	// ErrReservedMessageType indicates a message type in the range reserved
	// for future HiSLIP extensions.
	ErrReservedMessageType = errors.New("hislip: reserved message type")

	// ErrWrongChannel indicates a message arrived on a channel where it is
	// not legal.
	ErrWrongChannel = errors.New("hislip: message not legal on this channel")

	// ErrProtocolVersion indicates a message requiring a higher negotiated
	// protocol version than is in effect, or a version that cannot be
	// negotiated.
	ErrProtocolVersion = errors.New("hislip: unsupported protocol version")

	// ErrMessageTooLarge indicates a payload length above the negotiated
	// maximum.
	ErrMessageTooLarge = errors.New("hislip: message exceeds maximum size")

	// ErrPayloadLength indicates a header Length field that disagrees with the
	// payload supplied to WriteMessage.
	ErrPayloadLength = errors.New("hislip: payload length does not match header")
)

// FatalCodeFor returns the fatal error code a server should report for err, and
// whether err is fatal at all. A false second result means the condition is
// not fatal and should be reported with ErrorCodeFor instead.
func FatalCodeFor(err error) (FatalErrorCode, bool) {
	switch {
	case errors.Is(err, ErrInvalidPrologue), errors.Is(err, ErrShortBuffer):
		return FatalPoorlyFormedHeader, true
	case errors.Is(err, ErrProtocolVersion):
		return FatalInvalidInitSequence, true
	default:
		return FatalUnidentified, false
	}
}

// ErrorCodeFor returns the non-fatal error code a server should report for err.
//
// A condition with no specific code maps to NonFatalUnidentified rather than
// to a reserved value, because codes 6 to 127 are reserved for HiSLIP
// extensions and must not be used by an implementation.
func ErrorCodeFor(err error) ErrorCode {
	switch {
	case errors.Is(err, ErrUnknownMessageType),
		errors.Is(err, ErrReservedMessageType),
		errors.Is(err, ErrWrongChannel):
		return NonFatalUnrecognizedMessageType
	case errors.Is(err, ErrMessageTooLarge):
		return NonFatalMessageTooLarge
	default:
		return NonFatalUnidentified
	}
}
