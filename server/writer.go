// Copyright (c) 2026 The hislip developers. All rights reserved.
// Project site: https://github.com/gotmc/hislip
// Use of this source code is governed by a MIT-style license that
// can be found in the LICENSE.txt file for the project.

package server

import "github.com/gotmc/hislip/protocol"

// MessageWriter writes HiSLIP messages to one channel of a session.
//
// It owns the 16-byte header buffer for the lifetime of the connection, so the
// write path allocates nothing. A session holds one writer per channel; they are
// not interchangeable, because a message legal on one channel is generally not
// legal on the other.
//
// A MessageWriter is not safe for concurrent use. Each channel has a single
// writer by design: interleaving two messages on one TCP connection would
// corrupt the framing, so serialisation is a protocol requirement rather than an
// implementation convenience.
type MessageWriter struct {
	stream  Stream
	channel protocol.Channel
	buf     [protocol.HeaderSize]byte
	written uint64
}

// NewMessageWriter returns a writer for a channel.
func NewMessageWriter(s Stream, channel protocol.Channel) *MessageWriter {
	return &MessageWriter{stream: s, channel: channel}
}

// Channel returns the channel this writer serves.
func (w *MessageWriter) Channel() protocol.Channel { return w.channel }

// Bytes returns the total number of bytes written, headers included. It feeds
// the diagnostic counters of §52.
func (w *MessageWriter) Bytes() uint64 { return w.written }

// Write sends one message.
//
// The header's Length is taken from the payload, and a mismatch with a non-zero
// Length is refused rather than silently corrected, because a mismatch would
// desynchronise the peer.
//
// The message type is checked against the writer's channel. This catches the
// class of bug where a response is sent on the wrong connection, which is
// otherwise diagnosed only by a confused client: the peer reads a header it
// considers illegal on that channel and the session fails for a reason unrelated
// to its apparent cause.
func (w *MessageWriter) Write(h protocol.Header, payload []byte) error {
	if !h.Type.LegalOn(w.channel) {
		return protocol.ErrWrongChannel
	}
	if err := protocol.WriteMessage(w.stream, h, payload, &w.buf); err != nil {
		return err
	}
	w.written += protocol.HeaderSize + uint64(len(payload))
	return nil
}

// WriteError sends a non-fatal Error message with an ASCII description.
//
// The description may be empty; a zero-length payload is legal. It is not
// formatted here, because formatting would pull fmt into the device-side import
// set, and because a fixed description is more useful in a field log than a
// constructed one.
func (w *MessageWriter) WriteError(code protocol.ErrorCode, description string) error {
	return w.Write(protocol.Header{
		Type:    protocol.Error,
		Control: uint8(code),
	}, []byte(description))
}

// WriteFatalError sends a FatalError message. The caller closes the session
// afterwards; this method does not, so that both channels can be told before
// either is torn down.
func (w *MessageWriter) WriteFatalError(
	code protocol.FatalErrorCode,
	description string,
) error {
	return w.Write(protocol.Header{
		Type:    protocol.FatalError,
		Control: uint8(code),
	}, []byte(description))
}
