// Copyright (c) 2026 The hislip developers. All rights reserved.
// Project site: https://github.com/tridentsx/hislip
// Use of this source code is governed by a MIT-style license that
// can be found in the LICENSE.txt file for the project.

//go:build !race

// The race detector adds its own allocations, so these assertions are excluded
// from race builds. They still run in the default `go test ./...`.

package protocol

import (
	"io"
	"testing"
)

// fixedReader serves a fixed buffer without allocating, so that an allocation
// measured inside AllocsPerRun is attributable to the code under test.
type fixedReader struct {
	data []byte
	pos  int
}

func (r *fixedReader) Read(p []byte) (int, error) {
	if r.pos >= len(r.data) {
		return 0, io.EOF
	}
	n := copy(p, r.data[r.pos:])
	r.pos += n
	return n, nil
}

// nullWriter discards without allocating.
type nullWriter struct{ n int }

func (w *nullWriter) Write(p []byte) (int, error) {
	w.n += len(p)
	return len(p), nil
}

// TestCodecDoesNotAllocate is the executable form of the zero-allocation rule
// for the data path. See the design specification, R-FW-021 and R-FW-106.
//
// It matters more than it looks. On the RP2350 the garbage collector stops both
// cores, so an allocation during a transfer can stall a timing-sensitive GPIB
// handshake, and a spurious handshake timeout presents to the user as a bus
// fault. Asserting the property here rather than at Milestone 8 means a
// regression is caught by the commit that introduces it.
func TestCodecDoesNotAllocate(t *testing.T) {
	const runs = 100

	h := Header{
		Type:      DataEnd,
		Control:   ControlRMTDelivered,
		Parameter: uint32(InitialMessageID),
		Length:    4,
	}
	wire := make([]byte, HeaderSize)
	if err := h.Encode(wire); err != nil {
		t.Fatalf("Encode() error = %v", err)
	}

	// Everything the measured closures touch is allocated up front.
	var buf [HeaderSize]byte
	scratch := make([]byte, 64)
	payload := make([]byte, 512)
	message := make([]byte, 0, HeaderSize+len(payload))
	message = append(message, wire...)
	message = append(message, payload...)
	reader := &fixedReader{data: message}
	writer := &nullWriter{}
	discard := func([]byte) error { return nil }
	var err error

	t.Run("DecodeHeader", func(t *testing.T) {
		n := testing.AllocsPerRun(runs, func() {
			_, err = DecodeHeader(wire)
		})
		if err != nil {
			t.Fatalf("DecodeHeader() error = %v", err)
		}
		assertNoAllocs(t, "DecodeHeader", n)
	})

	t.Run("Encode", func(t *testing.T) {
		n := testing.AllocsPerRun(runs, func() {
			err = h.Encode(buf[:])
		})
		if err != nil {
			t.Fatalf("Encode() error = %v", err)
		}
		assertNoAllocs(t, "Header.Encode", n)
	})

	t.Run("ReadHeader", func(t *testing.T) {
		n := testing.AllocsPerRun(runs, func() {
			reader.pos = 0
			_, err = ReadHeader(reader, &buf)
		})
		if err != nil {
			t.Fatalf("ReadHeader() error = %v", err)
		}
		assertNoAllocs(t, "ReadHeader", n)
	})

	t.Run("ReadPayload", func(t *testing.T) {
		n := testing.AllocsPerRun(runs, func() {
			reader.pos = HeaderSize
			err = ReadPayload(reader, uint64(len(payload)), scratch, discard)
		})
		if err != nil {
			t.Fatalf("ReadPayload() error = %v", err)
		}
		assertNoAllocs(t, "ReadPayload", n)
	})

	t.Run("WriteMessage", func(t *testing.T) {
		n := testing.AllocsPerRun(runs, func() {
			err = WriteMessage(writer, Header{Type: Data}, payload, &buf)
		})
		if err != nil {
			t.Fatalf("WriteMessage() error = %v", err)
		}
		assertNoAllocs(t, "WriteMessage", n)
	})

	t.Run("MessageIDCounter", func(t *testing.T) {
		c := NewCounter()
		n := testing.AllocsPerRun(runs, func() {
			c.Take()
			_ = c.Last()
		})
		assertNoAllocs(t, "Counter.Take", n)
	})
}

func assertNoAllocs(t *testing.T, name string, got float64) {
	t.Helper()
	if got != 0 {
		t.Errorf(
			"%s allocated %.1f times per run, want 0.\n"+
				"The device-side data path must not allocate; see R-FW-021.",
			name, got,
		)
	}
}

// TestReadPayloadScratchIsTheOnlyBuffer confirms that the amount of memory
// ReadPayload touches is set by the scratch buffer and not by the declared
// length, which is the property that makes a 64-bit peer-supplied length safe.
func TestReadPayloadScratchIsTheOnlyBuffer(t *testing.T) {
	const declared = 1 << 20 // 1 MiB declared
	scratch := make([]byte, 32)
	data := make([]byte, declared)
	reader := &fixedReader{data: data}
	discard := func([]byte) error { return nil }

	n := testing.AllocsPerRun(5, func() {
		reader.pos = 0
		if err := ReadPayload(reader, declared, scratch, discard); err != nil {
			t.Fatalf("ReadPayload() error = %v", err)
		}
	})
	assertNoAllocs(t, "ReadPayload over 1 MiB", n)
}
