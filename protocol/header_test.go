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

// loadHeaders returns the valid header vectors. They live in
// internal/vectors/headers.txt rather than in this file so that the same corpus
// can drive a firmware test harness or an implementation in another language.
func loadHeaders(t *testing.T) []vectors.Header {
	t.Helper()
	hs, err := vectors.Headers()
	if err != nil {
		t.Fatalf("loading header vectors: %v", err)
	}
	return hs
}

// header converts a vector to the decoded form this package produces.
func header(v vectors.Header) Header {
	return Header{
		Type:      MessageType(v.Type),
		Control:   v.Control,
		Parameter: v.Parameter,
		Length:    v.Length,
	}
}

func TestGoldenHeadersDecode(t *testing.T) {
	for _, v := range loadHeaders(t) {
		t.Run(v.Name, func(t *testing.T) {
			if len(v.Wire) != HeaderSize {
				t.Fatalf("vector is %d bytes, want %d", len(v.Wire), HeaderSize)
			}
			got, err := DecodeHeader(v.Wire)
			if err != nil {
				t.Fatalf("DecodeHeader() error = %v", err)
			}
			if want := header(v); got != want {
				t.Errorf("DecodeHeader() = %+v, want %+v", got, want)
			}
		})
	}
}

func TestGoldenHeadersEncode(t *testing.T) {
	for _, v := range loadHeaders(t) {
		t.Run(v.Name, func(t *testing.T) {
			var buf [HeaderSize]byte
			if err := header(v).Encode(buf[:]); err != nil {
				t.Fatalf("Encode() error = %v", err)
			}
			if !bytes.Equal(buf[:], v.Wire) {
				t.Errorf("Encode() = % x, want % x", buf[:], v.Wire)
			}
		})
	}
}

func TestHeaderRoundTrip(t *testing.T) {
	for _, v := range loadHeaders(t) {
		t.Run(v.Name, func(t *testing.T) {
			var buf [HeaderSize]byte
			var w bytes.Buffer
			want := header(v)
			if err := WriteHeader(&w, want, &buf); err != nil {
				t.Fatalf("WriteHeader() error = %v", err)
			}
			got, err := ReadHeader(&w, &buf)
			if err != nil {
				t.Fatalf("ReadHeader() error = %v", err)
			}
			if got != want {
				t.Errorf("round trip = %+v, want %+v", got, want)
			}
		})
	}
}

// TestInvalidHeaders drives the malformed corpus in
// internal/vectors/invalid.txt.
func TestInvalidHeaders(t *testing.T) {
	vs, err := vectors.Invalids()
	if err != nil {
		t.Fatalf("loading invalid vectors: %v", err)
	}
	for _, v := range vs {
		t.Run(v.Name, func(t *testing.T) {
			var want error
			switch v.Reason {
			case vectors.ReasonPrologue:
				want = ErrInvalidPrologue
			case vectors.ReasonShort:
				want = ErrShortBuffer
			default:
				t.Fatalf("unhandled reason %q", v.Reason)
			}
			if _, err := DecodeHeader(v.Wire); !errors.Is(err, want) {
				t.Errorf("DecodeHeader() error = %v, want %v", err, want)
			}
		})
	}
}

func TestEncodeRejectsShortBuffer(t *testing.T) {
	h := header(loadHeaders(t)[0])
	for n := 0; n < HeaderSize; n++ {
		b := make([]byte, n)
		if err := h.Encode(b); !errors.Is(err, ErrShortBuffer) {
			t.Errorf("Encode(%d bytes) error = %v, want ErrShortBuffer", n, err)
		}
	}
}

func TestReadHeaderTruncatedStream(t *testing.T) {
	full := loadHeaders(t)[0].Wire
	for n := 1; n < HeaderSize; n++ {
		var buf [HeaderSize]byte
		_, err := ReadHeader(bytes.NewReader(full[:n]), &buf)
		if !errors.Is(err, io.ErrUnexpectedEOF) {
			t.Errorf("ReadHeader(%d bytes) error = %v, want io.ErrUnexpectedEOF", n, err)
		}
	}
	var buf [HeaderSize]byte
	if _, err := ReadHeader(bytes.NewReader(nil), &buf); !errors.Is(err, io.EOF) {
		t.Errorf("ReadHeader(empty) error = %v, want io.EOF", err)
	}
}

func TestHeaderAccessors(t *testing.T) {
	h := Header{Parameter: uint32(InitialMessageID), Control: ControlRMTDelivered}
	if got := h.MessageID(); got != InitialMessageID {
		t.Errorf("MessageID() = %#x, want %#x", got, InitialMessageID)
	}
	if !h.RMTDelivered() {
		t.Error("RMTDelivered() = false, want true")
	}
	h.Control = 0
	if h.RMTDelivered() {
		t.Error("RMTDelivered() = true, want false")
	}
}

// TestDecodeHeaderAcceptsRejectableMessages records the deliberate decision
// that decoding does not validate the message type or the length. A receiver
// must decode a header it intends to reject so that it can consume the payload
// and stay synchronised with the peer.
func TestDecodeHeaderAcceptsRejectableMessages(t *testing.T) {
	for _, typ := range []MessageType{40, 127, 128, 255} {
		wire := make([]byte, HeaderSize)
		wire[0], wire[1] = 'H', 'S'
		wire[2] = byte(typ)
		h, err := DecodeHeader(wire)
		if err != nil {
			t.Fatalf("DecodeHeader(type %d) error = %v, want nil", typ, err)
		}
		if h.Type != typ {
			t.Errorf("Type = %d, want %d", h.Type, typ)
		}
	}
}
