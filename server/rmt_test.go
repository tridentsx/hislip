// Copyright (c) 2026 The hislip developers. All rights reserved.
// Project site: https://github.com/tridentsx/hislip
// Use of this source code is governed by a MIT-style license that
// can be found in the LICENSE.txt file for the project.

package server

import "testing"

// TestRMTTruthTable checks the rule of IVI-6.1 section 3.1.1 directly: on
// receiving Data, DataEnd or Trigger, equal flags clear RMT-expected and
// unequal flags declare an interrupted error.
func TestRMTTruthTable(t *testing.T) {
	tests := []struct {
		name        string
		expected    bool
		delivered   bool
		wantErr     Interrupted
		wantCleared bool
	}{
		{
			name:        "no response outstanding, client did not acknowledge",
			expected:    false,
			delivered:   false,
			wantErr:     NotInterrupted,
			wantCleared: true,
		},
		{
			name:        "response outstanding, client acknowledged it",
			expected:    true,
			delivered:   true,
			wantErr:     NotInterrupted,
			wantCleared: true,
		},
		{
			name:        "response outstanding, client did not read it",
			expected:    true,
			delivered:   false,
			wantErr:     InterruptedRMTMismatch,
			wantCleared: true,
		},
		{
			name:        "no response outstanding, client acknowledged one anyway",
			expected:    false,
			delivered:   true,
			wantErr:     InterruptedRMTMismatch,
			wantCleared: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var r RMT
			if tc.expected {
				r.SentDataEnd()
			}
			if got := r.ReceivedSyncMessage(tc.delivered); got != tc.wantErr {
				t.Errorf("ReceivedSyncMessage() = %v, want %v", got, tc.wantErr)
			}
			if got := r.Expected(); got != !tc.wantCleared {
				t.Errorf("Expected() = %v, want %v", got, !tc.wantCleared)
			}
		})
	}
}

// TestRMTMismatchIsSilent is the distinction most easily got wrong. The
// RMT-mismatch error is reported only inside the server; sending an Interrupted
// transaction for it would be a protocol violation.
func TestRMTMismatchIsSilent(t *testing.T) {
	var r RMT
	r.SentDataEnd()
	got := r.ReceivedSyncMessage(false)
	if got != InterruptedRMTMismatch {
		t.Fatalf("ReceivedSyncMessage() = %v, want InterruptedRMTMismatch", got)
	}
	if got.Notifies() {
		t.Error("InterruptedRMTMismatch.Notifies() = true; this error must not be sent to the client")
	}
	if !InterruptedInputQueued.Notifies() {
		t.Error("InterruptedInputQueued.Notifies() = false; this error must send both messages")
	}
	if NotInterrupted.Notifies() {
		t.Error("NotInterrupted.Notifies() = true")
	}
}

// TestRMTMismatchDoesNotCascade pins the interpretation documented on
// RMT.ReceivedSyncMessage. The standard does not say what becomes of
// RMT-expected after a mismatch. Leaving it set would turn one missed read into
// an error on every subsequent message, which contradicts "the server resumes
// normal operation".
func TestRMTMismatchDoesNotCascade(t *testing.T) {
	var r RMT
	r.SentDataEnd()

	if got := r.ReceivedSyncMessage(false); got != InterruptedRMTMismatch {
		t.Fatalf("first message: got %v, want InterruptedRMTMismatch", got)
	}
	for i := 0; i < 5; i++ {
		if got := r.ReceivedSyncMessage(false); got != NotInterrupted {
			t.Fatalf("message %d after the error: got %v, want NotInterrupted", i+2, got)
		}
	}
}

// TestRMTStatusQueryClears covers the second way RMT-expected is cleared, and
// confirms a status query cannot itself declare an interrupted error.
func TestRMTStatusQueryClears(t *testing.T) {
	var r RMT

	r.SentDataEnd()
	r.ReceivedStatusQuery(false)
	if !r.Expected() {
		t.Error("a status query without RMT-delivered cleared RMT-expected, want unchanged")
	}

	r.ReceivedStatusQuery(true)
	if r.Expected() {
		t.Error("a status query with RMT-delivered did not clear RMT-expected")
	}

	// Having been cleared by the status query, the next synchronous message with
	// the flag clear is now consistent and must not error.
	if got := r.ReceivedSyncMessage(false); got != NotInterrupted {
		t.Errorf("after status query: got %v, want NotInterrupted", got)
	}
}

// TestRMTNormalQuerySequence walks the sequence a conforming client produces, to
// confirm no interrupted error is declared anywhere in it.
func TestRMTNormalQuerySequence(t *testing.T) {
	var r RMT

	// Query 1: client sends DataEnd with the flag clear, server answers DataEnd.
	if got := r.ReceivedSyncMessage(false); got != NotInterrupted {
		t.Fatalf("query 1 request: got %v", got)
	}
	r.SentDataEnd()

	// Query 2: the client has now delivered the terminator to its application,
	// so it sets the flag on its next message.
	if got := r.ReceivedSyncMessage(true); got != NotInterrupted {
		t.Fatalf("query 2 request: got %v", got)
	}
	r.SentDataEnd()

	// Query 3: same again.
	if got := r.ReceivedSyncMessage(true); got != NotInterrupted {
		t.Fatalf("query 3 request: got %v", got)
	}

	// A command with no response: flag clear, nothing outstanding.
	if got := r.ReceivedSyncMessage(false); got != NotInterrupted {
		t.Fatalf("command: got %v", got)
	}
}

// TestRMTCommandOnlySequence covers a run of commands that produce no
// responses, which must never declare an interrupted error however long it runs.
func TestRMTCommandOnlySequence(t *testing.T) {
	var r RMT
	for i := 0; i < 100; i++ {
		if got := r.ReceivedSyncMessage(false); got != NotInterrupted {
			t.Fatalf("command %d: got %v, want NotInterrupted", i, got)
		}
	}
}

func TestRMTReset(t *testing.T) {
	var r RMT
	r.SentDataEnd()
	if !r.Expected() {
		t.Fatal("SentDataEnd did not set RMT-expected")
	}
	r.Reset()
	if r.Expected() {
		t.Error("Reset did not clear RMT-expected")
	}
	// After a device clear the client's next message has the flag clear, and
	// that must be consistent.
	if got := r.ReceivedSyncMessage(false); got != NotInterrupted {
		t.Errorf("after Reset: got %v, want NotInterrupted", got)
	}
}

func TestZeroRMTIsUsable(t *testing.T) {
	var r RMT
	if r.Expected() {
		t.Error("the zero RMT has RMT-expected set, want clear")
	}
}

func TestInterruptedString(t *testing.T) {
	for _, i := range []Interrupted{NotInterrupted, InterruptedRMTMismatch, InterruptedInputQueued} {
		if i.String() == "" {
			t.Errorf("Interrupted(%d).String() is empty", i)
		}
	}
	if got := Interrupted(99).String(); got != "unknown interrupted kind" {
		t.Errorf("Interrupted(99).String() = %q", got)
	}
}
