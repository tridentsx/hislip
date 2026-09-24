// Copyright (c) 2026 The hislip developers. All rights reserved.
// Project site: https://github.com/tridentsx/hislip
// Use of this source code is governed by a MIT-style license that
// can be found in the LICENSE.txt file for the project.

// Package protocol implements the HiSLIP wire format defined by IVI-6.1,
// revision 2.0.
//
// It contains the 16-byte header codec, the message type table, the
// IVI-defined error codes, protocol version handling and the MessageID
// arithmetic. It implements no session or transport behaviour, and it holds no
// state beyond the MessageID Counter.
//
// # Device-side constraints
//
// This package is compiled for microcontroller targets under TinyGo, so it is
// restricted to a small subset of the standard library and must not allocate in
// the data path. Two rules follow, and both are enforced by a test in the root
// package of this module:
//
//   - the only imports permitted are encoding/binary, errors, io and strconv;
//   - the caller supplies every buffer.
//
// The second rule is why ReadHeader, WriteHeader, WriteMessage and ReadPayload
// all take a buffer or scratch slice rather than allocating one. A session is
// expected to own its buffers for the lifetime of a connection.
//
// # Never size a buffer from the wire
//
// The header's payload length field is 64 bits wide and is supplied by the
// peer. Code of the form
//
//	payload := make([]byte, header.Length)
//
// is a denial-of-service vector on an unauthenticated TCP port. Use
// CheckLength against the negotiated maximum, then stream the payload through a
// fixed scratch buffer with ReadPayload.
//
// # Decoding is permissive by design
//
// DecodeHeader validates the prologue and nothing else. It accepts undefined,
// reserved and vendor-specific message types and any payload length, because a
// receiver must be able to decode a header it intends to reject in order to
// consume the payload and stay synchronised. Use MessageType.Defined,
// MessageType.Reserved, MessageType.LegalOn, MessageType.MinVersion and
// CheckLength to decide whether to accept a message, and DiscardPayload to
// resynchronise after refusing one.
package protocol
