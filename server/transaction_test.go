// Copyright (c) 2026 The hislip developers. All rights reserved.
// Project site: https://github.com/gotmc/hislip
// Use of this source code is governed by a MIT-style license that
// can be found in the LICENSE.txt file for the project.

package server

import (
	"errors"
	"testing"

	"github.com/gotmc/hislip/protocol"
)

func TestZeroTransactionIsIdle(t *testing.T) {
	var tx Transaction
	if tx.State() != TxIdle {
		t.Errorf("zero Transaction state = %v, want TxIdle", tx.State())
	}
}

// TestNormalQueryTransaction walks the sequence a query produces, which must pass
// end to end without a rejected transition.
func TestNormalQueryTransaction(t *testing.T) {
	var tx Transaction
	const id = protocol.MessageID(0xffffff00)

	steps := []struct {
		name string
		do   func() error
		want TxState
	}{
		{"Data arrives", tx.BeginInput, TxReceivingInput},
		{"more Data arrives", tx.BeginInput, TxReceivingInput},
		{"DataEnd arrives", func() error { return tx.CompleteInput(id) }, TxInputComplete},
		{"a response is expected", tx.ExpectResponse, TxResponsePending},
		{"first chunk goes out", tx.BeginResponse, TxSendingResponse},
		{"second chunk goes out", tx.BeginResponse, TxSendingResponse},
		{"the response completes", tx.Complete, TxIdle},
	}
	for _, step := range steps {
		if err := step.do(); err != nil {
			t.Fatalf("%s: %v", step.name, err)
		}
		if tx.State() != step.want {
			t.Fatalf("%s: state = %v, want %v", step.name, tx.State(), step.want)
		}
	}
	if got := tx.MessageID(); got != protocol.NoMessageID {
		t.Errorf("MessageID after Complete = %#x, want NoMessageID", uint32(got))
	}
}

// TestCommandTransaction covers a write with no response, which returns straight
// to idle from InputComplete.
func TestCommandTransaction(t *testing.T) {
	var tx Transaction
	if err := tx.CompleteInput(protocol.InitialMessageID); err != nil {
		t.Fatalf("CompleteInput() error = %v", err)
	}
	if err := tx.Complete(); err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	if tx.State() != TxIdle {
		t.Errorf("state = %v, want TxIdle", tx.State())
	}
}

func TestTransactionRejectsInvalidTransitions(t *testing.T) {
	tests := []struct {
		name  string
		setup func(*Transaction)
		do    func(*Transaction) error
	}{
		{
			name:  "a response cannot be sent before input completes",
			setup: func(*Transaction) {},
			do:    func(tx *Transaction) error { return tx.BeginResponse() },
		},
		{
			name:  "a response cannot be expected while still receiving input",
			setup: func(tx *Transaction) { _ = tx.BeginInput() },
			do:    func(tx *Transaction) error { return tx.ExpectResponse() },
		},
		{
			name:  "idle cannot go to idle",
			setup: func(*Transaction) {},
			do:    func(tx *Transaction) error { return tx.Complete() },
		},
		{
			name:  "a closed transaction accepts nothing",
			setup: func(tx *Transaction) { tx.Close() },
			do:    func(tx *Transaction) error { return tx.BeginInput() },
		},
		{
			name:  "input cannot resume after it is complete without new Data",
			setup: func(tx *Transaction) { _ = tx.CompleteInput(protocol.InitialMessageID) },
			do:    func(tx *Transaction) error { return tx.CompleteInput(protocol.InitialMessageID) },
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var tx Transaction
			tc.setup(&tx)
			before := tx.State()
			if err := tc.do(&tx); !errors.Is(err, ErrInvalidTransition) {
				t.Errorf("error = %v, want ErrInvalidTransition", err)
			}
			if tx.State() != before {
				t.Errorf("a rejected transition changed the state to %v", tx.State())
			}
		})
	}
}

// TestClearingIsReachableFromAnywhere covers the requirement that a Device Clear
// may arrive at any point in a transaction.
func TestClearingIsReachableFromAnywhere(t *testing.T) {
	setups := map[string]func(*Transaction){
		"idle":             func(*Transaction) {},
		"receiving input":  func(tx *Transaction) { _ = tx.BeginInput() },
		"input complete":   func(tx *Transaction) { _ = tx.CompleteInput(protocol.InitialMessageID) },
		"response pending": func(tx *Transaction) { _ = tx.CompleteInput(protocol.InitialMessageID); _ = tx.ExpectResponse() },
		"sending response": func(tx *Transaction) {
			_ = tx.CompleteInput(protocol.InitialMessageID)
			_ = tx.ExpectResponse()
			_ = tx.BeginResponse()
		},
	}
	for name, setup := range setups {
		t.Run(name, func(t *testing.T) {
			var tx Transaction
			setup(&tx)
			if err := tx.Clear(); err != nil {
				t.Fatalf("Clear() from %s: %v", name, err)
			}
			if tx.State() != TxClearing {
				t.Errorf("state = %v, want TxClearing", tx.State())
			}
			// A completed clear returns to idle.
			if err := tx.Complete(); err != nil {
				t.Errorf("Complete() after Clear: %v", err)
			}
		})
	}
}

func TestTransactionInterruptAndRecover(t *testing.T) {
	var tx Transaction
	if err := tx.CompleteInput(protocol.InitialMessageID); err != nil {
		t.Fatalf("CompleteInput() error = %v", err)
	}
	if err := tx.ExpectResponse(); err != nil {
		t.Fatalf("ExpectResponse() error = %v", err)
	}
	if err := tx.Interrupt(); err != nil {
		t.Fatalf("Interrupt() error = %v", err)
	}
	if tx.State() != TxInterrupted {
		t.Fatalf("state = %v, want TxInterrupted", tx.State())
	}
	// Recovery returns to idle and nowhere else, so an interrupted transaction
	// cannot quietly continue producing a response.
	if err := tx.BeginResponse(); !errors.Is(err, ErrInvalidTransition) {
		t.Errorf("BeginResponse() while interrupted = %v, want ErrInvalidTransition", err)
	}
	if err := tx.Complete(); err != nil {
		t.Errorf("Complete() after interrupt: %v", err)
	}
}

func TestTransactionReset(t *testing.T) {
	var tx Transaction
	if err := tx.CompleteInput(protocol.MessageID(0xffffff10)); err != nil {
		t.Fatalf("CompleteInput() error = %v", err)
	}
	tx.Reset()
	if tx.State() != TxIdle {
		t.Errorf("state = %v, want TxIdle", tx.State())
	}
	if got := tx.MessageID(); got != protocol.NoMessageID {
		t.Errorf("MessageID = %#x, want NoMessageID", uint32(got))
	}
}

func TestTxStateString(t *testing.T) {
	for s := TxIdle; s <= TxClosed; s++ {
		if s.String() == "" {
			t.Errorf("TxState(%d).String() is empty", s)
		}
	}
	if got := TxState(99).String(); got != "unknown transaction state" {
		t.Errorf("TxState(99).String() = %q", got)
	}
}

func TestResponseOutcomeString(t *testing.T) {
	for o := ResponseComplete; o <= ResponseInterrupted; o++ {
		if o.String() == "" {
			t.Errorf("ResponseOutcome(%d).String() is empty", o)
		}
	}
	if got := ResponseOutcome(99).String(); got != "unknown outcome" {
		t.Errorf("ResponseOutcome(99).String() = %q", got)
	}
}
