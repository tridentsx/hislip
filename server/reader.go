// Copyright (c) 2026 The hislip developers. All rights reserved.
// Project site: https://github.com/tridentsx/hislip
// Use of this source code is governed by a MIT-style license that
// can be found in the LICENSE.txt file for the project.

package server

import (
	"github.com/tridentsx/hislip/protocol"
)

// readAheadSlots is the number of payload buffers a reader owns.
//
// Two is the minimum that gives genuine read-ahead: one buffer holds the message
// the consumer is working on while the other receives the next. A third would buy
// nothing, because the message channel holds at most one.
const readAheadSlots = 2

// message is one received HiSLIP message.
type message struct {
	header  protocol.Header
	payload []byte

	// buf is the buffer payload points into. The consumer returns it with
	// reader.release when it has finished with the payload, and must not touch
	// payload afterwards.
	buf []byte

	// err is non-nil when the message could not be accepted. The header is still
	// valid and the payload has already been drained, so the session can reply
	// and stay synchronised.
	err error
}

// reader reads messages from one channel of a session, keeping one message ahead
// of the consumer.
//
// Read-ahead exists for one reason: the interrupted check of IVI-6.1 section
// 3.1.1 rule 1 requires the server to know whether client input is queued at the
// moment it would send a response terminator. A server that reads one message at
// a time cannot know, because the only way to find out is to have read it.
// Without this, Response.InputQueued could only ever answer false, and the
// interrupted transaction would be unreachable.
//
// Buffer ownership is explicit rather than inferred from timing. A buffer is
// handed to the consumer with the message and is not reused until the consumer
// returns it. The alternative, reasoning that a buffer must be free because the
// consumer has taken a later message, is true but fragile: it depends on the
// channel capacity and on the consumer's internal order, either of which a later
// change could invalidate silently. Here a premature reuse is impossible rather
// than merely unlikely.
type reader struct {
	stream  Stream
	channel protocol.Channel
	limit   uint64

	// free carries buffers available for reading into.
	free chan []byte

	// msgs carries messages to the consumer. Its capacity of one is what bounds
	// read-ahead to a single message.
	msgs chan message

	// done stops the reading goroutine.
	done chan struct{}
}

// newReader returns a reader for a channel, with payload buffers of limit bytes.
func newReader(stream Stream, channel protocol.Channel, limit uint64) *reader {
	r := &reader{
		stream:  stream,
		channel: channel,
		limit:   limit,
		free:    make(chan []byte, readAheadSlots),
		msgs:    make(chan message, 1),
		done:    make(chan struct{}),
	}
	for i := 0; i < readAheadSlots; i++ {
		r.free <- make([]byte, limit)
	}
	return r
}

// run reads messages until the stream fails or the reader is stopped. It is
// intended to be called in its own goroutine, which is the "sync RX" and "async
// RX" of the concurrency model in §23.
func (r *reader) run() {
	defer close(r.msgs)
	for {
		var buf []byte
		select {
		case buf = <-r.free:
		case <-r.done:
			return
		}

		msg := r.readOne(buf)

		select {
		case r.msgs <- msg:
		case <-r.done:
			return
		}
		if msg.err != nil && !recoverable(msg.err) {
			return
		}
	}
}

// readOne reads a single message into buf.
//
// A message the session will reject is still decoded and its payload still
// drained, so that the stream stays synchronised and the session can reply.
// Refusing a message without consuming its payload desynchronises the connection,
// which then presents as a cascade of invalid prologues rather than as the single
// error it is.
func (r *reader) readOne(buf []byte) message {
	var hdrBuf [protocol.HeaderSize]byte
	h, err := protocol.ReadHeader(r.stream, &hdrBuf)
	if err != nil {
		return message{buf: buf, err: err}
	}

	if h.Length > uint64(len(buf)) {
		// Too large for the buffer. The length is checked against the negotiated
		// maximum and the payload drained through the buffer as scratch; it is
		// never used to size an allocation.
		if derr := protocol.DiscardPayload(r.stream, h.Length, buf); derr != nil {
			return message{header: h, buf: buf, err: derr}
		}
		return message{header: h, buf: buf, err: protocol.ErrMessageTooLarge}
	}

	n := int(h.Length)
	if err := protocol.ReadPayload(
		r.stream,
		h.Length,
		buf[:max64(n, 1)],
		nil,
	); err != nil {
		return message{header: h, buf: buf, err: err}
	}
	return message{header: h, payload: buf[:n], buf: buf}
}

// release returns a buffer for reuse. The consumer calls it once it has finished
// with a message's payload.
func (r *reader) release(buf []byte) {
	if buf == nil {
		return
	}
	select {
	case r.free <- buf:
	default:
		// Cannot happen: the pool has exactly readAheadSlots buffers and each is
		// returned once. Dropping rather than blocking keeps a bug from
		// deadlocking the session.
	}
}

// next returns the next message, or false when the channel has ended.
func (r *reader) next() (message, bool) {
	msg, ok := <-r.msgs
	return msg, ok
}

// pending reports whether a message has already arrived and is waiting.
//
// This is the InputQueued predicate of the interrupted check. A stale false is
// harmless: it means the server does not notice a queued message this instant and
// will notice it on the next transaction. A false true is impossible, because a
// buffered message is a message that really arrived.
func (r *reader) pending() bool {
	return len(r.msgs) > 0
}

// stop ends the reading goroutine.
func (r *reader) stop() {
	select {
	case <-r.done:
	default:
		close(r.done)
	}
}

// recoverable reports whether the session can continue after an error reading a
// message. A malformed header means synchronisation is lost and cannot be
// regained; an oversized payload was drained, so it can.
func recoverable(err error) bool {
	return err == protocol.ErrMessageTooLarge
}

// max64 returns the larger of two ints, used to keep a zero-length read from
// being handed an empty scratch slice.
func max64(a, b int) int {
	if a > b {
		return a
	}
	return b
}
