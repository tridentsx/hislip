// Copyright (c) 2026 The hislip developers. All rights reserved.
// Project site: https://github.com/tridentsx/hislip
// Use of this source code is governed by a MIT-style license that
// can be found in the LICENSE.txt file for the project.

package server

import (
	"bytes"
	"errors"
	"testing"
	"time"

	"github.com/tridentsx/hislip/protocol"
)

// canned is a minimal in-package stream. internal/teststream imports nothing from
// here, but keeping this local avoids a dependency from an in-package test on a
// package that exists for the external ones.
type canned struct {
	data   []byte
	pos    int
	closed bool
}

func (c *canned) Read(p []byte) (int, error) {
	if c.pos >= len(c.data) {
		return 0, errors.New("eof")
	}
	n := copy(p, c.data[c.pos:])
	c.pos += n
	return n, nil
}

func (c *canned) Write(p []byte) (int, error) { return len(p), nil }

func (c *canned) Close() error { c.closed = true; return nil }

// encodeMessages builds a byte stream of the given messages.
func encodeMessages(t *testing.T, msgs ...struct {
	h       protocol.Header
	payload []byte
}) []byte {
	t.Helper()
	var out bytes.Buffer
	var buf [protocol.HeaderSize]byte
	for _, m := range msgs {
		if err := protocol.WriteMessage(&out, m.h, m.payload, &buf); err != nil {
			t.Fatalf("WriteMessage() error = %v", err)
		}
	}
	return out.Bytes()
}

type msgSpec = struct {
	h       protocol.Header
	payload []byte
}

func TestReaderDeliversMessagesInOrder(t *testing.T) {
	stream := &canned{data: encodeMessages(t,
		msgSpec{protocol.Header{Type: protocol.Data, Parameter: 1}, []byte("one")},
		msgSpec{protocol.Header{Type: protocol.Data, Parameter: 2}, []byte("two")},
		msgSpec{protocol.Header{Type: protocol.DataEnd, Parameter: 3}, []byte("three")},
	)}
	r := newReader(stream, protocol.ChannelSynchronous, 64)
	go r.run()
	defer r.stop()

	want := []struct {
		param   uint32
		payload string
	}{{1, "one"}, {2, "two"}, {3, "three"}}

	for i, w := range want {
		msg, ok := r.next()
		if !ok {
			t.Fatalf("message %d: channel closed early", i)
		}
		if msg.err != nil {
			t.Fatalf("message %d: err = %v", i, msg.err)
		}
		if msg.header.Parameter != w.param {
			t.Errorf("message %d parameter = %d, want %d", i, msg.header.Parameter, w.param)
		}
		if got := string(msg.payload); got != w.payload {
			t.Errorf("message %d payload = %q, want %q", i, got, w.payload)
		}
		r.release(msg.buf)
	}
}

// TestReaderPendingDetectsQueuedInput is the property read-ahead exists for. The
// interrupted check of IVI-6.1 section 3.1.1 rule 1 cannot be implemented without
// it, because the only way to know a message has arrived is to have read it.
func TestReaderPendingDetectsQueuedInput(t *testing.T) {
	stream := &canned{data: encodeMessages(t,
		msgSpec{protocol.Header{Type: protocol.DataEnd, Parameter: 1}, []byte("first")},
		msgSpec{protocol.Header{Type: protocol.DataEnd, Parameter: 2}, []byte("second")},
	)}
	r := newReader(stream, protocol.ChannelSynchronous, 64)
	go r.run()
	defer r.stop()

	first, ok := r.next()
	if !ok {
		t.Fatal("no first message")
	}

	// The reader should now have read the second message ahead. Poll briefly,
	// since the reading goroutine is not synchronised with this one.
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) && !r.pending() {
		time.Sleep(time.Millisecond)
	}
	if !r.pending() {
		t.Error("pending() = false with a second message already on the wire")
	}

	// The first message's payload is still intact: the buffer holding it has not
	// been reused for the second, which is the ownership guarantee.
	if got := string(first.payload); got != "first" {
		t.Errorf("the first payload became %q; a buffer was reused too early", got)
	}
	r.release(first.buf)

	second, ok := r.next()
	if !ok {
		t.Fatal("no second message")
	}
	if got := string(second.payload); got != "second" {
		t.Errorf("second payload = %q", got)
	}
	r.release(second.buf)
}

// TestReaderBuffersDoNotAlias reads more messages than there are buffers,
// confirming that a released buffer is reused and that a held payload is never
// corrupted while it is held.
func TestReaderBuffersDoNotAlias(t *testing.T) {
	const count = 12
	specs := make([]msgSpec, count)
	for i := range specs {
		specs[i] = msgSpec{
			protocol.Header{Type: protocol.Data, Parameter: uint32(i)},
			bytes.Repeat([]byte{byte('a' + i)}, 16),
		}
	}
	stream := &canned{data: encodeMessages(t, specs...)}
	r := newReader(stream, protocol.ChannelSynchronous, 64)
	go r.run()
	defer r.stop()

	for i := 0; i < count; i++ {
		msg, ok := r.next()
		if !ok {
			t.Fatalf("message %d: channel closed early", i)
		}
		want := bytes.Repeat([]byte{byte('a' + i)}, 16)
		// Hold the payload across a pause, so that the reader has every chance
		// to fill another buffer meanwhile.
		time.Sleep(time.Millisecond)
		if !bytes.Equal(msg.payload, want) {
			t.Fatalf("message %d payload = %q, want %q", i, msg.payload, want)
		}
		r.release(msg.buf)
	}
}

// TestReaderDrainsOversizedPayload confirms an oversized message is reported but
// its payload consumed, so the stream stays synchronised and the next message is
// still found. Rejecting without draining presents as a cascade of invalid
// prologues rather than the single error it is.
func TestReaderDrainsOversizedPayload(t *testing.T) {
	stream := &canned{data: encodeMessages(t,
		msgSpec{protocol.Header{Type: protocol.Data, Parameter: 1}, bytes.Repeat([]byte("x"), 200)},
		msgSpec{protocol.Header{Type: protocol.DataEnd, Parameter: 2}, []byte("after")},
	)}
	r := newReader(stream, protocol.ChannelSynchronous, 64)
	go r.run()
	defer r.stop()

	first, ok := r.next()
	if !ok {
		t.Fatal("no first message")
	}
	if !errors.Is(first.err, protocol.ErrMessageTooLarge) {
		t.Errorf("err = %v, want ErrMessageTooLarge", first.err)
	}
	if first.header.Type != protocol.Data {
		t.Errorf("the header was not decoded: type = %v", first.header.Type)
	}
	r.release(first.buf)

	second, ok := r.next()
	if !ok {
		t.Fatal("the reader stopped after an oversized message; the payload was not drained")
	}
	if second.err != nil {
		t.Fatalf("second message err = %v", second.err)
	}
	if got := string(second.payload); got != "after" {
		t.Errorf("second payload = %q, want \"after\"; the stream lost synchronisation", got)
	}
	r.release(second.buf)
}

// TestReaderStopsOnUnrecoverableError confirms the reader ends the channel when
// synchronisation cannot be regained.
func TestReaderStopsOnUnrecoverableError(t *testing.T) {
	// A valid message followed by rubbish.
	data := encodeMessages(t, msgSpec{protocol.Header{Type: protocol.Data}, []byte("ok")})
	data = append(data, []byte("not a hislip header at all")...)

	r := newReader(&canned{data: data}, protocol.ChannelSynchronous, 64)
	go r.run()
	defer r.stop()

	first, ok := r.next()
	if !ok || first.err != nil {
		t.Fatalf("first message: ok=%v err=%v", ok, first.err)
	}
	r.release(first.buf)

	bad, ok := r.next()
	if !ok {
		t.Fatal("no error message delivered")
	}
	if !errors.Is(bad.err, protocol.ErrInvalidPrologue) {
		t.Errorf("err = %v, want ErrInvalidPrologue", bad.err)
	}
	r.release(bad.buf)

	// The channel must now close rather than spin on a stream it cannot parse.
	if _, ok := r.next(); ok {
		t.Error("the reader continued after losing synchronisation")
	}
}

func TestReaderZeroLengthPayload(t *testing.T) {
	stream := &canned{data: encodeMessages(t,
		msgSpec{protocol.Header{Type: protocol.Trigger, Parameter: 7}, nil},
	)}
	r := newReader(stream, protocol.ChannelSynchronous, 64)
	go r.run()
	defer r.stop()

	msg, ok := r.next()
	if !ok {
		t.Fatal("no message")
	}
	if msg.err != nil {
		t.Fatalf("err = %v", msg.err)
	}
	if len(msg.payload) != 0 {
		t.Errorf("payload = %q, want empty", msg.payload)
	}
	if msg.header.Type != protocol.Trigger {
		t.Errorf("type = %v, want Trigger", msg.header.Type)
	}
	r.release(msg.buf)
}

func TestReaderStopIsIdempotent(t *testing.T) {
	r := newReader(&canned{}, protocol.ChannelSynchronous, 64)
	go r.run()
	r.stop()
	r.stop()
}

func TestReaderReleaseNilIsHarmless(t *testing.T) {
	r := newReader(&canned{}, protocol.ChannelSynchronous, 64)
	r.release(nil)
}

func TestRecoverableClassification(t *testing.T) {
	if !recoverable(protocol.ErrMessageTooLarge) {
		t.Error("an oversized message should be recoverable; its payload was drained")
	}
	if recoverable(protocol.ErrInvalidPrologue) {
		t.Error("a bad prologue is not recoverable; synchronisation is lost")
	}
}
