// Copyright (c) 2026 The hislip developers. All rights reserved.
// Project site: https://github.com/gotmc/hislip
// Use of this source code is governed by a MIT-style license that
// can be found in the LICENSE.txt file for the project.

// Package teststream provides in-memory streams for protocol tests.
//
// Two shapes are offered, for two different kinds of test.
//
// Canned is for deterministic sequence tests: preload the bytes a peer would
// send, run the code under test, then inspect exactly what it wrote. No
// goroutines are involved, so a failure has a stack trace that means something
// and a hang is impossible.
//
// Pipe is for round-trip tests where both halves run concurrently, as a client
// and server do. It blocks like a socket.
//
// This package is test support and is not device-side code, so it is not subject
// to the import restrictions of the protocol and server packages.
package teststream

import (
	"bytes"
	"errors"
	"io"
	"sync"
	"time"
)

// ErrClosed is returned by operations on a closed stream.
var ErrClosed = errors.New("teststream: stream is closed")

// Canned is a stream whose reads come from a preloaded buffer and whose writes
// are captured.
//
// Reads return io.EOF once the input is exhausted rather than blocking, which is
// what makes sequence tests terminate instead of hanging.
type Canned struct {
	in     bytes.Reader
	out    bytes.Buffer
	closed bool

	// ReadErr, when set, is returned by Read instead of data. It is used to
	// exercise transport failure paths.
	ReadErr error

	// WriteErr, when set, is returned by Write. Bytes are still recorded, so a
	// test can assert what would have been sent.
	WriteErr error

	// MaxRead, when non-zero, caps the bytes returned by one Read, so that a
	// test can force short reads and confirm the caller reassembles correctly.
	MaxRead int
}

// NewCanned returns a stream that will read the given bytes.
func NewCanned(input []byte) *Canned {
	c := &Canned{}
	c.in.Reset(input)
	return c
}

// Read implements the server Stream interface.
func (c *Canned) Read(p []byte) (int, error) {
	if c.closed {
		return 0, ErrClosed
	}
	if c.ReadErr != nil {
		return 0, c.ReadErr
	}
	if c.MaxRead > 0 && len(p) > c.MaxRead {
		p = p[:c.MaxRead]
	}
	return c.in.Read(p)
}

// Write implements the server Stream interface.
func (c *Canned) Write(p []byte) (int, error) {
	if c.closed {
		return 0, ErrClosed
	}
	n, err := c.out.Write(p)
	if err != nil {
		return n, err
	}
	return n, c.WriteErr
}

// Close implements the server Stream interface. Closing twice is not an error,
// because a session teardown path may legitimately close more than once.
func (c *Canned) Close() error {
	c.closed = true
	return nil
}

// Closed reports whether Close has been called.
func (c *Canned) Closed() bool {
	return c.closed
}

// Written returns the bytes written so far.
func (c *Canned) Written() []byte {
	return c.out.Bytes()
}

// Unread returns the input bytes not yet consumed. A non-empty result at the end
// of a test usually means the code under test stopped early or lost
// synchronisation.
func (c *Canned) Unread() []byte {
	b := make([]byte, c.in.Len())
	if len(b) == 0 {
		return nil
	}
	// Read from a copy so that inspection does not consume the stream.
	r := c.in
	if _, err := io.ReadFull(&r, b); err != nil {
		return nil
	}
	return b
}

// ResetOutput discards captured writes, which is convenient between phases of a
// multi-step test.
func (c *Canned) ResetOutput() {
	c.out.Reset()
}

// Pipe returns two connected streams. Bytes written to one are read from the
// other. Reads block until data is available or the peer closes.
func Pipe() (*Conn, *Conn) {
	a2b := newBuffer()
	b2a := newBuffer()
	return &Conn{read: b2a, write: a2b}, &Conn{read: a2b, write: b2a}
}

// Conn is one end of a Pipe.
type Conn struct {
	read  *buffer
	write *buffer
	once  sync.Once
}

// Read implements the server Stream interface.
func (c *Conn) Read(p []byte) (int, error) { return c.read.Read(p) }

// Write implements the server Stream interface.
func (c *Conn) Write(p []byte) (int, error) { return c.write.Write(p) }

// Close closes this end. The peer's pending and subsequent reads return io.EOF
// once it has drained what was already written.
func (c *Conn) Close() error {
	c.once.Do(func() {
		c.write.close()
		c.read.close()
	})
	return nil
}

// buffer is a blocking byte queue.
type buffer struct {
	mu     sync.Mutex
	cond   *sync.Cond
	data   []byte
	closed bool
}

func newBuffer() *buffer {
	b := &buffer{}
	b.cond = sync.NewCond(&b.mu)
	return b
}

func (b *buffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return 0, ErrClosed
	}
	b.data = append(b.data, p...)
	b.cond.Broadcast()
	return len(p), nil
}

func (b *buffer) Read(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for len(b.data) == 0 {
		if b.closed {
			return 0, io.EOF
		}
		b.cond.Wait()
	}
	n := copy(p, b.data)
	b.data = b.data[n:]
	return n, nil
}

func (b *buffer) close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.closed = true
	b.cond.Broadcast()
}

// SetDeadline satisfies the server DeadlineStream interface. Deadlines are
// accepted and ignored, because an in-memory stream has no timing to enforce;
// the method exists so that code paths guarded by a DeadlineStream type
// assertion are exercised by tests.
func (c *Conn) SetDeadline(time.Time) error { return nil }
