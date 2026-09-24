// Copyright (c) 2026 The hislip developers. All rights reserved.
// Project site: https://github.com/tridentsx/hislip
// Use of this source code is governed by a MIT-style license that
// can be found in the LICENSE.txt file for the project.

package protocol

import (
	"encoding/binary"
	"io"
)

// HeaderSize is the size in bytes of every HiSLIP message header.
const HeaderSize = 16

// Prologue is the two-byte signature that begins every HiSLIP message.
var Prologue = [2]byte{'H', 'S'}

// Header is a HiSLIP message header.
//
// The prologue is not stored, because it is invariant. All multibyte fields
// are carried on the wire in network byte order.
//
//	offset  size  field
//	0       2     "HS"
//	2       1     message type
//	3       1     control code
//	4       4     message parameter
//	8       8     payload length
type Header struct {
	Type      MessageType
	Control   uint8
	Parameter uint32
	Length    uint64
}

// DecodeHeader decodes a header from the first HeaderSize bytes of b.
//
// It validates the prologue and nothing else. In particular it accepts
// undefined, reserved and vendor-specific message types, and it accepts any
// payload length. This is deliberate: a receiver must be able to decode a
// header it intends to reject so that it can consume the payload and stay
// synchronised with the peer. Rejection is the session layer's decision, using
// MessageType.Defined, MessageType.Reserved, MessageType.LegalOn and
// CheckLength.
func DecodeHeader(b []byte) (Header, error) {
	if len(b) < HeaderSize {
		return Header{}, ErrShortBuffer
	}
	if b[0] != Prologue[0] || b[1] != Prologue[1] {
		return Header{}, ErrInvalidPrologue
	}
	return Header{
		Type:      MessageType(b[2]),
		Control:   b[3],
		Parameter: binary.BigEndian.Uint32(b[4:8]),
		Length:    binary.BigEndian.Uint64(b[8:16]),
	}, nil
}

// Encode writes h into the first HeaderSize bytes of b.
func (h Header) Encode(b []byte) error {
	if len(b) < HeaderSize {
		return ErrShortBuffer
	}
	b[0] = Prologue[0]
	b[1] = Prologue[1]
	b[2] = byte(h.Type)
	b[3] = h.Control
	binary.BigEndian.PutUint32(b[4:8], h.Parameter)
	binary.BigEndian.PutUint64(b[8:16], h.Length)
	return nil
}

// ReadHeader reads exactly HeaderSize bytes from r and decodes them.
//
// The caller supplies the scratch buffer so that a session can read headers
// without allocating. A device-side session is expected to own one buffer for
// the lifetime of the connection.
func ReadHeader(r io.Reader, buf *[HeaderSize]byte) (Header, error) {
	if _, err := io.ReadFull(r, buf[:]); err != nil {
		return Header{}, err
	}
	return DecodeHeader(buf[:])
}

// WriteHeader encodes h into buf and writes it to w.
func WriteHeader(w io.Writer, h Header, buf *[HeaderSize]byte) error {
	if err := h.Encode(buf[:]); err != nil {
		return err
	}
	_, err := w.Write(buf[:])
	return err
}

// MessageID returns the message parameter interpreted as a MessageID. It is
// only meaningful for the Data, DataEnd, Trigger and AsyncStatusQuery messages
// and for vendor messages that participate in the synchronous sequence.
func (h Header) MessageID() MessageID {
	return MessageID(h.Parameter)
}

// RMTDelivered reports whether the RMT-delivered flag is set in the control
// code. It is only meaningful for the client-to-server messages that carry the
// flag: Data, DataEnd, Trigger, AsyncStatusQuery, AsyncStartTLS and
// AsyncEndTLS.
func (h Header) RMTDelivered() bool {
	return h.Control&ControlRMTDelivered != 0
}
