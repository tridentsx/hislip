// Copyright (c) 2026 The hislip developers. All rights reserved.
// Project site: https://github.com/gotmc/hislip
// Use of this source code is governed by a MIT-style license that
// can be found in the LICENSE.txt file for the project.

package server

import (
	"context"
	"errors"

	"github.com/gotmc/hislip/protocol"
)

// ResponseOutcome reports how a response attempt ended. The caller uses it to
// drive the diagnostic counters of §52; a single "error" result would not let an
// operator distinguish a slow instrument from a mistyped query.
type ResponseOutcome uint8

// Response outcomes.
const (
	// ResponseComplete means the response was delivered and terminated with
	// DataEnd.
	ResponseComplete ResponseOutcome = iota

	// ResponseTruncated means some bytes arrived and then the backend failed.
	// The partial response was delivered and terminated; it is not discarded.
	ResponseTruncated

	// ResponseAbsent means a response was expected and none arrived. See §17.3.
	ResponseAbsent

	// ResponseInterrupted means the response was discarded because client input
	// was queued when the terminator was offered, and the Interrupted
	// transaction was sent. See §11.3.2.
	ResponseInterrupted
)

// String returns a short description.
func (o ResponseOutcome) String() string {
	switch o {
	case ResponseComplete:
		return "complete"
	case ResponseTruncated:
		return "truncated"
	case ResponseAbsent:
		return "absent"
	case ResponseInterrupted:
		return "interrupted"
	default:
		return "unknown outcome"
	}
}

// Response describes one response attempt.
type Response struct {
	// Channels the response is written on. Sync is required; Async is required
	// only so that the Interrupted transaction can be sent, and its absence is
	// refused rather than silently skipped.
	Sync  *MessageWriter
	Async *MessageWriter

	// MessageID is the client message the response is attributed to.
	MessageID protocol.MessageID

	// Scratch is the buffer response bytes are streamed through. The caller owns
	// it; nothing here allocates. Its size sets the HiSLIP packet payload size,
	// so it must not exceed the negotiated maximum.
	Scratch []byte

	// InputQueued reports whether unread client input is waiting on the
	// synchronous channel. It implements the check of IVI-6.1 section 3.1.1
	// rule 1 and must be supplied; a nil value is an error rather than a
	// default, because silently treating it as false would skip the interrupted
	// check and leave the server non-conforming in a way no test would notice.
	InputQueued func() bool
}

// validate reports whether the response parameters are usable.
func (r Response) validate(sess *Session) error {
	if r.Sync == nil || r.Async == nil {
		return ErrInvalidConfig
	}
	if len(r.Scratch) == 0 {
		return protocol.ErrShortBuffer
	}
	if sess != nil && uint64(len(r.Scratch)) > sess.MaxTxPayload() {
		return protocol.ErrMessageTooLarge
	}
	if r.InputQueued == nil {
		return ErrInputQueuedRequired
	}
	return nil
}

// ErrInputQueuedRequired indicates a Response without an InputQueued predicate.
var ErrInputQueuedRequired = errors.New(
	"hislip: Response.InputQueued must be supplied to implement the interrupted check",
)

// ReceiveData records the arrival of a Data or DataEnd message and writes its
// payload to the device.
//
// The order of operations matters and is the reason this is one function rather
// than several. The RMT check must happen on arrival, before the payload reaches
// the instrument, because an interrupted condition means the previous response
// was never read and the bridge must record that fact whether or not this message
// turns out to be valid. The MessageID must be recorded for MAV before any
// response can be attributed to it.
//
// The returned Interrupted value is the silent RMT-mismatch error of IVI-6.1
// section 3.1.1 rule 2 when non-zero. It is not reported to the client; the
// caller counts it. See §11.3.1 and R-SYNC-013.
func (s *Server) ReceiveData(
	ctx context.Context,
	sess *Session,
	tx *Transaction,
	h protocol.Header,
	payload []byte,
) (Interrupted, error) {
	end := h.Type == protocol.DataEnd
	if h.Type != protocol.Data && !end {
		return NotInterrupted, protocol.ErrUnknownMessageType
	}

	interrupted := sess.RMT().ReceivedSyncMessage(h.RMTDelivered())
	sess.Status().ReceivedSyncMessage(h.MessageID())

	if end {
		if err := tx.CompleteInput(h.MessageID()); err != nil {
			return interrupted, err
		}
	} else if err := tx.BeginInput(); err != nil {
		return interrupted, err
	}

	// A DataEnd with an empty payload still terminates the logical input
	// message, so the write is not skipped on length alone.
	if err := s.dev.Write(ctx, payload, end); err != nil {
		return interrupted, err
	}
	return interrupted, nil
}

// SendResponse streams the device's response to the client as zero or more Data
// messages followed by one DataEnd.
//
// What it guarantees, in the order the guarantees are established:
//
//   - the response is never buffered whole, only streamed through r.Scratch;
//   - the terminating DataEnd carries the originating client MessageID, which is
//     what lets a synchronized client correlate it (R-PROTO-022);
//   - the interrupted check of IVI-6.1 section 3.1.1 rule 1 happens before the
//     terminator is sent, not after, because the point of the check is to
//     suppress a terminator that would be wrong;
//   - RMT-expected is set only when a DataEnd is actually written, so a
//     suppressed or failed response does not leave the tracker claiming a
//     terminator was delivered.
func (s *Server) SendResponse(
	ctx context.Context,
	sess *Session,
	tx *Transaction,
	r Response,
) (ResponseOutcome, error) {
	if err := r.validate(sess); err != nil {
		return ResponseAbsent, err
	}
	if err := tx.ExpectResponse(); err != nil {
		return ResponseAbsent, err
	}

	sess.Status().SetResponsePending(true)
	defer sess.Status().SetResponsePending(false)

	delivered := uint64(0)
	for {
		n, end, readErr := s.dev.Read(ctx, r.Scratch)

		if n > 0 {
			if err := tx.BeginResponse(); err != nil {
				return ResponseAbsent, err
			}
			if !end || readErr != nil {
				// An intermediate chunk. The MessageID is carried through
				// because this bridge treats DataEnd as the end-of-message, so
				// the originating end-of-message is always at the end of the
				// identified client message, which is the condition
				// R-PROTO-023 attaches to using the real value. A server that
				// honoured terminators embedded inside Data payloads could not
				// make that claim and would have to send NoMessageID here.
				h := protocol.Header{
					Type:      protocol.Data,
					Parameter: uint32(r.MessageID),
					Length:    uint64(n),
				}
				if err := r.Sync.Write(h, r.Scratch[:n]); err != nil {
					return ResponseTruncated, err
				}
				delivered += uint64(n)
			}
		}

		if readErr != nil {
			if delivered == 0 {
				// No response at all. This is the most common real-world
				// failure: a mistyped query, or a command the read policy
				// misclassified. §17.3 requires that the cause be reported and
				// the transaction terminated, so that a conforming client's read
				// completes instead of stranding until its own timeout.
				out, err := s.completeAbsentResponse(sess, tx, r)
				return out, err
			}
			// Bytes arrived and then the backend failed. The partial response is
			// delivered rather than discarded, per R-DEV-011, and the truncation
			// is reported.
			if err := r.Sync.WriteError(
				CodeResponseTimeout,
				"response truncated",
			); err != nil {
				return ResponseTruncated, err
			}
			if err := s.terminate(sess, tx, r, r.Scratch[:0]); err != nil {
				return ResponseTruncated, err
			}
			return ResponseTruncated, nil
		}

		if !end {
			continue
		}

		// The backend is offering a response message terminator. This is the
		// point at which IVI-6.1 section 3.1.1 rule 1 applies.
		if r.InputQueued() {
			if err := s.sendInterrupted(sess, tx, r); err != nil {
				return ResponseInterrupted, err
			}
			return ResponseInterrupted, nil
		}

		if err := tx.BeginResponse(); err != nil {
			return ResponseAbsent, err
		}
		if err := s.terminate(sess, tx, r, r.Scratch[:n]); err != nil {
			return ResponseComplete, err
		}
		return ResponseComplete, nil
	}
}

// terminate writes the DataEnd that completes a response and updates the
// Synchronized Mode state.
func (s *Server) terminate(
	sess *Session,
	tx *Transaction,
	r Response,
	final []byte,
) error {
	h := protocol.Header{
		Type:      protocol.DataEnd,
		Parameter: uint32(r.MessageID),
		Length:    uint64(len(final)),
	}
	if err := r.Sync.Write(h, final); err != nil {
		return err
	}
	// Only now, with the terminator actually on the wire, does the server claim
	// to have delivered an RMT.
	sess.RMT().SentDataEnd()
	sess.setLastTxMessageID(r.MessageID)
	return tx.Complete()
}

// completeAbsentResponse implements §17.3: report the cause, then terminate the
// transaction with an empty DataEnd so that the client's read completes.
//
// The order is deliberate. The Error comes first so that a client which surfaces
// it has the diagnosis before the empty result arrives; a client which ignores
// Error still makes progress on the DataEnd.
func (s *Server) completeAbsentResponse(
	sess *Session,
	tx *Transaction,
	r Response,
) (ResponseOutcome, error) {
	if err := r.Sync.WriteError(
		CodeResponseTimeout,
		"no response from instrument",
	); err != nil {
		return ResponseAbsent, err
	}
	if err := tx.BeginResponse(); err != nil {
		return ResponseAbsent, err
	}
	if err := s.terminate(sess, tx, r, nil); err != nil {
		return ResponseAbsent, err
	}
	return ResponseAbsent, nil
}

// sendInterrupted performs the Interrupted transaction of §11.3.2.
//
// Both messages are sent, and both carry the MessageID of the message that
// interrupted the response. Sending only one leaves a conforming client
// permanently stalled: one that sees Interrupted first must not send further
// messages until AsyncInterrupted arrives, and one that sees AsyncInterrupted
// first must discard server data until Interrupted arrives. See R-SYNC-022.
//
// AsyncInterrupted is sent first, following IVI-6.1 Table 30. A conforming client
// tolerates either order, but the table's order avoids exercising the
// less-travelled path in third-party clients.
func (s *Server) sendInterrupted(
	sess *Session,
	tx *Transaction,
	r Response,
) error {
	if err := tx.Interrupt(); err != nil {
		return err
	}

	// The buffered response is discarded. Its MessageID association is void, so
	// it must not be retained for a later transaction; see R-SYNC-024.
	sess.Status().SetResponsePending(false)

	h := protocol.Header{
		Type:      protocol.AsyncInterrupted,
		Parameter: uint32(r.MessageID),
	}
	if err := r.Async.Write(h, nil); err != nil {
		return err
	}
	h.Type = protocol.Interrupted
	if err := r.Sync.Write(h, nil); err != nil {
		return err
	}
	return tx.Complete()
}
