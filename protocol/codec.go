// Copyright (c) 2026 The hislip developers. All rights reserved.
// Project site: https://github.com/tridentsx/hislip
// Use of this source code is governed by a MIT-style license that
// can be found in the LICENSE.txt file for the project.

package protocol

import "io"

// CheckLength reports whether a payload length is acceptable.
//
// The length field is 64 bits wide and is supplied by the peer, so it must be
// checked against a negotiated maximum before any buffer decision is made. A
// max of zero means unlimited, which is only appropriate for a host
// implementation that streams.
func CheckLength(n, max uint64) error {
	if max != 0 && n > max {
		return ErrMessageTooLarge
	}
	return nil
}

// ReadPayload reads exactly n bytes from r, passing them to consume in chunks
// of at most len(scratch) bytes.
//
// The payload is never buffered in full, so a peer cannot cause a large
// allocation by declaring a large length. The caller owns scratch and chooses
// its size; 512 to 2048 bytes is appropriate for a device-side session.
//
// If consume returns an error, ReadPayload stops and returns it. The stream is
// then positioned mid-payload and is no longer synchronised: the caller must
// either close the connection or discard the remainder, and it cannot know how
// much remains. Callers that need to stay synchronised should return nil from
// consume and record the condition instead.
func ReadPayload(
	r io.Reader,
	n uint64,
	scratch []byte,
	consume func([]byte) error,
) error {
	if len(scratch) == 0 {
		return ErrShortBuffer
	}
	remaining := n
	for remaining > 0 {
		chunk := min(remaining, uint64(len(scratch)))
		if _, err := io.ReadFull(r, scratch[:chunk]); err != nil {
			return err
		}
		if consume != nil {
			if err := consume(scratch[:chunk]); err != nil {
				return err
			}
		}
		remaining -= chunk
	}
	return nil
}

// DiscardPayload reads and discards exactly n bytes from r.
//
// It is used to stay synchronised with a peer after deciding to reject a
// message. Rejecting a message without consuming its payload desynchronises the
// stream, which then presents as a cascade of invalid prologues.
func DiscardPayload(r io.Reader, n uint64, scratch []byte) error {
	return ReadPayload(r, n, scratch, nil)
}

// WriteMessage writes a header and payload to w.
//
// The header's Length field is set from len(payload); a non-zero Length that
// disagrees with the payload is reported as ErrPayloadLength rather than
// silently corrected, because a mismatch indicates a caller bug that would
// otherwise desynchronise the peer.
//
// The caller supplies the header scratch buffer so that the write path does not
// allocate.
func WriteMessage(
	w io.Writer,
	h Header,
	payload []byte,
	buf *[HeaderSize]byte,
) error {
	if h.Length != 0 && h.Length != uint64(len(payload)) {
		return ErrPayloadLength
	}
	h.Length = uint64(len(payload))
	if err := WriteHeader(w, h, buf); err != nil {
		return err
	}
	if len(payload) == 0 {
		return nil
	}
	_, err := w.Write(payload)
	return err
}
