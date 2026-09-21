// Copyright (c) 2026 The hislip developers. All rights reserved.
// Project site: https://github.com/gotmc/hislip
// Use of this source code is governed by a MIT-style license that
// can be found in the LICENSE.txt file for the project.

package server

import "github.com/gotmc/hislip/protocol"

// Initialize handles an Initialize message arriving on a newly accepted
// synchronous channel, and returns the session it created together with the
// InitializeResponse to send.
//
// The sub-address is the message payload interpreted as ASCII, which the caller
// supplies separately because the payload is streamed rather than buffered by the
// codec. A zero-length sub-address selects the default logical instrument.
//
// On error the session is not created, and the caller reports the error using
// WireErrorFor and closes the connection. Every failure here is fatal by
// nature: there is no session to keep alive.
func (s *Server) Initialize(
	h protocol.Header,
	subAddress string,
	syncStream Stream,
) (*Session, protocol.Header, error) {
	if h.Type != protocol.Initialize {
		return nil, protocol.Header{}, ErrInvalidInitSequence
	}
	if !h.Type.LegalOn(protocol.ChannelSynchronous) {
		return nil, protocol.Header{}, protocol.ErrWrongChannel
	}
	if !s.cfg.acceptsSubAddress(subAddress) {
		return nil, protocol.Header{}, ErrInvalidSubAddress
	}

	clientVersion, _ := protocol.DecodeInitializeParameter(h.Parameter)
	if clientVersion.Less(protocol.Version10) {
		// A client claiming a version below 1.0 is not a HiSLIP client.
		return nil, protocol.Header{}, protocol.ErrProtocolVersion
	}
	negotiated := protocol.Negotiate(clientVersion, s.cfg.MaxVersion)

	sess, err := s.allocateSession(negotiated, syncStream)
	if err != nil {
		return nil, protocol.Header{}, err
	}

	resp := protocol.Header{
		Type: protocol.InitializeResponse,
		// Control code bit 0 advertises the mode preference. Zero means
		// Synchronized Mode, which is the entire advertisement side of the
		// Overlap Mode prohibition; the binding half is the feature bitmap
		// during Device Clear. See §11.5 and R-SRV-012.
		Control:   s.cfg.preferOverlapBit(),
		Parameter: protocol.EncodeInitializeResponseParameter(negotiated, sess.id),
	}
	return sess, resp, nil
}

// AsyncInitialize handles an AsyncInitialize message arriving on a newly
// accepted asynchronous channel, associates it with the session it names, and
// returns the AsyncInitializeResponse to send.
//
// A session becomes usable only at this point. Until then the server must refuse
// normal operations with fatal error 2; see Session.requireReady.
func (s *Server) AsyncInitialize(
	h protocol.Header,
	asyncStream Stream,
) (*Session, protocol.Header, error) {
	if h.Type != protocol.AsyncInitialize {
		return nil, protocol.Header{}, ErrInvalidInitSequence
	}
	if !h.Type.LegalOn(protocol.ChannelAsynchronous) {
		return nil, protocol.Header{}, protocol.ErrWrongChannel
	}

	// The session ID travels in the message parameter. IVI-6.1 Table 3 gives the
	// AsyncInitialize parameter as SessionID with no control code or payload.
	id := uint16(h.Parameter)
	sess := s.Session(id)
	if sess == nil {
		return nil, protocol.Header{}, ErrInvalidSession
	}
	if err := sess.attachAsync(asyncStream); err != nil {
		return nil, protocol.Header{}, err
	}

	return sess, s.asyncInitializeResponse(sess), nil
}

// asyncInitializeResponse builds the AsyncInitializeResponse for a session.
//
// The message parameter carries the server vendor ID, not the session ID. That
// asymmetry with InitializeResponse is easy to get wrong: the client learns the
// session ID from the synchronous response and echoes it here, so repeating it
// would be redundant.
//
// IVI-6.1 Table 3 does not say which half of the 32-bit parameter holds the
// two-character vendor ID. The lower 16 bits are used here, symmetric with the
// client vendor ID in Initialize. This is an interpretation and is recorded as
// such; it is inert while the project has no assigned vendor ID, since the field
// is then zero.
func (s *Server) asyncInitializeResponse(sess *Session) protocol.Header {
	var control uint8
	if sess.version.AtLeast(protocol.Version20) {
		// Bit 0 advertises the Secure Connection capability, which this server
		// does not implement. Bits 2 to 5 are reserved and must be zero.
		control = 0
	}
	vendor := uint32(s.cfg.VendorID[0])<<8 | uint32(s.cfg.VendorID[1])
	return protocol.Header{
		Type:      protocol.AsyncInitializeResponse,
		Control:   control,
		Parameter: vendor,
	}
}

// MaximumMessageSize handles an AsyncMaximumMessageSize message and returns the
// response.
//
// This transaction requires no bus access, so it is a class 0 operation and must
// be answered even while a GPIB transfer is in progress; see §47.1 and
// R-SRV-040. Queueing it behind instrument I/O, as a single operation queue
// would, delays it for no reason.
//
// The payload of both messages is an 8-byte big-endian maximum message size. The
// server answers with the smaller of its own maximum and the client's.
func (s *Server) MaximumMessageSize(
	sess *Session,
	clientMax uint64,
) (protocol.Header, uint64) {
	agreed := clientMax
	if agreed == 0 || agreed > s.cfg.MaxTxPayload {
		agreed = s.cfg.MaxTxPayload
	}
	if sess != nil {
		sess.mu.Lock()
		sess.maxTx = agreed
		sess.mu.Unlock()
	}
	return protocol.Header{
		Type:   protocol.AsyncMaximumMessageSizeResponse,
		Length: 8,
	}, agreed
}
