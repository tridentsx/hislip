// Copyright (c) 2026 The hislip developers. All rights reserved.
// Project site: https://github.com/tridentsx/hislip
// Use of this source code is governed by a MIT-style license that
// can be found in the LICENSE.txt file for the project.

package server

import (
	"errors"
	"testing"

	"github.com/tridentsx/hislip/protocol"
)

func initializeHeader(v protocol.Version, sub string) protocol.Header {
	return protocol.Header{
		Type:      protocol.Initialize,
		Parameter: protocol.EncodeInitializeParameter(v, [2]byte{'G', 'T'}),
		Length:    uint64(len(sub)),
	}
}

func TestInitializeHappyPath(t *testing.T) {
	srv := newTestServer(t)
	sess, resp, err := srv.Initialize(
		initializeHeader(protocol.Version10, protocol.DefaultSubAddress),
		protocol.DefaultSubAddress,
		nil,
	)
	if err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}
	if resp.Type != protocol.InitializeResponse {
		t.Errorf("response type = %v, want InitializeResponse", resp.Type)
	}
	gotVer, gotID := protocol.DecodeInitializeResponseParameter(resp.Parameter)
	if gotVer != protocol.Version10 {
		t.Errorf("negotiated version = %v, want 1.0", gotVer)
	}
	if gotID != sess.ID() {
		t.Errorf("response session ID = %d, want %d", gotID, sess.ID())
	}
	if sess.State() != StatePairing {
		t.Errorf("state after Initialize = %v, want StatePairing", sess.State())
	}
	if sess.Ready() {
		t.Error("Ready() = true with only one channel")
	}
}

// TestInitializeResponsePrefersSynchronized checks the advertisement half of the
// Overlap Mode prohibition. Control code bit 0 clear means the server prefers
// Synchronized Mode.
func TestInitializeResponsePrefersSynchronized(t *testing.T) {
	srv := newTestServer(t)
	_, resp, err := srv.Initialize(initializeHeader(protocol.Version20, ""), "", nil)
	if err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}
	if resp.Control&protocol.ControlPreferOverlap != 0 {
		t.Errorf("control code = %#x; bit 0 set advertises Overlap Mode", resp.Control)
	}
}

// TestInitializeNegotiatesDownward covers the firmware profile: the codec knows
// the 2.0 namespace but the server advertises 1.0, so a 2.0 client must end up on
// 1.0 rather than being refused.
func TestInitializeNegotiatesDownward(t *testing.T) {
	srv := newTestServer(t)
	_, resp, err := srv.Initialize(initializeHeader(protocol.Version20, ""), "", nil)
	if err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}
	gotVer, _ := protocol.DecodeInitializeResponseParameter(resp.Parameter)
	if gotVer != protocol.Version10 {
		t.Errorf("negotiated version = %v, want 1.0", gotVer)
	}
}

func TestInitializeSubAddresses(t *testing.T) {
	srv := newTestServer(t)
	tests := []struct {
		sub     string
		wantErr bool
	}{
		{protocol.DefaultSubAddress, false},
		{"", false}, // a null sub-address selects the default instrument
		{"hislip1", true},
		{"HISLIP0", true}, // case-sensitive
		{"inst0", true},   // the VXI-11 name is not ours
	}
	for _, tc := range tests {
		t.Run("sub="+tc.sub, func(t *testing.T) {
			s, _, err := srv.Initialize(initializeHeader(protocol.Version10, tc.sub), tc.sub, nil)
			if tc.wantErr {
				if !errors.Is(err, ErrInvalidSubAddress) {
					t.Errorf("error = %v, want ErrInvalidSubAddress", err)
				}
				if got := WireErrorFor(err); !got.Fatal ||
					got.FatalCode != protocol.FatalInvalidInitSequence {
					t.Errorf("WireErrorFor() = %+v, want fatal code 3", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("error = %v, want nil", err)
			}
			if err := srv.removeSession(s); err != nil {
				t.Fatalf("removeSession() error = %v", err)
			}
		})
	}
}

func TestInitializeRejectsWrongFirstMessage(t *testing.T) {
	srv := newTestServer(t)
	_, _, err := srv.Initialize(protocol.Header{Type: protocol.Data}, "", nil)
	if !errors.Is(err, ErrInvalidInitSequence) {
		t.Errorf("error = %v, want ErrInvalidInitSequence", err)
	}
	if got := WireErrorFor(err); !got.Fatal || got.FatalCode != protocol.FatalInvalidInitSequence {
		t.Errorf("WireErrorFor() = %+v, want fatal code 3", got)
	}
}

// TestSecondSessionRefused covers R-FW-042. A user connecting from a second
// machine must get a diagnosable failure, not a stall.
func TestSecondSessionRefused(t *testing.T) {
	srv := newTestServer(t)
	if _, _, err := srv.Initialize(initializeHeader(protocol.Version10, ""), "", nil); err != nil {
		t.Fatalf("first Initialize() error = %v", err)
	}
	_, _, err := srv.Initialize(initializeHeader(protocol.Version10, ""), "", nil)
	if !errors.Is(err, ErrMaxClients) {
		t.Fatalf("second Initialize() error = %v, want ErrMaxClients", err)
	}
	got := WireErrorFor(err)
	if !got.Fatal || got.FatalCode != protocol.FatalMaxClientsExceeded {
		t.Errorf("WireErrorFor() = %+v, want fatal code 4", got)
	}
	if n := srv.Sessions(); n != 1 {
		t.Errorf("Sessions() = %d, want 1", n)
	}
}

func TestAsyncInitializePairsSession(t *testing.T) {
	srv := newTestServer(t)
	sess, _, err := srv.Initialize(initializeHeader(protocol.Version10, ""), "", nil)
	if err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}

	got, resp, err := srv.AsyncInitialize(protocol.Header{
		Type:      protocol.AsyncInitialize,
		Parameter: uint32(sess.ID()),
	}, nil)
	if err != nil {
		t.Fatalf("AsyncInitialize() error = %v", err)
	}
	if got != sess {
		t.Error("AsyncInitialize returned a different session")
	}
	if resp.Type != protocol.AsyncInitializeResponse {
		t.Errorf("response type = %v", resp.Type)
	}
	if !sess.Ready() {
		t.Error("Ready() = false after pairing both channels")
	}
	if sess.State() != StateReady {
		t.Errorf("state = %v, want StateReady", sess.State())
	}
}

// TestAsyncInitializeResponseCarriesVendorID guards the asymmetry that is easy to
// get wrong: InitializeResponse carries the session ID, AsyncInitializeResponse
// carries the server vendor ID.
func TestAsyncInitializeResponseCarriesVendorID(t *testing.T) {
	srv, err := New(&nopDevice{}, Config{VendorID: [2]byte{'G', 'T'}})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	sess, _, err := srv.Initialize(initializeHeader(protocol.Version10, ""), "", nil)
	if err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}
	_, resp, err := srv.AsyncInitialize(protocol.Header{
		Type:      protocol.AsyncInitialize,
		Parameter: uint32(sess.ID()),
	}, nil)
	if err != nil {
		t.Fatalf("AsyncInitialize() error = %v", err)
	}
	if want := uint32('G')<<8 | uint32('T'); resp.Parameter != want {
		t.Errorf("parameter = %#x, want %#x (the server vendor ID)", resp.Parameter, want)
	}
	if resp.Parameter == uint32(sess.ID()) && sess.ID() != 0 {
		t.Error("parameter carries the session ID; it must carry the server vendor ID")
	}
}

func TestAsyncInitializeRejectsUnknownSession(t *testing.T) {
	srv := newTestServer(t)
	_, _, err := srv.AsyncInitialize(protocol.Header{
		Type:      protocol.AsyncInitialize,
		Parameter: 9999,
	}, nil)
	if !errors.Is(err, ErrInvalidSession) {
		t.Errorf("error = %v, want ErrInvalidSession", err)
	}
}

// TestAsyncInitializeRejectsSecondPairing stops a third connection from hijacking
// a session that is already complete.
func TestAsyncInitializeRejectsSecondPairing(t *testing.T) {
	srv := newTestServer(t)
	sess, _, err := srv.Initialize(initializeHeader(protocol.Version10, ""), "", nil)
	if err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}
	h := protocol.Header{Type: protocol.AsyncInitialize, Parameter: uint32(sess.ID())}
	if _, _, err := srv.AsyncInitialize(h, nil); err != nil {
		t.Fatalf("first AsyncInitialize() error = %v", err)
	}
	if _, _, err := srv.AsyncInitialize(h, nil); !errors.Is(err, ErrInvalidSession) {
		t.Errorf("second AsyncInitialize() error = %v, want ErrInvalidSession", err)
	}
}

func TestAsyncInitializeRejectsWrongFirstMessage(t *testing.T) {
	srv := newTestServer(t)
	_, _, err := srv.AsyncInitialize(protocol.Header{Type: protocol.AsyncStatusQuery}, nil)
	if !errors.Is(err, ErrInvalidInitSequence) {
		t.Errorf("error = %v, want ErrInvalidInitSequence", err)
	}
}

func TestSessionIDsAreUniqueAndNonZero(t *testing.T) {
	srv, err := New(&nopDevice{}, Config{MaxSessions: 64})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	seen := map[uint16]bool{}
	for i := range 64 {
		sess, _, err := srv.Initialize(initializeHeader(protocol.Version10, ""), "", nil)
		if err != nil {
			t.Fatalf("Initialize() %d error = %v", i, err)
		}
		if sess.ID() == 0 {
			t.Error("allocated session ID 0, which is reserved to keep a zero value distinguishable")
		}
		if seen[sess.ID()] {
			t.Errorf("session ID %d allocated twice", sess.ID())
		}
		seen[sess.ID()] = true
	}
}

// TestMaximumMessageSizeResponseShape covers the message framing. The semantics
// of each direction are covered by TestMaximumMessageSizeDirection in
// maxsize_test.go, which replaced a test that asserted the wrong behaviour: it
// expected the server to return the smaller of the two sizes, which is a
// negotiation of a common minimum and is not what IVI-6.1 Table 28 describes.
func TestMaximumMessageSizeResponseShape(t *testing.T) {
	srv := newTestServer(t)
	sess, _, err := srv.Initialize(initializeHeader(protocol.Version10, ""), "", nil)
	if err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}

	resp, reported := srv.MaximumMessageSize(sess, 4096)
	if resp.Type != protocol.AsyncMaximumMessageSizeResponse {
		t.Errorf("response type = %v", resp.Type)
	}
	if resp.Length != 8 {
		t.Errorf("response length = %d, want 8; the size travels in the payload", resp.Length)
	}
	if resp.Control != 0 || resp.Parameter != 0 {
		t.Errorf("control = %#x, parameter = %#x; both must be zero", resp.Control, resp.Parameter)
	}
	if reported != srv.Config().MaxRxPayload {
		t.Errorf("reported = %d, want the server's receive limit %d",
			reported, srv.Config().MaxRxPayload)
	}
	if got := sess.MaxTxPayload(); got != 4096 {
		t.Errorf("MaxTxPayload() = %d, want the client's limit 4096", got)
	}
}

func TestSessionCloseIsIdempotent(t *testing.T) {
	srv := newTestServer(t)
	sess, _, err := srv.Initialize(initializeHeader(protocol.Version10, ""), "", nil)
	if err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}
	if err := srv.removeSession(sess); err != nil {
		t.Fatalf("removeSession() error = %v", err)
	}
	if err := srv.removeSession(sess); err != nil {
		t.Errorf("second removeSession() error = %v, want nil", err)
	}
	if sess.State() != StateClosed {
		t.Errorf("state = %v, want StateClosed", sess.State())
	}
	if n := srv.Sessions(); n != 0 {
		t.Errorf("Sessions() = %d, want 0", n)
	}
}

func TestSessionClearCycle(t *testing.T) {
	srv := newTestServer(t)
	sess, _, err := srv.Initialize(initializeHeader(protocol.Version10, ""), "", nil)
	if err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}
	if err := sess.attachAsync(nil); err != nil {
		t.Fatalf("attachAsync() error = %v", err)
	}

	// Dirty the per-session state so that the reset is observable.
	sess.RMT().SentDataEnd()
	sess.Status().ReceivedSyncMessage(protocol.MessageID(0xffffff40))
	sess.Status().SetResponsePending(true)

	sess.beginClear()
	if sess.State() != StateClearing {
		t.Fatalf("state = %v, want StateClearing", sess.State())
	}
	// Operations are still accepted during clear; the clear handler discards
	// them until DeviceClearComplete.
	if err := sess.requireReady(); err != nil {
		t.Errorf("requireReady() during clear = %v, want nil", err)
	}

	sess.completeClear(false)

	if sess.State() != StateReady {
		t.Errorf("state = %v, want StateReady", sess.State())
	}
	if sess.RMT().Expected() {
		t.Error("RMT-expected survived the clear")
	}
	if sess.Status().ResponsePending() {
		t.Error("held response survived the clear")
	}
	if got := sess.Status().LastReceived(); got != protocol.StatusQueryInitialMessageID {
		t.Errorf("MessageID tracking = %#x, want the initial value", uint32(got))
	}
	if sess.Overlap() {
		t.Error("Overlap() = true after a clear that negotiated Synchronized Mode")
	}
}

func TestStateString(t *testing.T) {
	for s := StatePairing; s <= StateClosed; s++ {
		if s.String() == "" {
			t.Errorf("State(%d).String() is empty", s)
		}
	}
	if got := State(99).String(); got != "unknown state" {
		t.Errorf("State(99).String() = %q", got)
	}
}

// TestLastTxMessageID covers the correlation a chunked response needs: the
// intermediate Data packets may carry NoMessageID, but the terminating DataEnd
// must carry the originating client MessageID.
func TestLastTxMessageID(t *testing.T) {
	srv := newTestServer(t)
	sess, _, err := srv.Initialize(initializeHeader(protocol.Version10, ""), "", nil)
	if err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}
	if got := sess.LastTxMessageID(); got != protocol.NoMessageID {
		t.Errorf("LastTxMessageID() on a new session = %#x, want NoMessageID", uint32(got))
	}

	const id = protocol.MessageID(0xffffff08)
	sess.setLastTxMessageID(id)
	if got := sess.LastTxMessageID(); got != id {
		t.Errorf("LastTxMessageID() = %#x, want %#x", uint32(got), uint32(id))
	}

	// A device clear voids the correlation.
	sess.completeClear(false)
	if got := sess.LastTxMessageID(); got != protocol.NoMessageID {
		t.Errorf("LastTxMessageID() after clear = %#x, want NoMessageID", uint32(got))
	}
}

func TestServerAccessors(t *testing.T) {
	dev := &nopDevice{status: 0x41}
	srv, err := New(dev, Config{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if srv.Device() != dev {
		t.Error("Device() returned a different device")
	}
	if srv.Config().MaxSessions != 1 {
		t.Errorf("Config().MaxSessions = %d, want 1", srv.Config().MaxSessions)
	}
	if got := srv.Session(12345); got != nil {
		t.Error("Session() for an unknown ID returned non-nil")
	}
}

func TestCloseAll(t *testing.T) {
	srv, err := New(&nopDevice{}, Config{MaxSessions: 4})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	var sessions []*Session
	for range 3 {
		s, _, err := srv.Initialize(initializeHeader(protocol.Version10, ""), "", nil)
		if err != nil {
			t.Fatalf("Initialize() error = %v", err)
		}
		sessions = append(sessions, s)
	}
	if n := srv.Sessions(); n != 3 {
		t.Fatalf("Sessions() = %d, want 3", n)
	}
	if err := srv.CloseAll(); err != nil {
		t.Fatalf("CloseAll() error = %v", err)
	}
	if n := srv.Sessions(); n != 0 {
		t.Errorf("Sessions() after CloseAll = %d, want 0", n)
	}
	for i, s := range sessions {
		if s.State() != StateClosed {
			t.Errorf("session %d state = %v, want StateClosed", i, s.State())
		}
	}
	// Having closed everything, the server accepts a new session again.
	if _, _, err := srv.Initialize(initializeHeader(protocol.Version10, ""), "", nil); err != nil {
		t.Errorf("Initialize() after CloseAll error = %v", err)
	}
}

// TestRequireReadyOnClosedSession confirms a closed session is rejected as
// invalid rather than as not-yet-ready, so a client reconnecting gets the right
// diagnosis.
func TestRequireReadyOnClosedSession(t *testing.T) {
	srv := newTestServer(t)
	sess, _, err := srv.Initialize(initializeHeader(protocol.Version10, ""), "", nil)
	if err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}
	if err := srv.removeSession(sess); err != nil {
		t.Fatalf("removeSession() error = %v", err)
	}
	if err := sess.requireReady(); !errors.Is(err, ErrInvalidSession) {
		t.Errorf("requireReady() on a closed session = %v, want ErrInvalidSession", err)
	}
}
