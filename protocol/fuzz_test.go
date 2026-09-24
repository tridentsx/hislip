// Copyright (c) 2026 The hislip developers. All rights reserved.
// Project site: https://github.com/tridentsx/hislip
// Use of this source code is governed by a MIT-style license that
// can be found in the LICENSE.txt file for the project.

package protocol

import (
	"bytes"
	"errors"
	"io"
	"testing"

	"github.com/tridentsx/hislip/internal/vectors"
)

// seedCorpus adds the golden and malformed vectors to a fuzz target, so that
// fuzzing starts from inputs known to reach interesting code rather than from
// random noise that fails at the prologue.
func seedCorpus(f *testing.F) {
	f.Helper()
	hs, err := vectors.Headers()
	if err != nil {
		f.Fatalf("loading header vectors: %v", err)
	}
	for _, h := range hs {
		f.Add(h.Wire)
	}
	vs, err := vectors.Invalids()
	if err != nil {
		f.Fatalf("loading invalid vectors: %v", err)
	}
	for _, v := range vs {
		f.Add(v.Wire)
	}
}

// FuzzDecodeHeader checks the Milestone 0 exit criterion: malformed network
// input cannot cause a panic.
//
// It also checks a stronger property. Any input that decodes successfully must
// re-encode to exactly the bytes it came from. That is what catches a wrong
// field offset or a byte-order mistake, which a fixed set of golden vectors can
// miss if every vector happens to be symmetric.
func FuzzDecodeHeader(f *testing.F) {
	seedCorpus(f)
	f.Fuzz(func(t *testing.T, data []byte) {
		h, err := DecodeHeader(data)
		if err != nil {
			// Only two rejections are permitted at this layer. Anything else
			// means the decoder has grown a validation rule that belongs to the
			// session layer, which would prevent a receiver from draining the
			// payload of a message it intends to reject.
			if !errors.Is(err, ErrShortBuffer) && !errors.Is(err, ErrInvalidPrologue) {
				t.Fatalf("DecodeHeader() error = %v, want ErrShortBuffer or ErrInvalidPrologue", err)
			}
			return
		}

		if len(data) < HeaderSize {
			t.Fatalf("decoded %d bytes, which is fewer than a header", len(data))
		}
		if data[0] != 'H' || data[1] != 'S' {
			t.Fatalf("decoded a header with prologue %q", data[:2])
		}

		var buf [HeaderSize]byte
		if err := h.Encode(buf[:]); err != nil {
			t.Fatalf("Encode() after successful decode: %v", err)
		}
		if !bytes.Equal(buf[:], data[:HeaderSize]) {
			t.Fatalf("re-encode = % x, want % x", buf[:], data[:HeaderSize])
		}

		again, err := DecodeHeader(buf[:])
		if err != nil {
			t.Fatalf("second DecodeHeader() error = %v", err)
		}
		if again != h {
			t.Fatalf("second decode = %+v, want %+v", again, h)
		}
	})
}

// FuzzReadPayload checks that a peer-declared length cannot make the reader
// misbehave: it must never hand the consumer more than the scratch buffer, never
// deliver more bytes than were declared, and always terminate.
func FuzzReadPayload(f *testing.F) {
	f.Add(uint64(4), uint8(8), []byte("abcd"))
	f.Add(uint64(0), uint8(1), []byte(""))
	f.Add(^uint64(0), uint8(255), []byte("truncated"))
	f.Add(uint64(1000), uint8(1), bytes.Repeat([]byte("x"), 1000))

	f.Fuzz(func(t *testing.T, declared uint64, scratchLen uint8, data []byte) {
		scratch := make([]byte, int(scratchLen)%64+1)

		// Keep the declared length near the amount of data available, so the
		// fuzzer explores the boundary rather than spending its budget looping.
		// The genuinely huge case is covered by
		// TestReadPayloadHugeLengthDoesNotAllocate.
		declared %= uint64(len(data)) + 64

		var delivered uint64
		maxChunk := 0
		err := ReadPayload(
			bytes.NewReader(data),
			declared,
			scratch,
			func(b []byte) error {
				if len(b) > maxChunk {
					maxChunk = len(b)
				}
				delivered += uint64(len(b))
				return nil
			},
		)

		if maxChunk > len(scratch) {
			t.Fatalf("chunk of %d bytes exceeded the %d byte scratch", maxChunk, len(scratch))
		}
		if delivered > declared {
			t.Fatalf("delivered %d bytes, more than the %d declared", delivered, declared)
		}
		if err == nil {
			if delivered != declared {
				t.Fatalf("delivered %d bytes on success, want %d", delivered, declared)
			}
			return
		}
		// The only way to fail here is running out of input.
		if !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
			t.Fatalf("ReadPayload() error = %v, want an EOF error", err)
		}
		if declared <= uint64(len(data)) {
			t.Fatalf("ReadPayload() error = %v with %d bytes available for %d declared",
				err, len(data), declared)
		}
	})
}

// FuzzWriteMessage checks that anything written can be read back unchanged, and
// that the framing stays synchronised so a second message is still found.
func FuzzWriteMessage(f *testing.F) {
	f.Add(uint8(6), uint8(0), uint32(0xffffff00), []byte("*IDN?\n"))
	f.Add(uint8(7), uint8(1), uint32(0xffffffff), []byte(""))
	f.Add(uint8(128), uint8(0), uint32(0), []byte{0x47, 0x54, 0x4d, 0x43})

	f.Fuzz(func(t *testing.T, typ, control uint8, parameter uint32, payload []byte) {
		var buf [HeaderSize]byte
		var stream bytes.Buffer

		want := Header{
			Type:      MessageType(typ),
			Control:   control,
			Parameter: parameter,
		}
		if err := WriteMessage(&stream, want, payload, &buf); err != nil {
			t.Fatalf("WriteMessage() error = %v", err)
		}
		// A second, fixed message. Finding it proves the first message consumed
		// exactly its own bytes.
		sentinel := Header{Type: Trigger, Parameter: uint32(InitialMessageID)}
		if err := WriteMessage(&stream, sentinel, nil, &buf); err != nil {
			t.Fatalf("WriteMessage(sentinel) error = %v", err)
		}

		got, err := ReadHeader(&stream, &buf)
		if err != nil {
			t.Fatalf("ReadHeader() error = %v", err)
		}
		want.Length = uint64(len(payload))
		if got != want {
			t.Fatalf("ReadHeader() = %+v, want %+v", got, want)
		}

		scratch := make([]byte, 8)
		var body bytes.Buffer
		err = ReadPayload(&stream, got.Length, scratch, func(b []byte) error {
			body.Write(b)
			return nil
		})
		if err != nil {
			t.Fatalf("ReadPayload() error = %v", err)
		}
		if !bytes.Equal(body.Bytes(), payload) {
			t.Fatalf("payload = % x, want % x", body.Bytes(), payload)
		}

		next, err := ReadHeader(&stream, &buf)
		if err != nil {
			t.Fatalf("ReadHeader(sentinel) error = %v", err)
		}
		if next.Type != Trigger {
			t.Fatalf("sentinel type = %v, want Trigger; framing lost synchronisation", next.Type)
		}
	})
}
