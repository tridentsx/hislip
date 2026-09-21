// Copyright (c) 2026 The hislip developers. All rights reserved.
// Project site: https://github.com/gotmc/hislip
// Use of this source code is governed by a MIT-style license that
// can be found in the LICENSE.txt file for the project.

package server

import (
	"errors"

	"github.com/gotmc/hislip/protocol"
)

// ErrInvalidTransition indicates an attempt to move the transaction state
// machine along an edge that does not exist.
var ErrInvalidTransition = errors.New("hislip: invalid transaction state transition")

// TxState is the state of the synchronized input/response transaction, as
// required by §11.3 of the design specification.
//
// This is distinct from the session lifecycle State. A session is ready or not;
// a transaction is where in the request/response cycle the server currently is.
// Modelling it explicitly is what makes the interrupted condition observable as a
// protocol state rather than hidden inside the GPIB backend.
type TxState uint8

// Transaction states.
const (
	// TxIdle means no program message is in progress.
	TxIdle TxState = iota

	// TxReceivingInput means Data messages have arrived but no DataEnd yet.
	TxReceivingInput

	// TxInputComplete means DataEnd arrived and the program message is whole.
	TxInputComplete

	// TxResponsePending means the backend is expected to produce a response.
	TxResponsePending

	// TxSendingResponse means response bytes are being written to the client.
	TxSendingResponse

	// TxInterrupted means an interrupted error was declared and the transaction
	// is being unwound.
	TxInterrupted

	// TxClearing means a Device Clear is in progress.
	TxClearing

	// TxClosed means the session has ended.
	TxClosed
)

// String returns a short description of the state.
func (s TxState) String() string {
	switch s {
	case TxIdle:
		return "idle"
	case TxReceivingInput:
		return "receiving input"
	case TxInputComplete:
		return "input complete"
	case TxResponsePending:
		return "response pending"
	case TxSendingResponse:
		return "sending response"
	case TxInterrupted:
		return "interrupted"
	case TxClearing:
		return "clearing"
	case TxClosed:
		return "closed"
	default:
		return "unknown transaction state"
	}
}

// txTransitions is the adjacency set of the state machine.
//
// Clearing and Closed are reachable from everywhere, because a Device Clear or a
// fatal error can arrive at any point; they are added below rather than listed in
// every row.
var txTransitions = map[TxState][]TxState{
	TxIdle:            {TxReceivingInput, TxInputComplete},
	TxReceivingInput:  {TxReceivingInput, TxInputComplete, TxInterrupted},
	TxInputComplete:   {TxResponsePending, TxIdle, TxReceivingInput, TxInterrupted},
	TxResponsePending: {TxSendingResponse, TxIdle, TxInterrupted},
	TxSendingResponse: {TxSendingResponse, TxIdle, TxInterrupted},
	TxInterrupted:     {TxIdle},
	TxClearing:        {TxIdle},
	TxClosed:          nil,
}

// Transaction is the synchronized input/response state of one session.
//
// The zero Transaction is ready for use and is in TxIdle.
type Transaction struct {
	state TxState

	// id is the MessageID of the client message that completed the current
	// program message. A response is attributed to it.
	id protocol.MessageID
}

// State returns the current state.
func (t *Transaction) State() TxState { return t.state }

// MessageID returns the MessageID the current transaction is attributed to.
func (t *Transaction) MessageID() protocol.MessageID { return t.id }

// To moves the machine to next, or returns ErrInvalidTransition.
//
// Clearing and Closed are always reachable, because a Device Clear or a fatal
// error may arrive at any point. Every other edge must be declared. Rejecting
// undeclared transitions turns a class of sequencing bug into a test failure
// rather than into a subtly wrong wire exchange.
func (t *Transaction) To(next TxState) error {
	if next == t.state && !selfEdge(t.state) {
		return ErrInvalidTransition
	}
	if next == TxClearing || next == TxClosed {
		t.state = next
		return nil
	}
	if t.state == TxClosed {
		return ErrInvalidTransition
	}
	for _, allowed := range txTransitions[t.state] {
		if allowed == next {
			t.state = next
			return nil
		}
	}
	return ErrInvalidTransition
}

// selfEdge reports whether a state may transition to itself, which is how
// repeated Data messages and successive response chunks are represented.
func selfEdge(s TxState) bool {
	return s == TxReceivingInput || s == TxSendingResponse
}

// BeginInput records the arrival of a Data message.
func (t *Transaction) BeginInput() error {
	return t.To(TxReceivingInput)
}

// CompleteInput records the arrival of a DataEnd carrying id, which makes the
// program message whole.
func (t *Transaction) CompleteInput(id protocol.MessageID) error {
	if err := t.To(TxInputComplete); err != nil {
		return err
	}
	t.id = id
	return nil
}

// ExpectResponse records that the backend is expected to produce a response.
func (t *Transaction) ExpectResponse() error {
	return t.To(TxResponsePending)
}

// BeginResponse records that response bytes are being written.
func (t *Transaction) BeginResponse() error {
	return t.To(TxSendingResponse)
}

// Complete returns the machine to idle at the end of a transaction.
func (t *Transaction) Complete() error {
	if err := t.To(TxIdle); err != nil {
		return err
	}
	t.id = protocol.NoMessageID
	return nil
}

// Interrupt records an interrupted error.
func (t *Transaction) Interrupt() error {
	return t.To(TxInterrupted)
}

// Clear moves the machine into the clearing state, which is reachable from
// anywhere.
func (t *Transaction) Clear() error {
	return t.To(TxClearing)
}

// Reset returns the machine to idle unconditionally, discarding any transaction
// in progress. Used when a Device Clear completes.
func (t *Transaction) Reset() {
	t.state = TxIdle
	t.id = protocol.NoMessageID
}

// Close marks the transaction closed.
func (t *Transaction) Close() {
	t.state = TxClosed
	t.id = protocol.NoMessageID
}
