// Copyright (c) 2026 The hislip developers. All rights reserved.
// Project site: https://github.com/tridentsx/hislip
// Use of this source code is governed by a MIT-style license that
// can be found in the LICENSE.txt file for the project.

package server

import (
	"context"

	"github.com/tridentsx/hislip/protocol"
)

// BeginDeviceClear handles an AsyncDeviceClear message on the asynchronous
// channel and returns the AsyncDeviceClearAcknowledge to send.
//
// This is step 2 of the four-message transaction in §13.1. It performs the
// obligations IVI-6.1 section 6.12 places on the server at this point:
//
//   - abandon any bus operation in progress, without waiting for its timeout;
//   - move the session into its clearing state, in which synchronous messages are
//     accepted and discarded until DeviceClearComplete arrives;
//   - propose the feature bitmap the server prefers.
//
// Abandoning the operation is the part that makes Device Clear worth having. An
// operator issues one because the instrument is stuck, so a clear that waited for
// the stuck operation's timeout would be useless. See R-SRV-016.
//
// The instrument is not cleared here. IVI-6.1 places the device-level clear after
// the protocol buffers are cleared and the DeviceClearComplete has arrived, which
// is CompleteDeviceClear below.
func (s *Server) BeginDeviceClear(
	sess *Session,
	tx *Transaction,
) (protocol.Header, error) {
	if err := sess.requireReady(); err != nil {
		return protocol.Header{}, err
	}

	// Step 1 and 4 of the server procedure: abandon operations in progress and
	// discard buffered unsent transactions.
	sess.abortOperation()
	sess.Status().SetResponsePending(false)

	if err := tx.Clear(); err != nil {
		return protocol.Header{}, err
	}
	sess.beginClear()

	return protocol.Header{
		Type: protocol.AsyncDeviceClearAcknowledge,
		// The server proposes the features it prefers, and must support
		// everything it proposes. A synchronized-only profile therefore proposes
		// bit 0 clear.
		Control: s.cfg.featureBitmap(protocol.FeatureOverlapped),
	}, nil
}

// CompleteDeviceClear handles a DeviceClearComplete message on the synchronous
// channel, clears the instrument, and returns the DeviceClearAcknowledge to send.
//
// This is step 4 of §13.1. The feature bitmap in h is the client's request; the
// bitmap returned is binding, and the client must use it.
//
// Note the channel. IVI-6.1 Table 4 lists DeviceClearAcknowledge as
// asynchronous, which contradicts the transaction description in section 6.12,
// where step 7 of the client procedure directs the client to wait for it on the
// synchronous channel. The transaction description is followed here; see §4.4 and
// R-PROTO-040.
func (s *Server) CompleteDeviceClear(
	ctx context.Context,
	sess *Session,
	tx *Transaction,
	h protocol.Header,
) (protocol.Header, error) {
	if h.Type != protocol.DeviceClearComplete {
		return protocol.Header{}, protocol.ErrUnknownMessageType
	}

	// The negotiated mode. A profile that does not implement Overlap Mode
	// returns bit 0 clear whatever the client asked for, which is the binding
	// half of the prohibition; see R-SRV-012.
	agreed := s.cfg.featureBitmap(h.Control)
	overlap := agreed&protocol.FeatureOverlapped != 0

	// Protocol state is reset before the instrument is touched, so that a
	// failure in the backend cannot leave the protocol half-cleared.
	sess.completeClear(overlap)
	tx.Reset()

	if err := s.dev.Clear(ctx); err != nil {
		// The instrument refused or failed to clear. The protocol state is
		// already consistent and the session stays usable, so this is an
		// operation error rather than a session failure. The caller reports it
		// and still sends the acknowledgement, because withholding it would
		// leave a conforming client waiting indefinitely at step 7.
		return protocol.Header{
			Type:    protocol.DeviceClearAcknowledge,
			Control: agreed,
		}, err
	}

	return protocol.Header{
		Type:    protocol.DeviceClearAcknowledge,
		Control: agreed,
	}, nil
}

// DiscardDuringClear reports whether a synchronous message arriving mid-clear
// should be discarded rather than acted on.
//
// IVI-6.1 section 6.12 step 6: the server accepts and ignores synchronous
// messages until it finds DeviceClearComplete, while continuing to require
// well-formed messages. "Accepts and ignores" is the important part — the payload
// must still be drained, or the stream desynchronises and the DeviceClearComplete
// is never found.
func DiscardDuringClear(sess *Session, h protocol.Header) bool {
	if sess.State() != StateClearing {
		return false
	}
	return h.Type != protocol.DeviceClearComplete
}
