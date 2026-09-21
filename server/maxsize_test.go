// Copyright (c) 2026 The hislip developers. All rights reserved.
// Project site: https://github.com/gotmc/hislip
// Use of this source code is governed by a MIT-style license that
// can be found in the LICENSE.txt file for the project.

package server_test

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/gotmc/hislip/internal/testdevice"
	"github.com/gotmc/hislip/internal/teststream"
	"github.com/gotmc/hislip/protocol"
	"github.com/gotmc/hislip/server"
)

// TestMaximumMessageSizeDirection is a regression test for a bug found by
// lxi-tools/libhislip on its first query.
//
// IVI-6.1 Table 28 makes each half of the transaction independent, and it is easy
// to read as a negotiation of a common minimum. The client's value tells the
// server the largest message it may send, so it sets the transmit limit. The
// server's reply tells the client the largest message the server can receive, so
// it carries the server's receive limit. An implementation that returns the
// smaller of the two and applies it to its transmit limit gets both halves wrong.
func TestMaximumMessageSizeDirection(t *testing.T) {
	dev := testdevice.New()
	srv, err := server.New(dev, server.Config{MaxRxPayload: 8192, MaxTxPayload: 8192})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	sess, _, err := srv.Initialize(protocol.Header{
		Type:      protocol.Initialize,
		Parameter: protocol.EncodeInitializeParameter(protocol.Version10, [2]byte{}),
	}, "", nil)
	if err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}

	// A client that can accept only 4096 bytes.
	resp, reported := srv.MaximumMessageSize(sess, 4096)
	if resp.Type != protocol.AsyncMaximumMessageSizeResponse {
		t.Errorf("type = %v", resp.Type)
	}
	if resp.Length != 8 {
		t.Errorf("payload length = %d, want 8", resp.Length)
	}

	// The reply carries the server's own receive limit, not the smaller value and
	// not the client's.
	if reported != 8192 {
		t.Errorf("reported size = %d, want 8192, the server's receive limit", reported)
	}
	// The client's value became the transmit limit.
	if got := sess.MaxTxPayload(); got != 4096 {
		t.Errorf("MaxTxPayload() = %d, want 4096, the client's limit", got)
	}
	// The receive limit is untouched: the client said nothing about what it will
	// send.
	if got := sess.MaxRxPayload(); got != 8192 {
		t.Errorf("MaxRxPayload() = %d, want 8192", got)
	}
}

// TestMaximumMessageSizeIgnoresAbsurdValues covers the floor. IVI-6.1 says
// neither peer is obliged to accept a particular size, so a client may ask for
// something too small to be useful; reducing every response to single-byte packets
// serves nobody.
func TestMaximumMessageSizeIgnoresAbsurdValues(t *testing.T) {
	dev := testdevice.New()
	srv, err := server.New(dev, server.Config{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	sess, _, err := srv.Initialize(protocol.Header{
		Type:      protocol.Initialize,
		Parameter: protocol.EncodeInitializeParameter(protocol.Version10, [2]byte{}),
	}, "", nil)
	if err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}
	before := sess.MaxTxPayload()

	for _, tiny := range []uint64{0, 1, 16, 255} {
		srv.MaximumMessageSize(sess, tiny)
		if got := sess.MaxTxPayload(); got != before {
			t.Errorf("a request for %d changed the transmit limit to %d", tiny, got)
		}
	}
	// A reasonable value is honoured.
	srv.MaximumMessageSize(sess, 1024)
	if got := sess.MaxTxPayload(); got != 1024 {
		t.Errorf("MaxTxPayload() = %d, want 1024", got)
	}
}

// TestResponseHonoursLoweredMaximum is the second half of the libhislip finding,
// and the more damaging one. The response buffer was allocated once when the
// channel loop started and sized from the transmit limit at that moment. A client
// that lowered the limit afterwards left the buffer larger than the limit, so every
// subsequent response failed validation and the client received an error instead of
// its data.
//
// The test drives the server through Serve so that the buffer really is allocated
// by the loop, which is where the bug lived.
func TestResponseHonoursLoweredMaximum(t *testing.T) {
	// A response larger than the limit the client will ask for, so the server
	// must chunk it.
	payload := bytes.Repeat([]byte("A"), 3000)
	dev := testdevice.New(payload, payload)
	srv, err := server.New(dev, server.Config{
		MaxRxPayload: 8192,
		MaxTxPayload: 8192,
		Policy:       func() server.ResponsePolicy { return server.AlwaysReadPolicy{} },
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	c, cleanup := connect(t, srv)
	defer cleanup()

	// Before lowering the limit, the response arrives whole in one DataEnd.
	if got := len(c.query("READ?\n")); got != len(payload) {
		t.Fatalf("first response = %d bytes, want %d", got, len(payload))
	}

	// The client now lowers the maximum below the response size.
	const lowered uint64 = 1024
	var sizeBuf [8]byte
	for i := 0; i < 8; i++ {
		sizeBuf[i] = byte(lowered >> (8 * (7 - i)))
	}
	c.send(c.asyncConn, &c.asyncBuf, protocol.Header{
		Type:   protocol.AsyncMaximumMessageSize,
		Length: 8,
	}, sizeBuf[:])
	h, body := c.recv(c.asyncConn, &c.asyncBuf)
	if h.Type != protocol.AsyncMaximumMessageSizeResponse {
		t.Fatalf("response = %v, want AsyncMaximumMessageSizeResponse", h.Type)
	}
	if len(body) != 8 {
		t.Fatalf("response payload = %d bytes, want 8", len(body))
	}

	// Give the asynchronous channel a moment to apply it.
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if srv.Session(c.sessionID) != nil && srv.Session(c.sessionID).MaxTxPayload() == lowered {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if got := srv.Session(c.sessionID).MaxTxPayload(); got != lowered {
		t.Fatalf("MaxTxPayload() = %d, want %d", got, lowered)
	}

	// The next response must still arrive complete, chunked to the new limit.
	got := c.query("READ?\n")
	if len(got) != len(payload) {
		t.Errorf("second response = %d bytes, want %d", len(got), len(payload))
	}
	if got != string(payload) {
		t.Error("the second response did not match")
	}
}

// TestResponsePacketsRespectTheLimit confirms no packet exceeds the negotiated
// transmit limit, which is what a small device depends on.
func TestResponsePacketsRespectTheLimit(t *testing.T) {
	const limit = 512
	payload := bytes.Repeat([]byte("B"), 2000)
	dev := testdevice.New(payload)
	srv, err := server.New(dev, server.Config{
		MaxRxPayload: 4096,
		MaxTxPayload: 4096,
		Policy:       func() server.ResponsePolicy { return server.AlwaysReadPolicy{} },
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	syncStream := teststream.NewCanned(nil)
	asyncStream := teststream.NewCanned(nil)
	sess, _, err := srv.Initialize(protocol.Header{
		Type:      protocol.Initialize,
		Parameter: protocol.EncodeInitializeParameter(protocol.Version10, [2]byte{}),
	}, "", syncStream)
	if err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}
	if _, _, err := srv.AsyncInitialize(protocol.Header{
		Type:      protocol.AsyncInitialize,
		Parameter: uint32(sess.ID()),
	}, asyncStream); err != nil {
		t.Fatalf("AsyncInitialize() error = %v", err)
	}
	srv.MaximumMessageSize(sess, limit)
	syncStream.ResetOutput()

	tx := &server.Transaction{}
	if err := tx.CompleteInput(protocol.InitialMessageID); err != nil {
		t.Fatalf("CompleteInput() error = %v", err)
	}
	out, err := srv.SendResponse(context.Background(), sess, tx, server.Response{
		Sync:        server.NewMessageWriter(syncStream, protocol.ChannelSynchronous),
		Async:       server.NewMessageWriter(asyncStream, protocol.ChannelAsynchronous),
		MessageID:   protocol.InitialMessageID,
		Scratch:     make([]byte, limit),
		InputQueued: func() bool { return false },
	})
	if err != nil {
		t.Fatalf("SendResponse() error = %v", err)
	}
	if out != server.ResponseComplete {
		t.Fatalf("outcome = %v", out)
	}

	var assembled bytes.Buffer
	count := 0
	for _, m := range decodeAll(t, syncStream.Written()) {
		count++
		if m.H.Length > limit {
			t.Errorf("packet %d carried %d bytes, above the %d limit", count, m.H.Length, limit)
		}
		assembled.Write(m.Payload)
	}
	if count < 4 {
		t.Errorf("response arrived in %d packets; a 2000-byte response at a %d limit needs at least 4",
			count, limit)
	}
	if assembled.Len() != len(payload) {
		t.Errorf("assembled %d bytes, want %d", assembled.Len(), len(payload))
	}
}
