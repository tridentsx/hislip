// Copyright (c) 2026 The hislip developers. All rights reserved.
// Project site: https://github.com/tridentsx/hislip
// Use of this source code is governed by a MIT-style license that
// can be found in the LICENSE.txt file for the project.

// Package server_test holds the server tests that need the recording mock
// device. internal/testdevice imports the server package, so these cannot live
// in-package without an import cycle.
package server_test

import (
	"testing"

	"github.com/tridentsx/hislip/internal/testdevice"
	"github.com/tridentsx/hislip/internal/teststream"
	"github.com/tridentsx/hislip/protocol"
	"github.com/tridentsx/hislip/server"
)

// encode writes a message into a byte slice the way a client would put it on the
// wire.
func encode(t *testing.T, h protocol.Header, payload []byte) []byte {
	t.Helper()
	var buf [protocol.HeaderSize]byte
	stream := teststream.NewCanned(nil)
	if err := protocol.WriteMessage(stream, h, payload, &buf); err != nil {
		t.Fatalf("WriteMessage() error = %v", err)
	}
	return stream.Written()
}

// TestInitializationOverTheWire drives the two-channel handshake through real
// encoded bytes rather than through Header values, so that a field-placement
// error in the response is caught here rather than during interoperability
// testing.
func TestInitializationOverTheWire(t *testing.T) {
	dev := testdevice.New()
	srv, err := server.New(dev, server.Config{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	// The client opens the synchronous channel and sends Initialize with the
	// conventional sub-address.
	const sub = protocol.DefaultSubAddress
	syncBytes := encode(t, protocol.Header{
		Type:      protocol.Initialize,
		Parameter: protocol.EncodeInitializeParameter(protocol.Version20, [2]byte{'G', 'T'}),
	}, []byte(sub))

	syncStream := teststream.NewCanned(syncBytes)
	// Force short reads, so the test proves the header is reassembled rather
	// than assuming one Read returns all sixteen bytes. Real sockets do this.
	syncStream.MaxRead = 5

	var buf [protocol.HeaderSize]byte
	h, err := protocol.ReadHeader(syncStream, &buf)
	if err != nil {
		t.Fatalf("ReadHeader() error = %v", err)
	}
	if h.Type != protocol.Initialize {
		t.Fatalf("first message type = %v, want Initialize", h.Type)
	}

	// The sub-address is the payload, streamed through a fixed scratch buffer.
	scratch := make([]byte, 16)
	var gotSub []byte
	if err := protocol.ReadPayload(syncStream, h.Length, scratch, func(b []byte) error {
		gotSub = append(gotSub, b...)
		return nil
	}); err != nil {
		t.Fatalf("ReadPayload() error = %v", err)
	}
	if string(gotSub) != sub {
		t.Fatalf("sub-address = %q, want %q", gotSub, sub)
	}

	sess, resp, err := srv.Initialize(h, string(gotSub), syncStream)
	if err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}
	syncStream.ResetOutput()
	if err := protocol.WriteMessage(syncStream, resp, nil, &buf); err != nil {
		t.Fatalf("WriteMessage() error = %v", err)
	}

	// Decode the response as a client would.
	clientView, err := protocol.DecodeHeader(syncStream.Written())
	if err != nil {
		t.Fatalf("DecodeHeader() error = %v", err)
	}
	if clientView.Type != protocol.InitializeResponse {
		t.Errorf("response type = %v", clientView.Type)
	}
	if clientView.Control&protocol.ControlPreferOverlap != 0 {
		t.Error("response advertises Overlap Mode preference")
	}
	version, sessionID := protocol.DecodeInitializeResponseParameter(clientView.Parameter)
	if version != protocol.Version10 {
		t.Errorf("negotiated version = %v, want 1.0", version)
	}
	if sessionID != sess.ID() {
		t.Errorf("session ID on the wire = %d, want %d", sessionID, sess.ID())
	}
	if clientView.Length != 0 {
		t.Errorf("response payload length = %d, want 0", clientView.Length)
	}

	// The client now opens the asynchronous channel, quoting the session ID it
	// just learned.
	asyncBytes := encode(t, protocol.Header{
		Type:      protocol.AsyncInitialize,
		Parameter: uint32(sessionID),
	}, nil)
	asyncStream := teststream.NewCanned(asyncBytes)

	ah, err := protocol.ReadHeader(asyncStream, &buf)
	if err != nil {
		t.Fatalf("ReadHeader() on async error = %v", err)
	}
	paired, aresp, err := srv.AsyncInitialize(ah, asyncStream)
	if err != nil {
		t.Fatalf("AsyncInitialize() error = %v", err)
	}
	if paired != sess {
		t.Error("AsyncInitialize paired a different session")
	}
	if aresp.Type != protocol.AsyncInitializeResponse {
		t.Errorf("async response type = %v", aresp.Type)
	}
	if !sess.Ready() {
		t.Fatal("session not ready after both channels")
	}

	// Nothing should have reached the instrument during initialization.
	if ops := dev.Ops(); len(ops) != 0 {
		t.Errorf("device saw %v during initialization, want nothing", ops)
	}
}

// TestOperationsRefusedBeforePairing confirms the fatal-error-2 guard at the
// level a client would experience it, and that the instrument is left untouched.
func TestOperationsRefusedBeforePairing(t *testing.T) {
	dev := testdevice.New()
	srv, err := server.New(dev, server.Config{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	sess, _, err := srv.Initialize(protocol.Header{
		Type:      protocol.Initialize,
		Parameter: protocol.EncodeInitializeParameter(protocol.Version10, [2]byte{}),
	}, "", teststream.NewCanned(nil))
	if err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}

	for _, typ := range []protocol.MessageType{protocol.Data, protocol.DataEnd, protocol.Trigger} {
		_, err := srv.Accept(sess, protocol.Header{Type: typ}, protocol.ChannelSynchronous)
		wire := server.WireErrorFor(err)
		if !wire.Fatal || wire.FatalCode != protocol.FatalChannelsNotEstablished {
			t.Errorf("%v before pairing: WireErrorFor() = %+v, want fatal code 2", typ, wire)
		}
	}
	if ops := dev.Ops(); len(ops) != 0 {
		t.Errorf("device saw %v, want nothing before the session was ready", ops)
	}
}

// TestPipeCarriesAHandshake uses the blocking pipe rather than canned bytes, the
// shape a concurrent client and server will need.
func TestPipeCarriesAHandshake(t *testing.T) {
	clientEnd, serverEnd := teststream.Pipe()

	var buf [protocol.HeaderSize]byte
	go func() {
		h := protocol.Header{
			Type:      protocol.Initialize,
			Parameter: protocol.EncodeInitializeParameter(protocol.Version10, [2]byte{'G', 'T'}),
		}
		if err := protocol.WriteMessage(clientEnd, h, []byte(protocol.DefaultSubAddress), &buf); err != nil {
			t.Errorf("client WriteMessage() error = %v", err)
		}
	}()

	var serverBuf [protocol.HeaderSize]byte
	h, err := protocol.ReadHeader(serverEnd, &serverBuf)
	if err != nil {
		t.Fatalf("ReadHeader() error = %v", err)
	}
	if h.Type != protocol.Initialize {
		t.Errorf("type = %v, want Initialize", h.Type)
	}
	if h.Length != uint64(len(protocol.DefaultSubAddress)) {
		t.Errorf("length = %d, want %d", h.Length, len(protocol.DefaultSubAddress))
	}
}
