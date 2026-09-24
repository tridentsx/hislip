// Copyright (c) 2026 The hislip developers. All rights reserved.
// Project site: https://github.com/tridentsx/hislip
// Use of this source code is governed by a MIT-style license that
// can be found in the LICENSE.txt file for the project.

package server_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/tridentsx/hislip/internal/testdevice"
	"github.com/tridentsx/hislip/protocol"
	"github.com/tridentsx/hislip/server"
)

// TestDeviceClearFourMessageSequence walks the whole transaction and checks the
// channel each message belongs on.
func TestDeviceClearFourMessageSequence(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)

	// Step 2: the server acknowledges on the asynchronous channel, proposing its
	// preferred features.
	ack, err := f.srv.BeginDeviceClear(f.sess, f.tx)
	if err != nil {
		t.Fatalf("BeginDeviceClear() error = %v", err)
	}
	if ack.Type != protocol.AsyncDeviceClearAcknowledge {
		t.Errorf("type = %v, want AsyncDeviceClearAcknowledge", ack.Type)
	}
	if !ack.Type.LegalOn(protocol.ChannelAsynchronous) {
		t.Error("AsyncDeviceClearAcknowledge is not legal on the asynchronous channel")
	}
	if ack.Control&protocol.FeatureOverlapped != 0 {
		t.Errorf("proposed bitmap = %#x; bit 0 set proposes Overlap Mode", ack.Control)
	}
	if f.sess.State() != server.StateClearing {
		t.Errorf("session state = %v, want StateClearing", f.sess.State())
	}
	if f.tx.State() != server.TxClearing {
		t.Errorf("transaction state = %v, want TxClearing", f.tx.State())
	}

	// Step 4: the server acknowledges completion on the synchronous channel.
	done, err := f.srv.CompleteDeviceClear(ctx, f.sess, f.tx, protocol.Header{
		Type: protocol.DeviceClearComplete,
	})
	if err != nil {
		t.Fatalf("CompleteDeviceClear() error = %v", err)
	}
	if done.Type != protocol.DeviceClearAcknowledge {
		t.Errorf("type = %v, want DeviceClearAcknowledge", done.Type)
	}
	// This is the channel that IVI-6.1 Table 4 and section 6.12 disagree about.
	// We follow the transaction description: synchronous.
	if !done.Type.LegalOn(protocol.ChannelSynchronous) {
		t.Error("DeviceClearAcknowledge is not legal on the synchronous channel; see §4.4")
	}
	if f.sess.State() != server.StateReady {
		t.Errorf("session state = %v, want StateReady", f.sess.State())
	}
	if n := f.dev.Count(testdevice.OpClear); n != 1 {
		t.Errorf("device Clear called %d times, want exactly 1", n)
	}
}

// TestDeviceClearDeclinesOverlap is the binding half of the mode prohibition: the
// bitmap the server returns is what the client must use, whatever it asked for.
func TestDeviceClearDeclinesOverlap(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)

	if _, err := f.srv.BeginDeviceClear(f.sess, f.tx); err != nil {
		t.Fatalf("BeginDeviceClear() error = %v", err)
	}
	done, err := f.srv.CompleteDeviceClear(ctx, f.sess, f.tx, protocol.Header{
		Type:    protocol.DeviceClearComplete,
		Control: protocol.FeatureOverlapped | protocol.FeatureEncryptionMandatory,
	})
	if err != nil {
		t.Fatalf("CompleteDeviceClear() error = %v", err)
	}
	if done.Control&protocol.FeatureOverlapped != 0 {
		t.Errorf("agreed bitmap = %#x; the server agreed to Overlap Mode", done.Control)
	}
	if done.Control&protocol.FeatureEncryptionMandatory != 0 {
		t.Errorf("agreed bitmap = %#x; encryption is not implemented", done.Control)
	}
	if f.sess.Overlap() {
		t.Error("the session is in Overlap Mode after declining it")
	}
}

// TestDeviceClearResetsProtocolState covers the §13 requirement list: MessageID
// tracking, RMT state and any held response all return to their session-start
// values, while the TCP session stays usable.
func TestDeviceClearResetsProtocolState(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t, []byte("stale response\n"))
	const id = protocol.MessageID(0xffffff30)

	if _, err := f.srv.ReceiveData(ctx, f.sess, f.tx, protocol.Header{
		Type:      protocol.DataEnd,
		Parameter: uint32(id),
	}, []byte("A?\n")); err != nil {
		t.Fatalf("ReceiveData() error = %v", err)
	}
	if _, err := f.srv.SendResponse(ctx, f.sess, f.tx, f.response(id, 64)); err != nil {
		t.Fatalf("SendResponse() error = %v", err)
	}
	if !f.sess.RMT().Expected() {
		t.Fatal("RMT-expected not set before the clear")
	}

	if _, err := f.srv.BeginDeviceClear(f.sess, f.tx); err != nil {
		t.Fatalf("BeginDeviceClear() error = %v", err)
	}
	if _, err := f.srv.CompleteDeviceClear(ctx, f.sess, f.tx, protocol.Header{
		Type: protocol.DeviceClearComplete,
	}); err != nil {
		t.Fatalf("CompleteDeviceClear() error = %v", err)
	}

	if f.sess.RMT().Expected() {
		t.Error("RMT-expected survived the clear")
	}
	if got := f.sess.Status().LastReceived(); got != protocol.StatusQueryInitialMessageID {
		t.Errorf("MessageID tracking = %#x, want the initial value", uint32(got))
	}
	if f.sess.Status().ResponsePending() {
		t.Error("a held response survived the clear")
	}
	if got := f.sess.LastTxMessageID(); got != protocol.NoMessageID {
		t.Errorf("LastTxMessageID = %#x, want NoMessageID", uint32(got))
	}
	if f.tx.State() != server.TxIdle {
		t.Errorf("transaction state = %v, want TxIdle", f.tx.State())
	}
	if !f.sess.Ready() {
		t.Error("the session is no longer usable; a clear must leave it so")
	}

	// A client's first message after a clear carries RMT-delivered clear, and
	// that must now be consistent rather than judged interrupted.
	interrupted, err := f.srv.ReceiveData(ctx, f.sess, f.tx, protocol.Header{
		Type:      protocol.DataEnd,
		Parameter: uint32(protocol.InitialMessageID),
	}, []byte("B?\n"))
	if err != nil {
		t.Fatalf("ReceiveData() after clear error = %v", err)
	}
	if interrupted != server.NotInterrupted {
		t.Errorf("first message after a clear judged %v", interrupted)
	}
}

// TestDeviceClearAbortsInFlightOperation is the requirement that makes Device
// Clear worth having. An operator issues one because the instrument is stuck, so
// it must not wait for the stuck operation's timeout.
func TestDeviceClearAbortsInFlightOperation(t *testing.T) {
	f := newFixture(t)

	opCtx, release := f.sess.Operation(context.Background())
	defer release()

	select {
	case <-opCtx.Done():
		t.Fatal("the operation context was already cancelled")
	default:
	}

	if _, err := f.srv.BeginDeviceClear(f.sess, f.tx); err != nil {
		t.Fatalf("BeginDeviceClear() error = %v", err)
	}

	select {
	case <-opCtx.Done():
	case <-time.After(time.Second):
		t.Fatal("the in-flight operation was not abandoned by the device clear")
	}
	if !errors.Is(opCtx.Err(), context.Canceled) {
		t.Errorf("operation context error = %v, want context.Canceled", opCtx.Err())
	}
}

// TestDeviceClearAcknowledgedEvenIfBackendFails checks that the handshake
// completes when the instrument refuses to clear. Withholding the acknowledgement
// would leave a conforming client waiting indefinitely at step 7.
func TestDeviceClearAcknowledgedEvenIfBackendFails(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	f.dev.Errors = map[testdevice.Op]error{
		testdevice.OpClear: errors.New("no listener on the bus"),
	}

	if _, err := f.srv.BeginDeviceClear(f.sess, f.tx); err != nil {
		t.Fatalf("BeginDeviceClear() error = %v", err)
	}
	done, err := f.srv.CompleteDeviceClear(ctx, f.sess, f.tx, protocol.Header{
		Type: protocol.DeviceClearComplete,
	})
	if err == nil {
		t.Error("the backend failure was not reported to the caller")
	}
	if done.Type != protocol.DeviceClearAcknowledge {
		t.Errorf("type = %v; the acknowledgement must still be sent", done.Type)
	}
	// The protocol state was reset before the instrument was touched, so it is
	// consistent despite the failure.
	if f.sess.State() != server.StateReady {
		t.Errorf("session state = %v, want StateReady", f.sess.State())
	}
	if f.sess.RMT().Expected() {
		t.Error("protocol state was left half-cleared by the backend failure")
	}
}

// TestDiscardDuringClear covers step 6: synchronous messages are accepted and
// ignored until DeviceClearComplete is found.
func TestDiscardDuringClear(t *testing.T) {
	f := newFixture(t)

	if server.DiscardDuringClear(f.sess, protocol.Header{Type: protocol.Data}) {
		t.Error("messages are being discarded outside a clear")
	}
	if _, err := f.srv.BeginDeviceClear(f.sess, f.tx); err != nil {
		t.Fatalf("BeginDeviceClear() error = %v", err)
	}
	for _, typ := range []protocol.MessageType{protocol.Data, protocol.DataEnd, protocol.Trigger} {
		if !server.DiscardDuringClear(f.sess, protocol.Header{Type: typ}) {
			t.Errorf("%v was not discarded during a clear", typ)
		}
	}
	// The one message that must get through.
	if server.DiscardDuringClear(f.sess, protocol.Header{Type: protocol.DeviceClearComplete}) {
		t.Error("DeviceClearComplete was discarded; the clear could never complete")
	}
}

// TestStatusQueryUsesBothFields checks the two fields Revision 2 overlooked.
func TestStatusQueryUsesBothFields(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t, []byte("data"))
	f.dev.Status = 0x20 // ESB set, MAV clear
	const id = protocol.InitialMessageID

	if _, err := f.srv.ReceiveData(ctx, f.sess, f.tx, protocol.Header{
		Type:      protocol.DataEnd,
		Parameter: uint32(id),
	}, []byte("A?\n")); err != nil {
		t.Fatalf("ReceiveData() error = %v", err)
	}
	f.sess.Status().SetResponsePending(true)

	// A query naming the active transaction, with the flag clear, reports MAV.
	resp, err := f.srv.StatusQuery(ctx, f.sess, protocol.Header{
		Type:      protocol.AsyncStatusQuery,
		Parameter: uint32(id),
	})
	if err != nil {
		t.Fatalf("StatusQuery() error = %v", err)
	}
	if resp.Type != protocol.AsyncStatusResponse {
		t.Errorf("type = %v, want AsyncStatusResponse", resp.Type)
	}
	if resp.Control&server.StatusMAV == 0 {
		t.Errorf("status = %#x; MAV not set with data held for the active transaction", resp.Control)
	}
	if resp.Control&0x20 == 0 {
		t.Errorf("status = %#x; the device's ESB bit was dropped", resp.Control)
	}

	// A query naming a stale MessageID reports MAV false even though data is
	// still held. This is R-SYNC-030.
	resp, err = f.srv.StatusQuery(ctx, f.sess, protocol.Header{
		Type:      protocol.AsyncStatusQuery,
		Parameter: uint32(id.Previous()),
	})
	if err != nil {
		t.Fatalf("StatusQuery() error = %v", err)
	}
	if resp.Control&server.StatusMAV != 0 {
		t.Errorf("status = %#x; MAV set for a stale MessageID", resp.Control)
	}

	// The RMT-delivered flag both forces MAV false and clears RMT-expected.
	f.sess.RMT().SentDataEnd()
	resp, err = f.srv.StatusQuery(ctx, f.sess, protocol.Header{
		Type:      protocol.AsyncStatusQuery,
		Control:   protocol.ControlRMTDelivered,
		Parameter: uint32(id),
	})
	if err != nil {
		t.Fatalf("StatusQuery() error = %v", err)
	}
	if resp.Control&server.StatusMAV != 0 {
		t.Errorf("status = %#x; MAV set although RMT-delivered was reported", resp.Control)
	}
	if f.sess.RMT().Expected() {
		t.Error("a status query carrying RMT-delivered did not clear RMT-expected")
	}
}

// TestServiceRequestCoalesces covers R-SRV-023. A second request must not be sent
// before the client clears RQS with a status query.
func TestServiceRequestCoalesces(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	f.dev.Status = 0x41

	h, send := f.srv.ServiceRequest(f.sess, 0x41)
	if !send {
		t.Fatal("the first service request was suppressed")
	}
	if h.Type != protocol.AsyncServiceRequest {
		t.Errorf("type = %v, want AsyncServiceRequest", h.Type)
	}
	if h.Control != 0x41 {
		t.Errorf("status = %#x, want 0x41 in the control code", h.Control)
	}

	// Further events while one is outstanding coalesce.
	for i := 0; i < 3; i++ {
		if _, send := f.srv.ServiceRequest(f.sess, 0x41); send {
			t.Errorf("service request %d was sent while one was outstanding", i+2)
		}
	}

	// The client's status query clears the latch, and RQS is reported to it even
	// though the bridge's own poll would have consumed the instrument's.
	resp, err := f.srv.StatusQuery(ctx, f.sess, protocol.Header{
		Type:      protocol.AsyncStatusQuery,
		Parameter: uint32(protocol.StatusQueryInitialMessageID),
	})
	if err != nil {
		t.Fatalf("StatusQuery() error = %v", err)
	}
	if resp.Control&server.StatusRQS == 0 {
		t.Errorf("status = %#x; the latched RQS was not reported", resp.Control)
	}
	if f.sess.Status().ServiceRequestOutstanding() {
		t.Error("the latch survived the status query")
	}

	// With the latch cleared, a new event is sent again.
	if _, send := f.srv.ServiceRequest(f.sess, 0x42); !send {
		t.Error("a new service request was suppressed after the latch cleared")
	}
}

func TestServiceRequestRefusedBeforePairing(t *testing.T) {
	srv, err := server.New(testdevice.New(), server.Config{})
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
	if _, send := srv.ServiceRequest(sess, 0x41); send {
		t.Error("a service request was sent before both channels existed")
	}
	if _, send := srv.ServiceRequest(nil, 0x41); send {
		t.Error("a service request was sent with no session")
	}
}

func TestRemoteLocalAppliesValidModes(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)

	for mode := server.DisableRemote; mode <= server.GoToLocal; mode++ {
		resp, err := f.srv.RemoteLocal(ctx, f.sess, protocol.Header{
			Type:    protocol.AsyncRemoteLocalControl,
			Control: uint8(mode),
		})
		if err != nil {
			t.Fatalf("RemoteLocal(%v) error = %v", mode, err)
		}
		if resp.Type != protocol.AsyncRemoteLocalResponse {
			t.Errorf("type = %v, want AsyncRemoteLocalResponse", resp.Type)
		}
	}
	if n := f.dev.Count(testdevice.OpRemoteLocal); n != 7 {
		t.Errorf("device saw %d remote/local calls, want 7", n)
	}
	calls := f.dev.Calls()
	if calls[0].Mode != server.DisableRemote || calls[6].Mode != server.GoToLocal {
		t.Error("the modes reached the device out of order or altered")
	}
}

// TestRemoteLocalRejectsUndefinedMode confirms an undefined control code is
// answered with error code 2 and never reaches the backend, which would otherwise
// have to decide what an undefined request means.
func TestRemoteLocalRejectsUndefinedMode(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)

	for _, control := range []uint8{7, 100, 255} {
		_, err := f.srv.RemoteLocal(ctx, f.sess, protocol.Header{
			Type:    protocol.AsyncRemoteLocalControl,
			Control: control,
		})
		if !errors.Is(err, server.ErrUnrecognizedControlCode) {
			t.Errorf("control %d: error = %v, want ErrUnrecognizedControlCode", control, err)
		}
		wire := server.WireErrorFor(err)
		if wire.Fatal {
			t.Errorf("control %d: an undefined mode was treated as fatal", control)
		}
		if wire.Code != protocol.NonFatalUnrecognizedControlCode {
			t.Errorf("control %d: code = %d, want 2", control, wire.Code)
		}
	}
	if n := f.dev.Count(testdevice.OpRemoteLocal); n != 0 {
		t.Errorf("an undefined mode reached the backend %d times", n)
	}
}

// TestAsyncHandlersRequireReadySession confirms the fatal-error-2 guard applies to
// the asynchronous operations too.
func TestAsyncHandlersRequireReadySession(t *testing.T) {
	ctx := context.Background()
	srv, err := server.New(testdevice.New(), server.Config{})
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
	tx := &server.Transaction{}

	if _, err := srv.StatusQuery(ctx, sess, protocol.Header{Type: protocol.AsyncStatusQuery}); !errors.Is(err, server.ErrSessionNotReady) {
		t.Errorf("StatusQuery() error = %v, want ErrSessionNotReady", err)
	}
	if _, err := srv.RemoteLocal(ctx, sess, protocol.Header{Type: protocol.AsyncRemoteLocalControl}); !errors.Is(err, server.ErrSessionNotReady) {
		t.Errorf("RemoteLocal() error = %v, want ErrSessionNotReady", err)
	}
	if _, err := srv.BeginDeviceClear(sess, tx); !errors.Is(err, server.ErrSessionNotReady) {
		t.Errorf("BeginDeviceClear() error = %v, want ErrSessionNotReady", err)
	}
}

func TestHandlersRejectWrongMessageType(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)

	if _, err := f.srv.StatusQuery(ctx, f.sess, protocol.Header{Type: protocol.Data}); !errors.Is(err, protocol.ErrUnknownMessageType) {
		t.Errorf("StatusQuery() with the wrong type = %v", err)
	}
	if _, err := f.srv.RemoteLocal(ctx, f.sess, protocol.Header{Type: protocol.Data}); !errors.Is(err, protocol.ErrUnknownMessageType) {
		t.Errorf("RemoteLocal() with the wrong type = %v", err)
	}
	if _, err := f.srv.CompleteDeviceClear(ctx, f.sess, f.tx, protocol.Header{Type: protocol.Data}); !errors.Is(err, protocol.ErrUnknownMessageType) {
		t.Errorf("CompleteDeviceClear() with the wrong type = %v", err)
	}
}
