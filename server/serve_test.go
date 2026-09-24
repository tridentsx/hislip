// Copyright (c) 2026 The hislip developers. All rights reserved.
// Project site: https://github.com/tridentsx/hislip
// Use of this source code is governed by a MIT-style license that
// can be found in the LICENSE.txt file for the project.

package server_test

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/tridentsx/hislip/internal/testdevice"
	"github.com/tridentsx/hislip/internal/teststream"
	"github.com/tridentsx/hislip/protocol"
	"github.com/tridentsx/hislip/server"
)

// client is a minimal HiSLIP client, enough to drive the server through Serve.
// It is deliberately separate from anything the server uses, so that a shared
// misunderstanding cannot make both sides agree wrongly.
type client struct {
	t          *testing.T
	syncConn   *teststream.Conn
	asyncConn  *teststream.Conn
	syncBuf    [protocol.HeaderSize]byte
	asyncBuf   [protocol.HeaderSize]byte
	counter    *protocol.Counter
	sessionID  uint16
	rmtPending bool
}

func (c *client) send(conn *teststream.Conn, buf *[protocol.HeaderSize]byte, h protocol.Header, payload []byte) {
	c.t.Helper()
	if err := protocol.WriteMessage(conn, h, payload, buf); err != nil {
		c.t.Fatalf("client write %v: %v", h.Type, err)
	}
}

func (c *client) recv(conn *teststream.Conn, buf *[protocol.HeaderSize]byte) (protocol.Header, []byte) {
	c.t.Helper()
	h, err := protocol.ReadHeader(conn, buf)
	if err != nil {
		c.t.Fatalf("client read header: %v", err)
	}
	payload := make([]byte, h.Length)
	if h.Length > 0 {
		if err := protocol.ReadPayload(conn, h.Length, payload, nil); err != nil {
			c.t.Fatalf("client read payload: %v", err)
		}
	}
	return h, payload
}

// control returns the RMT-delivered flag for the next synchronous message, and
// clears it. A conforming client sets it once per terminator it has consumed.
func (c *client) control() uint8 {
	if c.rmtPending {
		c.rmtPending = false
		return protocol.ControlRMTDelivered
	}
	return 0
}

// query sends a program message and reads the response.
func (c *client) query(program string) string {
	c.t.Helper()
	id := c.counter.Take()
	c.send(c.syncConn, &c.syncBuf, protocol.Header{
		Type:      protocol.DataEnd,
		Control:   c.control(),
		Parameter: uint32(id),
	}, []byte(program))

	var out bytes.Buffer
	for {
		h, payload := c.recv(c.syncConn, &c.syncBuf)
		switch h.Type {
		case protocol.Data:
			out.Write(payload)
		case protocol.DataEnd:
			out.Write(payload)
			if h.MessageID() != id {
				c.t.Errorf("DataEnd MessageID = %#x, want %#x", uint32(h.MessageID()), uint32(id))
			}
			c.rmtPending = true
			return out.String()
		case protocol.Error:
			// Recorded and ignored; the DataEnd still follows.
		default:
			c.t.Fatalf("unexpected response %v", h.Type)
		}
	}
}

// command sends a program message expecting no response.
func (c *client) command(program string) {
	c.t.Helper()
	id := c.counter.Take()
	c.send(c.syncConn, &c.syncBuf, protocol.Header{
		Type:      protocol.DataEnd,
		Control:   c.control(),
		Parameter: uint32(id),
	}, []byte(program))
}

// connect runs both channels of a server and completes initialization.
func connect(t *testing.T, srv *server.Server) (*client, func()) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())

	clientSync, serverSync := teststream.Pipe()
	clientAsync, serverAsync := teststream.Pipe()

	syncDone := make(chan error, 1)
	asyncDone := make(chan error, 1)

	c := &client{t: t, syncConn: clientSync, asyncConn: clientAsync, counter: protocol.NewCounter()}

	go func() { syncDone <- srv.Serve(ctx, serverSync) }()

	c.send(clientSync, &c.syncBuf, protocol.Header{
		Type:      protocol.Initialize,
		Parameter: protocol.EncodeInitializeParameter(protocol.Version20, [2]byte{'G', 'T'}),
	}, []byte(protocol.DefaultSubAddress))

	h, _ := c.recv(clientSync, &c.syncBuf)
	if h.Type != protocol.InitializeResponse {
		t.Fatalf("first response = %v, want InitializeResponse", h.Type)
	}
	version, id := protocol.DecodeInitializeResponseParameter(h.Parameter)
	if version != protocol.Version10 {
		t.Errorf("negotiated version = %v, want 1.0", version)
	}
	if h.Control&protocol.ControlPreferOverlap != 0 {
		t.Error("server advertised an Overlap Mode preference")
	}
	c.sessionID = id

	go func() { asyncDone <- srv.Serve(ctx, serverAsync) }()

	c.send(clientAsync, &c.asyncBuf, protocol.Header{
		Type:      protocol.AsyncInitialize,
		Parameter: uint32(id),
	}, nil)
	ah, _ := c.recv(clientAsync, &c.asyncBuf)
	if ah.Type != protocol.AsyncInitializeResponse {
		t.Fatalf("async response = %v, want AsyncInitializeResponse", ah.Type)
	}

	cleanup := func() {
		cancel()
		_ = clientSync.Close()
		_ = clientAsync.Close()
		select {
		case <-syncDone:
		case <-time.After(2 * time.Second):
			t.Error("the synchronous channel did not end")
		}
		select {
		case <-asyncDone:
		case <-time.After(2 * time.Second):
			t.Error("the asynchronous channel did not end")
		}
	}
	return c, cleanup
}

// TestEndToEndQuery drives a full session over two concurrent connections, which
// is the shape a real client presents.
func TestEndToEndQuery(t *testing.T) {
	dev := testdevice.New(
		[]byte("SIGLENT,SDS1204X-HD,0123,1.2.3\n"),
		[]byte("12.3456\n"),
	)
	srv, err := server.New(dev, server.Config{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	c, cleanup := connect(t, srv)
	defer cleanup()

	if got := c.query("*IDN?\n"); got != "SIGLENT,SDS1204X-HD,0123,1.2.3\n" {
		t.Errorf("first query = %q", got)
	}
	if got := c.query("MEAS:VOLT?\n"); got != "12.3456\n" {
		t.Errorf("second query = %q", got)
	}
	if got := string(dev.Written()); got != "*IDN?\nMEAS:VOLT?\n" {
		t.Errorf("instrument received %q", got)
	}

	counters := srv.Counters()
	if counters.SessionsOpened != 1 {
		t.Errorf("SessionsOpened = %d, want 1", counters.SessionsOpened)
	}
	if counters.SilentInterrupted != 0 {
		t.Errorf("SilentInterrupted = %d; a conforming exchange declared an interrupted error",
			counters.SilentInterrupted)
	}
}

// TestEndToEndCommandDoesNotRead confirms the response policy keeps a command
// from waiting for a reply the instrument was never going to send.
func TestEndToEndCommandDoesNotRead(t *testing.T) {
	dev := testdevice.New()
	srv, err := server.New(dev, server.Config{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	c, cleanup := connect(t, srv)
	defer cleanup()

	c.command("*RST\n")
	c.command("SOUR:VOLT 3.3\n")

	// Give the server a moment to process, then confirm it wrote both and read
	// nothing.
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if string(dev.Written()) == "*RST\nSOUR:VOLT 3.3\n" {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if got := string(dev.Written()); got != "*RST\nSOUR:VOLT 3.3\n" {
		t.Errorf("instrument received %q", got)
	}
	if n := dev.Count(testdevice.OpRead); n != 0 {
		t.Errorf("the instrument was read %d times for commands with no query", n)
	}
}

// TestEndToEndStatusQueryWhileIdle exercises the asynchronous channel
// concurrently with the synchronous one.
func TestEndToEndStatusQuery(t *testing.T) {
	dev := testdevice.New([]byte("1\n"))
	dev.Status = 0x20
	srv, err := server.New(dev, server.Config{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	c, cleanup := connect(t, srv)
	defer cleanup()

	// A status query before any synchronous message uses 0xfffffefe.
	c.send(c.asyncConn, &c.asyncBuf, protocol.Header{
		Type:      protocol.AsyncStatusQuery,
		Parameter: uint32(protocol.StatusQueryInitialMessageID),
	}, nil)
	h, _ := c.recv(c.asyncConn, &c.asyncBuf)
	if h.Type != protocol.AsyncStatusResponse {
		t.Fatalf("response = %v, want AsyncStatusResponse", h.Type)
	}
	if h.Control&0x20 == 0 {
		t.Errorf("status = %#x; the device's ESB bit was dropped", h.Control)
	}
	if h.Control&server.StatusMAV != 0 {
		t.Errorf("status = %#x; MAV set with no response held", h.Control)
	}
	if srv.Counters().SerialPolls != 1 {
		t.Errorf("SerialPolls = %d, want 1", srv.Counters().SerialPolls)
	}
}

// TestEndToEndDeviceClear drives the four-message handshake across both channels
// in the order a client performs it.
func TestEndToEndDeviceClear(t *testing.T) {
	dev := testdevice.New()
	srv, err := server.New(dev, server.Config{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	c, cleanup := connect(t, srv)
	defer cleanup()

	// Step 1 and 2 on the asynchronous channel.
	c.send(c.asyncConn, &c.asyncBuf, protocol.Header{Type: protocol.AsyncDeviceClear}, nil)
	ack, _ := c.recv(c.asyncConn, &c.asyncBuf)
	if ack.Type != protocol.AsyncDeviceClearAcknowledge {
		t.Fatalf("response = %v, want AsyncDeviceClearAcknowledge", ack.Type)
	}
	if ack.Control&protocol.FeatureOverlapped != 0 {
		t.Error("the server proposed Overlap Mode")
	}

	// Step 3 and 4 on the synchronous channel. The client requests Overlap Mode
	// to confirm the server declines it.
	c.send(c.syncConn, &c.syncBuf, protocol.Header{
		Type:    protocol.DeviceClearComplete,
		Control: protocol.FeatureOverlapped,
	}, nil)
	done, _ := c.recv(c.syncConn, &c.syncBuf)
	if done.Type != protocol.DeviceClearAcknowledge {
		t.Fatalf("response = %v, want DeviceClearAcknowledge", done.Type)
	}
	if done.Control&protocol.FeatureOverlapped != 0 {
		t.Errorf("agreed bitmap = %#x; the server accepted Overlap Mode", done.Control)
	}
	if n := dev.Count(testdevice.OpClear); n != 1 {
		t.Errorf("device Clear called %d times, want 1", n)
	}

	// The MessageID counter resets on both sides, so the client starts again.
	c.counter.Reset()
	c.rmtPending = false
	if srv.Counters().DeviceClears != 1 {
		t.Errorf("DeviceClears = %d, want 1", srv.Counters().DeviceClears)
	}
}

// TestEndToEndAbsentResponse is §17.3 as a client experiences it: a query that
// produces nothing must still complete the read rather than hang.
func TestEndToEndAbsentResponse(t *testing.T) {
	dev := testdevice.New() // no responses queued
	srv, err := server.New(dev, server.Config{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	c, cleanup := connect(t, srv)
	defer cleanup()

	done := make(chan string, 1)
	go func() { done <- c.query("MEAS:VLOT?\n") }()

	select {
	case got := <-done:
		if got != "" {
			t.Errorf("response = %q, want empty", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the client's read never completed; §17.3 exists to prevent this")
	}
	if n := srv.Counters().QueriesWithoutAnswer; n != 1 {
		t.Errorf("QueriesWithoutAnswer = %d, want 1", n)
	}
}

// TestEndToEndTriggerIsOrdered confirms a Trigger reaches the instrument after a
// write that preceded it, which is the §47 ordering property.
func TestEndToEndTriggerIsOrdered(t *testing.T) {
	dev := testdevice.New()
	srv, err := server.New(dev, server.Config{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	c, cleanup := connect(t, srv)
	defer cleanup()

	c.command("INIT\n")
	id := c.counter.Take()
	c.send(c.syncConn, &c.syncBuf, protocol.Header{
		Type:      protocol.Trigger,
		Control:   c.control(),
		Parameter: uint32(id),
	}, nil)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if dev.Count(testdevice.OpTrigger) == 1 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	ops := dev.Ops()
	if len(ops) != 2 {
		t.Fatalf("device saw %v, want a write then a trigger", ops)
	}
	if ops[0] != testdevice.OpWrite || ops[1] != testdevice.OpTrigger {
		t.Errorf("device saw %v; the trigger overtook the write", ops)
	}
}

// TestEndToEndOversizedMessageIsRefusedNotFatal confirms an oversized payload is
// drained and answered, leaving the session usable.
func TestEndToEndOversizedMessage(t *testing.T) {
	dev := testdevice.New([]byte("ok\n"))
	srv, err := server.New(dev, server.Config{MaxRxPayload: 64, MaxTxPayload: 64})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	c, cleanup := connect(t, srv)
	defer cleanup()

	id := c.counter.Take()
	c.send(c.syncConn, &c.syncBuf, protocol.Header{
		Type:      protocol.DataEnd,
		Parameter: uint32(id),
	}, bytes.Repeat([]byte("x"), 200))

	h, _ := c.recv(c.syncConn, &c.syncBuf)
	if h.Type != protocol.Error {
		t.Fatalf("response = %v, want a non-fatal Error", h.Type)
	}
	if protocol.ErrorCode(h.Control) != protocol.NonFatalMessageTooLarge {
		t.Errorf("code = %d, want 4", h.Control)
	}
	if n := srv.Counters().OversizedMessages; n != 1 {
		t.Errorf("OversizedMessages = %d, want 1", n)
	}

	// The session survives, and the payload was drained rather than leaving the
	// stream desynchronised.
	if got := c.query("*IDN?\n"); got != "ok\n" {
		t.Errorf("the session did not recover: query returned %q", got)
	}
}

// TestEndToEndSecondClientRefused confirms a second session is refused with a
// diagnosable fatal error rather than stalled.
func TestEndToEndSecondClientRefused(t *testing.T) {
	dev := testdevice.New()
	srv, err := server.New(dev, server.Config{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	_, cleanup := connect(t, srv)
	defer cleanup()

	ctx := context.Background()
	clientSync, serverSync := teststream.Pipe()
	go func() { _ = srv.Serve(ctx, serverSync) }()

	var buf [protocol.HeaderSize]byte
	if err := protocol.WriteMessage(clientSync, protocol.Header{
		Type:      protocol.Initialize,
		Parameter: protocol.EncodeInitializeParameter(protocol.Version10, [2]byte{}),
	}, []byte(protocol.DefaultSubAddress), &buf); err != nil {
		t.Fatalf("WriteMessage() error = %v", err)
	}

	h, err := protocol.ReadHeader(clientSync, &buf)
	if err != nil {
		t.Fatalf("ReadHeader() error = %v", err)
	}
	if h.Type != protocol.FatalError {
		t.Fatalf("response = %v, want FatalError", h.Type)
	}
	if protocol.FatalErrorCode(h.Control) != protocol.FatalMaxClientsExceeded {
		t.Errorf("code = %d, want 4", h.Control)
	}
	if n := srv.Counters().SessionsRefused; n != 1 {
		t.Errorf("SessionsRefused = %d, want 1", n)
	}
}

// TestServeRejectsWrongFirstMessage confirms a connection that does not begin
// with an initialization message is told why rather than left hanging.
func TestServeRejectsWrongFirstMessage(t *testing.T) {
	dev := testdevice.New()
	srv, err := server.New(dev, server.Config{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	clientEnd, serverEnd := teststream.Pipe()
	done := make(chan error, 1)
	go func() { done <- srv.Serve(context.Background(), serverEnd) }()

	var buf [protocol.HeaderSize]byte
	if err := protocol.WriteMessage(clientEnd, protocol.Header{Type: protocol.Data}, nil, &buf); err != nil {
		t.Fatalf("WriteMessage() error = %v", err)
	}
	h, err := protocol.ReadHeader(clientEnd, &buf)
	if err != nil {
		t.Fatalf("ReadHeader() error = %v", err)
	}
	if h.Type != protocol.FatalError {
		t.Errorf("response = %v, want FatalError", h.Type)
	}
	if protocol.FatalErrorCode(h.Control) != protocol.FatalInvalidInitSequence {
		t.Errorf("code = %d, want 3", h.Control)
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Error("Serve did not return")
	}
}
