// Copyright (c) 2026 The hislip developers. All rights reserved.
// Project site: https://github.com/gotmc/hislip
// Use of this source code is governed by a MIT-style license that
// can be found in the LICENSE.txt file for the project.

package server_test

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/gotmc/hislip/internal/testdevice"
	"github.com/gotmc/hislip/internal/teststream"
	"github.com/gotmc/hislip/protocol"
	"github.com/gotmc/hislip/server"
)

// fixture is a server with a paired session and writers on both channels.
type fixture struct {
	srv   *server.Server
	sess  *server.Session
	dev   *testdevice.Device
	tx    *server.Transaction
	sync  *teststream.Canned
	async *teststream.Canned
	syncW *server.MessageWriter
	asynW *server.MessageWriter
}

func newFixture(t *testing.T, responses ...[]byte) *fixture {
	t.Helper()
	dev := testdevice.New(responses...)
	srv, err := server.New(dev, server.Config{})
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
	syncStream.ResetOutput()
	asyncStream.ResetOutput()

	return &fixture{
		srv:   srv,
		sess:  sess,
		dev:   dev,
		tx:    &server.Transaction{},
		sync:  syncStream,
		async: asyncStream,
		syncW: server.NewMessageWriter(syncStream, protocol.ChannelSynchronous),
		asynW: server.NewMessageWriter(asyncStream, protocol.ChannelAsynchronous),
	}
}

// response builds the Response parameters, with no input queued by default.
func (f *fixture) response(id protocol.MessageID, scratch int) server.Response {
	return server.Response{
		Sync:        f.syncW,
		Async:       f.asynW,
		MessageID:   id,
		Scratch:     make([]byte, scratch),
		InputQueued: func() bool { return false },
	}
}

// decodeAll splits a byte stream into messages.
func decodeAll(t *testing.T, b []byte) []struct {
	H       protocol.Header
	Payload []byte
} {
	t.Helper()
	var out []struct {
		H       protocol.Header
		Payload []byte
	}
	for len(b) > 0 {
		h, err := protocol.DecodeHeader(b)
		if err != nil {
			t.Fatalf("DecodeHeader() error = %v, remaining % x", err, b)
		}
		b = b[protocol.HeaderSize:]
		if uint64(len(b)) < h.Length {
			t.Fatalf("truncated payload: have %d, want %d", len(b), h.Length)
		}
		out = append(out, struct {
			H       protocol.Header
			Payload []byte
		}{h, append([]byte(nil), b[:h.Length]...)})
		b = b[h.Length:]
	}
	return out
}

// TestQueryRoundTrip is the core of the server: a program message in, a chunked
// response out, with the MessageID correlation and RMT state correct throughout.
func TestQueryRoundTrip(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t, []byte("SIGLENT,SDS1204X-HD,0123,1.2.3\n"))
	const id = protocol.InitialMessageID

	// The client sends a complete program message as a single DataEnd.
	interrupted, err := f.srv.ReceiveData(ctx, f.sess, f.tx, protocol.Header{
		Type:      protocol.DataEnd,
		Parameter: uint32(id),
		Length:    6,
	}, []byte("*IDN?\n"))
	if err != nil {
		t.Fatalf("ReceiveData() error = %v", err)
	}
	if interrupted != server.NotInterrupted {
		t.Errorf("interrupted = %v on a clean first message", interrupted)
	}
	if got := string(f.dev.Written()); got != "*IDN?\n" {
		t.Errorf("instrument received %q", got)
	}

	// The response is streamed through an 8-byte scratch buffer, so a 30-byte
	// response must arrive as several Data packets and one DataEnd.
	out, err := f.srv.SendResponse(ctx, f.sess, f.tx, f.response(id, 8))
	if err != nil {
		t.Fatalf("SendResponse() error = %v", err)
	}
	if out != server.ResponseComplete {
		t.Fatalf("outcome = %v, want ResponseComplete", out)
	}

	msgs := decodeAll(t, f.sync.Written())
	if len(msgs) < 2 {
		t.Fatalf("got %d messages, want several Data plus a DataEnd", len(msgs))
	}

	var assembled bytes.Buffer
	for i, m := range msgs {
		last := i == len(msgs)-1
		wantType := protocol.Data
		if last {
			wantType = protocol.DataEnd
		}
		if m.H.Type != wantType {
			t.Errorf("message %d type = %v, want %v", i, m.H.Type, wantType)
		}
		if m.H.MessageID() != id {
			t.Errorf("message %d MessageID = %#x, want %#x", i, uint32(m.H.MessageID()), uint32(id))
		}
		if !last && m.H.Length > 8 {
			t.Errorf("message %d payload %d bytes, exceeds the scratch size", i, m.H.Length)
		}
		assembled.Write(m.Payload)
	}
	if got := assembled.String(); got != "SIGLENT,SDS1204X-HD,0123,1.2.3\n" {
		t.Errorf("assembled response = %q", got)
	}

	// The terminator was delivered, so RMT-expected is now set and the next
	// client message must acknowledge it.
	if !f.sess.RMT().Expected() {
		t.Error("RMT-expected not set after DataEnd")
	}
	if got := f.sess.LastTxMessageID(); got != id {
		t.Errorf("LastTxMessageID() = %#x, want %#x", uint32(got), uint32(id))
	}
	if f.tx.State() != server.TxIdle {
		t.Errorf("transaction state = %v, want TxIdle", f.tx.State())
	}
	if f.sess.Status().ResponsePending() {
		t.Error("ResponsePending() still set after the response completed")
	}
}

// TestInterruptedSuppressesTheTerminator is the §11.3.2 path. Client input queued
// when the terminator is offered means the response is void: it must be discarded
// and both Interrupted messages sent.
func TestInterruptedSuppressesTheTerminator(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t, []byte("12.345\n"))
	const id = protocol.InitialMessageID

	if _, err := f.srv.ReceiveData(ctx, f.sess, f.tx, protocol.Header{
		Type:      protocol.DataEnd,
		Parameter: uint32(id),
	}, []byte("MEAS:VOLT?\n")); err != nil {
		t.Fatalf("ReceiveData() error = %v", err)
	}

	r := f.response(id, 64)
	r.InputQueued = func() bool { return true }

	out, err := f.srv.SendResponse(ctx, f.sess, f.tx, r)
	if err != nil {
		t.Fatalf("SendResponse() error = %v", err)
	}
	if out != server.ResponseInterrupted {
		t.Fatalf("outcome = %v, want ResponseInterrupted", out)
	}

	// No DataEnd may have been written, and therefore RMT-expected must not be
	// set: the server did not deliver a terminator.
	for _, m := range decodeAll(t, f.sync.Written()) {
		if m.H.Type == protocol.DataEnd {
			t.Error("a DataEnd was written despite the interrupted condition")
		}
	}
	if f.sess.RMT().Expected() {
		t.Error("RMT-expected set although no terminator was delivered")
	}

	// Both halves of the transaction must have been sent, each carrying the
	// interrupting MessageID.
	syncMsgs := decodeAll(t, f.sync.Written())
	asyncMsgs := decodeAll(t, f.async.Written())
	if len(asyncMsgs) != 1 || asyncMsgs[0].H.Type != protocol.AsyncInterrupted {
		t.Fatalf("async channel got %v, want one AsyncInterrupted", asyncMsgs)
	}
	last := syncMsgs[len(syncMsgs)-1]
	if last.H.Type != protocol.Interrupted {
		t.Errorf("sync channel ended with %v, want Interrupted", last.H.Type)
	}
	for _, m := range []protocol.Header{asyncMsgs[0].H, last.H} {
		if m.MessageID() != id {
			t.Errorf("%v carried MessageID %#x, want the interrupting %#x",
				m.Type, uint32(m.MessageID()), uint32(id))
		}
		if m.Control != 0 {
			t.Errorf("%v control code = %#x, want 0", m.Type, m.Control)
		}
	}
	if f.sess.Status().ResponsePending() {
		t.Error("ResponsePending() still set; the discarded response must not be retained")
	}
}

// TestAbsentResponse is §17.3, the most common real-world failure: a query that
// produces nothing. The cause must be reported and the transaction terminated so
// that a conforming client's read completes.
func TestAbsentResponse(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t) // no responses queued
	const id = protocol.InitialMessageID

	if _, err := f.srv.ReceiveData(ctx, f.sess, f.tx, protocol.Header{
		Type:      protocol.DataEnd,
		Parameter: uint32(id),
	}, []byte("MEAS:VLOT?\n")); err != nil {
		t.Fatalf("ReceiveData() error = %v", err)
	}

	out, err := f.srv.SendResponse(ctx, f.sess, f.tx, f.response(id, 32))
	if err != nil {
		t.Fatalf("SendResponse() error = %v", err)
	}
	if out != server.ResponseAbsent {
		t.Fatalf("outcome = %v, want ResponseAbsent", out)
	}

	msgs := decodeAll(t, f.sync.Written())
	if len(msgs) != 2 {
		t.Fatalf("got %d messages, want an Error then a DataEnd: %+v", len(msgs), msgs)
	}
	if msgs[0].H.Type != protocol.Error {
		t.Errorf("first message = %v, want Error", msgs[0].H.Type)
	}
	// The code must be device-defined, not in the range reserved for HiSLIP
	// extensions.
	if code := protocol.ErrorCode(msgs[0].H.Control); code < protocol.NonFatalDeviceDefinedMin {
		t.Errorf("error code = %d, want a device-defined code", code)
	}
	if len(msgs[0].Payload) == 0 {
		t.Error("the Error carries no description")
	}
	if msgs[1].H.Type != protocol.DataEnd {
		t.Errorf("second message = %v, want DataEnd", msgs[1].H.Type)
	}
	if msgs[1].H.Length != 0 {
		t.Errorf("terminating DataEnd length = %d, want 0", msgs[1].H.Length)
	}
	if msgs[1].H.MessageID() != id {
		t.Errorf("terminating DataEnd MessageID = %#x, want %#x",
			uint32(msgs[1].H.MessageID()), uint32(id))
	}
	// The session survives; a mistyped query is an operation error, not a
	// session failure.
	if !f.sess.Ready() {
		t.Error("the session is no longer ready after an absent response")
	}
}

// failAfterReads wraps a device so that Read succeeds a fixed number of times and
// then fails, which is how a GPIB read that dies partway through presents.
type failAfterReads struct {
	*testdevice.Device
	ok  int
	err error
}

func (d *failAfterReads) Read(ctx context.Context, p []byte) (int, bool, error) {
	if d.ok == 0 {
		return 0, false, d.err
	}
	d.ok--
	return d.Device.Read(ctx, p)
}

// TestTruncatedResponseIsDelivered covers R-DEV-011: bytes that did arrive are
// delivered rather than discarded, the truncation is reported, and the
// transaction is still terminated so the client's read completes.
func TestTruncatedResponseIsDelivered(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t, []byte("0123456789ABCDEF"))
	const id = protocol.InitialMessageID

	if _, err := f.srv.ReceiveData(ctx, f.sess, f.tx, protocol.Header{
		Type:      protocol.DataEnd,
		Parameter: uint32(id),
	}, []byte("CURV?\n")); err != nil {
		t.Fatalf("ReceiveData() error = %v", err)
	}

	// Rebuild the server over a device that yields two 4-byte chunks and then
	// fails, so eight of the sixteen bytes arrive.
	failing := &failAfterReads{
		Device: f.dev,
		ok:     2,
		err:    errors.New("gpib handshake timeout"),
	}
	srv, err := server.New(failing, server.Config{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	sess, _, err := srv.Initialize(protocol.Header{
		Type:      protocol.Initialize,
		Parameter: protocol.EncodeInitializeParameter(protocol.Version10, [2]byte{}),
	}, "", f.sync)
	if err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}
	if _, _, err := srv.AsyncInitialize(protocol.Header{
		Type:      protocol.AsyncInitialize,
		Parameter: uint32(sess.ID()),
	}, f.async); err != nil {
		t.Fatalf("AsyncInitialize() error = %v", err)
	}
	tx := &server.Transaction{}
	if err := tx.CompleteInput(id); err != nil {
		t.Fatalf("CompleteInput() error = %v", err)
	}
	f.sync.ResetOutput()

	out, err := srv.SendResponse(ctx, sess, tx, server.Response{
		Sync:        f.syncW,
		Async:       f.asynW,
		MessageID:   id,
		Scratch:     make([]byte, 4),
		InputQueued: func() bool { return false },
	})
	if err != nil {
		t.Fatalf("SendResponse() error = %v", err)
	}
	if out != server.ResponseTruncated {
		t.Fatalf("outcome = %v, want ResponseTruncated", out)
	}

	msgs := decodeAll(t, f.sync.Written())
	var assembled bytes.Buffer
	sawError := false
	for _, m := range msgs {
		switch m.H.Type {
		case protocol.Data:
			assembled.Write(m.Payload)
		case protocol.Error:
			sawError = true
			if code := protocol.ErrorCode(m.H.Control); code < protocol.NonFatalDeviceDefinedMin {
				t.Errorf("error code = %d, want a device-defined code", code)
			}
		case protocol.DataEnd:
			assembled.Write(m.Payload)
		}
	}
	// The bytes that did arrive are delivered, not thrown away.
	if got := assembled.String(); got != "01234567" {
		t.Errorf("delivered %q, want the eight bytes that arrived", got)
	}
	if !sawError {
		t.Error("the truncation was not reported")
	}
	// The transaction is still terminated, so the client's read completes.
	if last := msgs[len(msgs)-1]; last.H.Type != protocol.DataEnd {
		t.Errorf("stream ended with %v, want DataEnd", last.H.Type)
	}
	if tx.State() != server.TxIdle {
		t.Errorf("transaction state = %v, want TxIdle", tx.State())
	}
	if !sess.Ready() {
		t.Error("the session failed; a backend timeout is an operation error")
	}
}

// TestRMTMismatchIsReportedButSilent confirms the silent interrupted error
// reaches the caller for counting without producing any wire traffic.
func TestRMTMismatchIsReportedButSilent(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t, []byte("1\n"), []byte("2\n"))

	first := protocol.InitialMessageID
	if _, err := f.srv.ReceiveData(ctx, f.sess, f.tx, protocol.Header{
		Type:      protocol.DataEnd,
		Parameter: uint32(first),
	}, []byte("A?\n")); err != nil {
		t.Fatalf("ReceiveData() error = %v", err)
	}
	if _, err := f.srv.SendResponse(ctx, f.sess, f.tx, f.response(first, 32)); err != nil {
		t.Fatalf("SendResponse() error = %v", err)
	}
	f.sync.ResetOutput()

	// The client now sends another query without acknowledging the terminator.
	second := first.Next()
	interrupted, err := f.srv.ReceiveData(ctx, f.sess, f.tx, protocol.Header{
		Type:      protocol.DataEnd,
		Parameter: uint32(second),
		Control:   0, // RMT-delivered clear, which is the mistake
	}, []byte("B?\n"))
	if err != nil {
		t.Fatalf("ReceiveData() error = %v", err)
	}
	if interrupted != server.InterruptedRMTMismatch {
		t.Fatalf("interrupted = %v, want InterruptedRMTMismatch", interrupted)
	}
	if interrupted.Notifies() {
		t.Error("the RMT mismatch claims to notify the client; it must be silent")
	}
	if n := len(f.sync.Written()); n != 0 {
		t.Errorf("%d bytes were written for a silent interrupted error", n)
	}
	if n := len(f.async.Written()); n != 0 {
		t.Errorf("%d bytes written on the async channel for a silent error", n)
	}
}

// TestPartialInputThenTerminator covers a program message split across Data and
// DataEnd, which is how a long command arrives.
func TestPartialInputThenTerminator(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	id := protocol.InitialMessageID

	if _, err := f.srv.ReceiveData(ctx, f.sess, f.tx, protocol.Header{
		Type:      protocol.Data,
		Parameter: uint32(id),
	}, []byte("SOUR:VOLT ")); err != nil {
		t.Fatalf("first ReceiveData() error = %v", err)
	}
	if f.tx.State() != server.TxReceivingInput {
		t.Errorf("state = %v, want TxReceivingInput", f.tx.State())
	}

	id = id.Next()
	if _, err := f.srv.ReceiveData(ctx, f.sess, f.tx, protocol.Header{
		Type:      protocol.DataEnd,
		Parameter: uint32(id),
	}, []byte("3.3\n")); err != nil {
		t.Fatalf("second ReceiveData() error = %v", err)
	}
	if f.tx.State() != server.TxInputComplete {
		t.Errorf("state = %v, want TxInputComplete", f.tx.State())
	}
	if got := string(f.dev.Written()); got != "SOUR:VOLT 3.3\n" {
		t.Errorf("instrument received %q", got)
	}

	// Only the final write carries the end flag, which for a GPIB backend is
	// what asserts EOI on the last byte.
	calls := f.dev.Calls()
	if len(calls) != 2 {
		t.Fatalf("device saw %d writes, want 2", len(calls))
	}
	if calls[0].End {
		t.Error("the Data write carried end true")
	}
	if !calls[1].End {
		t.Error("the DataEnd write carried end false")
	}
}

// TestEmptyDataEndStillTerminates covers the rule that a DataEnd with no payload
// terminates the logical input message.
func TestEmptyDataEndStillTerminates(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	if _, err := f.srv.ReceiveData(ctx, f.sess, f.tx, protocol.Header{
		Type:      protocol.DataEnd,
		Parameter: uint32(protocol.InitialMessageID),
	}, nil); err != nil {
		t.Fatalf("ReceiveData() error = %v", err)
	}
	calls := f.dev.Calls()
	if len(calls) != 1 {
		t.Fatalf("device saw %d writes, want 1", len(calls))
	}
	if !calls[0].End {
		t.Error("an empty DataEnd did not terminate the input message")
	}
}

func TestResponseRequiresInputQueuedPredicate(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t, []byte("x"))
	r := f.response(protocol.InitialMessageID, 16)
	r.InputQueued = nil

	_, err := f.srv.SendResponse(ctx, f.sess, f.tx, r)
	if !errors.Is(err, server.ErrInputQueuedRequired) {
		t.Errorf("error = %v, want ErrInputQueuedRequired", err)
	}
}

func TestResponseRejectsOversizedScratch(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t, []byte("x"))
	r := f.response(protocol.InitialMessageID, int(f.sess.MaxTxPayload())+1)

	_, err := f.srv.SendResponse(ctx, f.sess, f.tx, r)
	if !errors.Is(err, protocol.ErrMessageTooLarge) {
		t.Errorf("error = %v, want ErrMessageTooLarge", err)
	}
}

func TestResponseRejectsMissingWriters(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t, []byte("x"))
	r := f.response(protocol.InitialMessageID, 16)
	r.Async = nil

	// The async writer is needed only for the Interrupted transaction, but its
	// absence is refused up front: discovering it at the moment an interrupted
	// error occurs would mean failing exactly when correct behaviour matters
	// most.
	if _, err := f.srv.SendResponse(ctx, f.sess, f.tx, r); !errors.Is(err, server.ErrInvalidConfig) {
		t.Errorf("error = %v, want ErrInvalidConfig", err)
	}
}

// TestWriterRejectsWrongChannel guards against sending a response on the wrong
// connection, which a client diagnoses only as an unrelated session failure.
func TestWriterRejectsWrongChannel(t *testing.T) {
	stream := teststream.NewCanned(nil)
	w := server.NewMessageWriter(stream, protocol.ChannelSynchronous)
	err := w.Write(protocol.Header{Type: protocol.AsyncStatusResponse}, nil)
	if !errors.Is(err, protocol.ErrWrongChannel) {
		t.Errorf("error = %v, want ErrWrongChannel", err)
	}
	if len(stream.Written()) != 0 {
		t.Error("bytes were written for a rejected message")
	}
}

func TestWriterCountsBytes(t *testing.T) {
	stream := teststream.NewCanned(nil)
	w := server.NewMessageWriter(stream, protocol.ChannelSynchronous)
	if err := w.Write(protocol.Header{Type: protocol.Data}, []byte("abcd")); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if got := w.Bytes(); got != protocol.HeaderSize+4 {
		t.Errorf("Bytes() = %d, want %d", got, protocol.HeaderSize+4)
	}
	if w.Channel() != protocol.ChannelSynchronous {
		t.Error("Channel() mismatch")
	}
}
