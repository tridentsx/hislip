// Copyright (c) 2026 The hislip developers. All rights reserved.
// Project site: https://github.com/gotmc/hislip
// Use of this source code is governed by a MIT-style license that
// can be found in the LICENSE.txt file for the project.

package protocol

import (
	"bytes"
	"errors"
	"io"
	"testing"
)

func TestCheckLength(t *testing.T) {
	tests := []struct {
		name    string
		n, max  uint64
		wantErr bool
	}{
		{"under", 100, 1024, false},
		{"equal", 1024, 1024, false},
		{"over", 1025, 1024, true},
		{"unlimited", 1 << 40, 0, false},
		{"maxUint64 against limit", ^uint64(0), 1024, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := CheckLength(tc.n, tc.max)
			if tc.wantErr && !errors.Is(err, ErrMessageTooLarge) {
				t.Errorf("CheckLength() error = %v, want ErrMessageTooLarge", err)
			}
			if !tc.wantErr && err != nil {
				t.Errorf("CheckLength() error = %v, want nil", err)
			}
		})
	}
}

func TestReadPayloadChunks(t *testing.T) {
	payload := make([]byte, 1000)
	for i := range payload {
		payload[i] = byte(i)
	}
	for _, scratchSize := range []int{1, 7, 64, 999, 1000, 4096} {
		var got bytes.Buffer
		scratch := make([]byte, scratchSize)
		maxChunk := 0
		err := ReadPayload(
			bytes.NewReader(payload),
			uint64(len(payload)),
			scratch,
			func(b []byte) error {
				if len(b) > maxChunk {
					maxChunk = len(b)
				}
				got.Write(b)
				return nil
			},
		)
		if err != nil {
			t.Fatalf("scratch %d: ReadPayload() error = %v", scratchSize, err)
		}
		if !bytes.Equal(got.Bytes(), payload) {
			t.Errorf("scratch %d: payload mismatch", scratchSize)
		}
		if maxChunk > scratchSize {
			t.Errorf("scratch %d: chunk of %d bytes exceeded scratch", scratchSize, maxChunk)
		}
	}
}

// TestReadPayloadHugeLengthDoesNotAllocate is the Milestone 0 exit criterion in
// test form: a peer that declares an enormous payload must not be able to cause
// a large allocation. The reader supplies only a few bytes, so the call fails
// with an I/O error rather than attempting to buffer the declared length.
func TestReadPayloadHugeLengthDoesNotAllocate(t *testing.T) {
	const declared = ^uint64(0) // 2^64-1 bytes
	scratch := make([]byte, 256)
	consumed := 0
	err := ReadPayload(
		bytes.NewReader([]byte("only twenty bytes...")),
		declared,
		scratch,
		func(b []byte) error {
			consumed += len(b)
			return nil
		},
	)
	if err == nil {
		t.Fatal("ReadPayload() error = nil, want an I/O error")
	}
	if !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		t.Errorf("ReadPayload() error = %v, want an EOF error", err)
	}
	if consumed > len(scratch) {
		t.Errorf("consumed %d bytes, want at most the scratch size %d", consumed, len(scratch))
	}
}

func TestReadPayloadZeroLength(t *testing.T) {
	scratch := make([]byte, 16)
	calls := 0
	err := ReadPayload(bytes.NewReader(nil), 0, scratch, func([]byte) error {
		calls++
		return nil
	})
	if err != nil {
		t.Fatalf("ReadPayload() error = %v", err)
	}
	if calls != 0 {
		t.Errorf("consume called %d times for an empty payload, want 0", calls)
	}
}

func TestReadPayloadRejectsEmptyScratch(t *testing.T) {
	err := ReadPayload(bytes.NewReader([]byte("abc")), 3, nil, nil)
	if !errors.Is(err, ErrShortBuffer) {
		t.Errorf("ReadPayload() error = %v, want ErrShortBuffer", err)
	}
}

func TestReadPayloadPropagatesConsumeError(t *testing.T) {
	sentinel := errors.New("consume failed")
	scratch := make([]byte, 4)
	err := ReadPayload(
		bytes.NewReader(make([]byte, 64)),
		64,
		scratch,
		func([]byte) error { return sentinel },
	)
	if !errors.Is(err, sentinel) {
		t.Errorf("ReadPayload() error = %v, want the consume error", err)
	}
}

func TestDiscardPayloadResynchronises(t *testing.T) {
	// A rejected message followed by a valid one. Discarding the first
	// payload must leave the reader positioned at the second header.
	var stream bytes.Buffer
	var buf [HeaderSize]byte
	reject := Header{Type: VendorSpecificMin}
	if err := WriteMessage(&stream, reject, []byte("ignored payload"), &buf); err != nil {
		t.Fatalf("WriteMessage() error = %v", err)
	}
	next := Header{Type: Trigger, Parameter: uint32(InitialMessageID)}
	if err := WriteMessage(&stream, next, nil, &buf); err != nil {
		t.Fatalf("WriteMessage() error = %v", err)
	}

	h, err := ReadHeader(&stream, &buf)
	if err != nil {
		t.Fatalf("ReadHeader() error = %v", err)
	}
	scratch := make([]byte, 8)
	if err := DiscardPayload(&stream, h.Length, scratch); err != nil {
		t.Fatalf("DiscardPayload() error = %v", err)
	}
	got, err := ReadHeader(&stream, &buf)
	if err != nil {
		t.Fatalf("second ReadHeader() error = %v", err)
	}
	if got.Type != Trigger {
		t.Errorf("after discard, Type = %v, want Trigger", got.Type)
	}
}

func TestWriteMessageSetsLength(t *testing.T) {
	var out bytes.Buffer
	var buf [HeaderSize]byte
	payload := []byte("hislip0")
	h := Header{Type: Initialize}
	if err := WriteMessage(&out, h, payload, &buf); err != nil {
		t.Fatalf("WriteMessage() error = %v", err)
	}
	got, err := DecodeHeader(out.Bytes())
	if err != nil {
		t.Fatalf("DecodeHeader() error = %v", err)
	}
	if got.Length != uint64(len(payload)) {
		t.Errorf("Length = %d, want %d", got.Length, len(payload))
	}
	if body := out.Bytes()[HeaderSize:]; !bytes.Equal(body, payload) {
		t.Errorf("payload = %q, want %q", body, payload)
	}
}

func TestWriteMessageRejectsLengthMismatch(t *testing.T) {
	var out bytes.Buffer
	var buf [HeaderSize]byte
	h := Header{Type: Data, Length: 99}
	err := WriteMessage(&out, h, []byte("four"), &buf)
	if !errors.Is(err, ErrPayloadLength) {
		t.Errorf("WriteMessage() error = %v, want ErrPayloadLength", err)
	}
	if out.Len() != 0 {
		t.Errorf("wrote %d bytes on error, want 0", out.Len())
	}
}

func TestWriteMessageEmptyPayload(t *testing.T) {
	var out bytes.Buffer
	var buf [HeaderSize]byte
	// A DataEnd with no payload still terminates the input message.
	h := Header{Type: DataEnd, Parameter: uint32(InitialMessageID)}
	if err := WriteMessage(&out, h, nil, &buf); err != nil {
		t.Fatalf("WriteMessage() error = %v", err)
	}
	if out.Len() != HeaderSize {
		t.Errorf("wrote %d bytes, want %d", out.Len(), HeaderSize)
	}
}
