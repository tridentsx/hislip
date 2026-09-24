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

// TestRemoteLocalTable is transcribed from IVI-6.1 Table 25 independently of
// device.go, so that a transcription error has to be made twice to go unnoticed.
func TestRemoteLocalTable(t *testing.T) {
	const (
		F  = EffectFalse
		T  = EffectTrue
		nc = EffectNoChange
	)
	table := []struct {
		mode    RemoteLocalMode
		value   uint8
		ren     Effect
		llo     Effect
		remote  Effect
		visaAbb string
	}{
		{DisableRemote, 0, F, F, F, "REN_DEASSERT"},
		{EnableRemote, 1, T, nc, nc, "REN_ASSERT"},
		{DisableRemoteGoToLocal, 2, F, F, F, "REN_DEASSERT_GTL"},
		{EnableRemoteGoToRemote, 3, T, nc, T, "REN_ASSERT_ADDRESS"},
		{EnableRemoteLockoutLocal, 4, T, T, nc, "REN_ASSERT_LLO"},
		{EnableRemoteGoToRemoteLockoutLocal, 5, T, T, T, "REN_ASSERT_ADDRESS_LLO"},
		{GoToLocal, 6, nc, nc, F, "REN_ADDRESS_GTL"},
	}
	if len(table) != 7 {
		t.Fatalf("expectation table has %d rows, want 7", len(table))
	}
	for _, tc := range table {
		t.Run(tc.visaAbb, func(t *testing.T) {
			if uint8(tc.mode) != tc.value {
				t.Errorf("control code = %d, want %d", uint8(tc.mode), tc.value)
			}
			if !tc.mode.Valid() {
				t.Error("Valid() = false, want true")
			}
			want := Effects{RemoteEnable: tc.ren, LocalLockout: tc.llo, Remote: tc.remote}
			if got := tc.mode.Effects(); got != want {
				t.Errorf("Effects() = %+v, want %+v", got, want)
			}
			if tc.mode.String() == "" {
				t.Error("String() is empty")
			}
		})
	}
}

// TestEffectNoChangeIsNotFalse guards the distinction a backend must respect: a
// mode marked "nc" in Table 25 must leave the state alone, not clear it.
func TestEffectNoChangeIsNotFalse(t *testing.T) {
	if EffectNoChange == EffectFalse {
		t.Fatal("EffectNoChange and EffectFalse are the same value")
	}
	// EnableRemote asserts REN and must not disturb local lockout.
	e := EnableRemote.Effects()
	if e.LocalLockout != EffectNoChange {
		t.Errorf("EnableRemote local lockout = %v, want EffectNoChange", e.LocalLockout)
	}
	// The zero Effects is all no-change, so an unhandled mode is inert rather
	// than destructive.
	var zero Effects
	if zero.RemoteEnable != EffectNoChange ||
		zero.LocalLockout != EffectNoChange ||
		zero.Remote != EffectNoChange {
		t.Error("the zero Effects is not all EffectNoChange")
	}
}

func TestRemoteLocalRejectsUndefinedModes(t *testing.T) {
	for _, m := range []RemoteLocalMode{7, 8, 100, 255} {
		if m.Valid() {
			t.Errorf("RemoteLocalMode(%d).Valid() = true, want false", m)
		}
		if got := m.Effects(); got != (Effects{}) {
			t.Errorf("RemoteLocalMode(%d).Effects() = %+v, want the zero Effects", m, got)
		}
		if got := m.String(); got != "undefined remote/local mode" {
			t.Errorf("RemoteLocalMode(%d).String() = %q", m, got)
		}
	}
}

// TestModesZeroAndTwoShareEffects records that Table 25 gives modes 0 and 2 the
// same bus effects. It is not a transcription error, so a future reader should
// not "fix" it.
func TestModesZeroAndTwoShareEffects(t *testing.T) {
	if DisableRemote.Effects() != DisableRemoteGoToLocal.Effects() {
		t.Error("modes 0 and 2 differ; IVI-6.1 Table 25 gives them identical effects")
	}
	if DisableRemote == DisableRemoteGoToLocal {
		t.Error("modes 0 and 2 have the same control code; they are distinct requests")
	}
}

func TestWireErrorMapping(t *testing.T) {
	tests := []struct {
		name      string
		err       error
		fatal     bool
		fatalCode protocol.FatalErrorCode
		code      protocol.ErrorCode
	}{
		{
			name:      "bad prologue is fatal",
			err:       protocol.ErrInvalidPrologue,
			fatal:     true,
			fatalCode: protocol.FatalPoorlyFormedHeader,
		},
		{
			name:      "operating before both channels exist is fatal",
			err:       ErrSessionNotReady,
			fatal:     true,
			fatalCode: protocol.FatalChannelsNotEstablished,
		},
		{
			name:      "a second client is refused fatally and diagnosably",
			err:       ErrMaxClients,
			fatal:     true,
			fatalCode: protocol.FatalMaxClientsExceeded,
		},
		{
			name:      "an unknown sub-address is an initialization failure",
			err:       ErrInvalidSubAddress,
			fatal:     true,
			fatalCode: protocol.FatalInvalidInitSequence,
		},
		{
			name:  "an oversized message is not fatal",
			err:   protocol.ErrMessageTooLarge,
			fatal: false,
			code:  protocol.NonFatalMessageTooLarge,
		},
		{
			name:  "a wrong-channel message is not fatal",
			err:   protocol.ErrWrongChannel,
			fatal: false,
			code:  protocol.NonFatalUnrecognizedMessageType,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := WireErrorFor(tc.err)
			if got.Fatal != tc.fatal {
				t.Fatalf("Fatal = %v, want %v", got.Fatal, tc.fatal)
			}
			if tc.fatal && got.FatalCode != tc.fatalCode {
				t.Errorf("FatalCode = %d, want %d", got.FatalCode, tc.fatalCode)
			}
			if !tc.fatal && got.Code != tc.code {
				t.Errorf("Code = %d, want %d", got.Code, tc.code)
			}
		})
	}
}

// TestBridgeCodesAreDeviceDefined confirms the bridge's own error codes sit in
// the device-defined range. Codes 6 to 127 are reserved for HiSLIP extensions
// and must not be used by an implementation.
func TestBridgeCodesAreDeviceDefined(t *testing.T) {
	codes := map[string]protocol.ErrorCode{
		"handshake timeout":        CodeHandshakeTimeout,
		"response timeout":         CodeResponseTimeout,
		"no listener":              CodeNoListener,
		"no serial poll response":  CodeNoSerialPollResponse,
		"undetermined disposition": CodeUndeterminedDisposition,
		"interface recovering":     CodeInterfaceRecovering,
	}
	seen := map[protocol.ErrorCode]string{}
	for name, code := range codes {
		if code < protocol.NonFatalDeviceDefinedMin {
			t.Errorf("%s = %d, which is below the device-defined range", name, code)
		}
		if other, dup := seen[code]; dup {
			t.Errorf("%s and %s share code %d", name, other, code)
		}
		seen[code] = name
	}
}

func TestServerErrorsAreDistinct(t *testing.T) {
	all := []error{
		ErrSessionNotReady, ErrInvalidSession, ErrInvalidSubAddress,
		ErrMaxClients, ErrLocked, ErrUnsupportedMode,
	}
	for i, a := range all {
		for j, b := range all {
			if i != j && errors.Is(a, b) {
				t.Errorf("error %d and %d compare equal", i, j)
			}
		}
	}
}
