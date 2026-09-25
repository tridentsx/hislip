// Copyright (c) 2026 The hislip developers. All rights reserved.
// Project site: https://github.com/tridentsx/hislip
// Use of this source code is governed by a MIT-style license that
// can be found in the LICENSE.txt file for the project.

package teststream

import (
	"bytes"
	"errors"
	"io"
	"sync"
	"testing"
	"time"
)

func TestCannedReadsThenEOF(t *testing.T) {
	c := NewCanned([]byte("HS\x00\x00"))
	got, err := io.ReadAll(c)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	if string(got) != "HS\x00\x00" {
		t.Errorf("read %q", got)
	}
	// A second read must report EOF rather than block, so that a sequence test
	// fails with a diagnosis instead of hanging.
	if _, err := c.Read(make([]byte, 1)); !errors.Is(err, io.EOF) {
		t.Errorf("second Read() error = %v, want io.EOF", err)
	}
}

func TestCannedCapturesWrites(t *testing.T) {
	c := NewCanned(nil)
	if _, err := c.Write([]byte("abc")); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if _, err := c.Write([]byte("def")); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if got := string(c.Written()); got != "abcdef" {
		t.Errorf("Written() = %q, want \"abcdef\"", got)
	}
	c.ResetOutput()
	if len(c.Written()) != 0 {
		t.Errorf("Written() after ResetOutput = %q, want empty", c.Written())
	}
}

// TestCannedMaxRead forces short reads, which is how a test confirms that a
// caller reassembles a header rather than assuming one Read returns all 16
// bytes. Real sockets do this; in-memory buffers usually do not, which is why
// the knob exists.
func TestCannedMaxRead(t *testing.T) {
	input := bytes.Repeat([]byte("x"), 16)
	c := NewCanned(input)
	c.MaxRead = 3

	buf := make([]byte, 16)
	n, err := c.Read(buf)
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if n != 3 {
		t.Errorf("Read() returned %d bytes, want 3", n)
	}

	// io.ReadFull must still assemble the whole thing.
	rest := make([]byte, 13)
	if _, err := io.ReadFull(c, rest); err != nil {
		t.Fatalf("ReadFull() error = %v", err)
	}
}

func TestCannedInjectedErrors(t *testing.T) {
	sentinel := errors.New("transport failure")

	c := NewCanned([]byte("data"))
	c.ReadErr = sentinel
	if _, err := c.Read(make([]byte, 4)); !errors.Is(err, sentinel) {
		t.Errorf("Read() error = %v, want the injected error", err)
	}

	c = NewCanned(nil)
	c.WriteErr = sentinel
	if _, err := c.Write([]byte("abc")); !errors.Is(err, sentinel) {
		t.Errorf("Write() error = %v, want the injected error", err)
	}
	// The bytes are still recorded, so a test can assert what would have gone
	// out before the failure.
	if got := string(c.Written()); got != "abc" {
		t.Errorf("Written() = %q, want \"abc\"", got)
	}
}

func TestCannedClose(t *testing.T) {
	c := NewCanned([]byte("data"))
	if c.Closed() {
		t.Error("Closed() = true before Close")
	}
	if err := c.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if !c.Closed() {
		t.Error("Closed() = false after Close")
	}
	// Closing twice must be harmless; a teardown path may do it.
	if err := c.Close(); err != nil {
		t.Errorf("second Close() error = %v", err)
	}
	if _, err := c.Read(make([]byte, 1)); !errors.Is(err, ErrClosed) {
		t.Errorf("Read() after Close error = %v, want ErrClosed", err)
	}
	if _, err := c.Write([]byte("x")); !errors.Is(err, ErrClosed) {
		t.Errorf("Write() after Close error = %v, want ErrClosed", err)
	}
}

func TestCannedUnread(t *testing.T) {
	c := NewCanned([]byte("abcdef"))
	if _, err := io.ReadFull(c, make([]byte, 2)); err != nil {
		t.Fatalf("ReadFull() error = %v", err)
	}
	if got := string(c.Unread()); got != "cdef" {
		t.Errorf("Unread() = %q, want \"cdef\"", got)
	}
	// Inspecting must not consume.
	if got := string(c.Unread()); got != "cdef" {
		t.Errorf("Unread() again = %q, want \"cdef\"", got)
	}
	if _, err := io.ReadFull(c, make([]byte, 4)); err != nil {
		t.Fatalf("ReadFull() error = %v", err)
	}
	if c.Unread() != nil {
		t.Errorf("Unread() at EOF = %q, want nil", c.Unread())
	}
}

func TestPipeRoundTrip(t *testing.T) {
	a, b := Pipe()
	var wg sync.WaitGroup
	wg.Go(func() {
		if _, err := a.Write([]byte("ping")); err != nil {
			t.Errorf("Write() error = %v", err)
		}
	})
	buf := make([]byte, 4)
	if _, err := io.ReadFull(b, buf); err != nil {
		t.Fatalf("ReadFull() error = %v", err)
	}
	if string(buf) != "ping" {
		t.Errorf("read %q, want \"ping\"", buf)
	}
	wg.Wait()

	if _, err := b.Write([]byte("pong")); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if _, err := io.ReadFull(a, buf); err != nil {
		t.Fatalf("ReadFull() error = %v", err)
	}
	if string(buf) != "pong" {
		t.Errorf("read %q, want \"pong\"", buf)
	}
}

func TestPipeCloseUnblocksReader(t *testing.T) {
	a, b := Pipe()
	done := make(chan error, 1)
	go func() {
		_, err := b.Read(make([]byte, 4))
		done <- err
	}()
	if err := a.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if err := <-done; !errors.Is(err, io.EOF) {
		t.Errorf("blocked Read() after peer Close = %v, want io.EOF", err)
	}
}

func TestPipeDeadlineAccepted(t *testing.T) {
	a, _ := Pipe()
	// Conn satisfies DeadlineStream so that code paths guarded by that type
	// assertion are exercised.
	if err := a.SetDeadline(time.Now().Add(time.Second)); err != nil {
		t.Errorf("SetDeadline() error = %v", err)
	}
}
