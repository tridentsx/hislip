// Copyright (c) 2026 The hislip developers. All rights reserved.
// Project site: https://github.com/tridentsx/hislip
// Use of this source code is governed by a MIT-style license that
// can be found in the LICENSE.txt file for the project.

package server

// Disposition reports whether a completed program message is expected to produce
// a response.
type Disposition uint8

// Dispositions, matching the QueryDisposition of §48.
const (
	// NoResponseExpected means the message produces no response.
	NoResponseExpected Disposition = iota

	// ResponseExpected means a response should be read from the instrument.
	ResponseExpected

	// Unknown means the policy could not decide. What happens then is
	// configurable; see §48.
	Unknown
)

// String returns a short description.
func (d Disposition) String() string {
	switch d {
	case NoResponseExpected:
		return "no response expected"
	case ResponseExpected:
		return "response expected"
	case Unknown:
		return "unknown"
	default:
		return "invalid disposition"
	}
}

// ResponsePolicy decides whether a program message expects a response.
//
// This is the hardest semantic problem in the design and it has no clean
// solution, which §17 explains at length. A HiSLIP client never says "the
// application called viRead"; a native instrument knows when its parser produced
// response data, and a generic bridge does not. Every option is a heuristic or a
// configuration choice, so the choice is explicit and swappable rather than
// hidden inside the server.
//
// The interface is incremental rather than taking a whole message, and that is
// not a style preference. A logical program message may be arbitrarily larger
// than any buffer in the adapter, which R-FW-130 requires, so accumulating one to
// classify it is wrong in principle. An earlier version of this package did
// accumulate, bounded by the negotiated maximum, and silently truncated anything
// longer; a 9810-byte compound message whose query marker fell in the tail was
// therefore classified as a command, the response was never read, and the client
// timed out. That bug was found by pyvisa-py, not by any test here, which is the
// argument for interoperability testing in one line.
//
// A policy is stateful and belongs to one session. Implementations need not be
// safe for concurrent use.
type ResponsePolicy interface {
	// Feed consumes the next chunk of the current program message.
	Feed(chunk []byte)

	// Disposition reports the decision for everything fed since the last Reset.
	Disposition() Disposition

	// Reset discards state and prepares for a new program message.
	Reset()
}

// PolicyFactory creates a policy per session. A Config holds one of these rather
// than a policy, because a policy carries per-message state.
type PolicyFactory func() ResponsePolicy

// SCPIQueryPolicy infers a response from SCPI query syntax.
//
// It looks for a question mark marking a query header, which is what makes it work
// with ordinary SCPI instruments and ordinary VISA software. It is not a SCPI
// parser and does not need to be; it needs only enough lexical awareness to tell a
// query marker from a question mark inside data.
//
// Two sources of false positive are handled, because both occur in normal traffic
// from the instruments this bridge targets:
//
// Quoted strings. SCPI string arguments are single or double quoted, may contain
// anything including question marks, and escape a quote by doubling it.
//
// Definite-length arbitrary block data, the IEEE 488.2 "#<digit><length><bytes>"
// form used for waveform and screen-image transfers. The payload is binary and
// contains question marks by chance, roughly one byte in 256.
//
// It is a byte-at-a-time state machine with O(1) state, so it is safe across chunk
// boundaries: a quoted string, a doubled-quote escape or a block header may be
// split between two Data messages without changing the result.
//
// What it does not handle, and what §48's fuller detector must: indefinite-length
// blocks, which declare no length and so cannot be skipped, and instruments whose
// query syntax departs from IEEE 488.2. Those are why the explicit-read extension
// and AlwaysReadPolicy exist.
type SCPIQueryPolicy struct {
	found bool
	state scpiState

	quote  byte   // the quote character of the string being scanned
	digits int    // remaining length digits to read in a block header
	length uint64 // block payload length accumulated so far
	remain uint64 // block payload bytes still to skip
}

// scpiState is the state of the lexical scan.
type scpiState uint8

const (
	// scpiNormal is scanning ordinary program text.
	scpiNormal scpiState = iota

	// scpiString is inside a quoted string.
	scpiString

	// scpiStringQuote has just seen the quote character inside a string, which
	// either ends it or is the first of a doubled escape.
	scpiStringQuote

	// scpiHash has seen '#' and is deciding whether a block follows.
	scpiHash

	// scpiBlockLen is reading the declared length of a block.
	scpiBlockLen

	// scpiBlockData is skipping a block payload.
	scpiBlockData
)

// NewSCPIQueryPolicy returns a policy ready for a new program message.
func NewSCPIQueryPolicy() *SCPIQueryPolicy { return &SCPIQueryPolicy{} }

// Reset implements ResponsePolicy.
func (p *SCPIQueryPolicy) Reset() {
	*p = SCPIQueryPolicy{}
}

// Disposition implements ResponsePolicy.
func (p *SCPIQueryPolicy) Disposition() Disposition {
	if p.found {
		return ResponseExpected
	}
	return NoResponseExpected
}

// Feed implements ResponsePolicy.
//
// Once a query marker has been seen the rest of the message cannot change the
// answer, so the scan stops. That matters for a large binary write following a
// query in one compound message: there is no point walking megabytes of payload
// to confirm a decision already made.
func (p *SCPIQueryPolicy) Feed(chunk []byte) {
	for i := 0; i < len(chunk); {
		if p.found {
			return
		}
		c := chunk[i]
		switch p.state {
		case scpiNormal:
			switch c {
			case '?':
				p.found = true
			case '\'', '"':
				p.quote = c
				p.state = scpiString
			case '#':
				p.state = scpiHash
			}
			i++

		case scpiString:
			if c == p.quote {
				p.state = scpiStringQuote
			}
			i++

		case scpiStringQuote:
			if c == p.quote {
				// A doubled quote is an escaped quote: still inside the string.
				p.state = scpiString
				i++
				continue
			}
			// The string ended at the previous byte. Re-examine this one as
			// ordinary text rather than consuming it here, so that a query marker
			// immediately after a closing quote is not missed.
			p.state = scpiNormal
			p.quote = 0

		case scpiHash:
			if c >= '1' && c <= '9' {
				p.digits = int(c - '0')
				p.length = 0
				p.state = scpiBlockLen
				i++
				continue
			}
			// Not a definite-length block. "#0" introduces an indefinite-length
			// block, whose length is not declared and so cannot be skipped; its
			// payload is scanned as text, which may produce a false positive. That
			// limitation is documented on the type.
			p.state = scpiNormal

		case scpiBlockLen:
			if c < '0' || c > '9' {
				// A malformed header. Treat what follows as ordinary text rather
				// than guessing a length.
				p.state = scpiNormal
				continue
			}
			p.length = p.length*10 + uint64(c-'0')
			p.digits--
			i++
			if p.digits == 0 {
				p.remain = p.length
				p.state = scpiBlockData
				if p.remain == 0 {
					p.state = scpiNormal
				}
			}

		case scpiBlockData:
			available := uint64(len(chunk) - i)
			skip := p.remain
			if skip > available {
				skip = available
			}
			i += int(skip)
			p.remain -= skip
			if p.remain == 0 {
				p.state = scpiNormal
			}
		}
	}
}

// AlwaysReadPolicy expects a response to every program message.
//
// This is the ALWAYS_READ policy of §48, for legacy instruments whose response
// behaviour cannot be inferred from syntax. Every command then waits for the
// response timeout, so it is slow by construction: a correctness escape hatch, not
// a default.
type AlwaysReadPolicy struct{}

// Feed implements ResponsePolicy.
func (AlwaysReadPolicy) Feed([]byte) {}

// Disposition implements ResponsePolicy.
func (AlwaysReadPolicy) Disposition() Disposition { return ResponseExpected }

// Reset implements ResponsePolicy.
func (AlwaysReadPolicy) Reset() {}

// NeverReadPolicy expects no response to anything. It suits a write-only device
// and tests.
type NeverReadPolicy struct{}

// Feed implements ResponsePolicy.
func (NeverReadPolicy) Feed([]byte) {}

// Disposition implements ResponsePolicy.
func (NeverReadPolicy) Disposition() Disposition { return NoResponseExpected }

// Reset implements ResponsePolicy.
func (NeverReadPolicy) Reset() {}

var (
	_ ResponsePolicy = (*SCPIQueryPolicy)(nil)
	_ ResponsePolicy = AlwaysReadPolicy{}
	_ ResponsePolicy = NeverReadPolicy{}
)
