// Copyright (c) 2026 The hislip developers. All rights reserved.
// Project site: https://github.com/gotmc/hislip
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

// ResponsePolicy decides whether a completed program message expects a response.
//
// This is the hardest semantic problem in the whole design and it has no clean
// solution, which §17 explains at length. A HiSLIP client never says "the
// application called viRead"; a native instrument knows when its parser produced
// response data, and a generic bridge does not. Every option here is a heuristic
// or a configuration choice, and the honest response is to make the choice
// explicit and swappable rather than to hide one inside the server.
type ResponsePolicy interface {
	// Disposition inspects a complete program message. The bytes are the whole
	// message as delivered to the instrument, terminator included.
	Disposition(programMessage []byte) Disposition
}

// SCPIQueryPolicy infers a response from SCPI query syntax.
//
// It looks for a question mark that marks a query header, which is what makes it
// work with ordinary SCPI instruments and ordinary VISA software. It is not a SCPI
// parser and does not need to be; it needs only enough lexical awareness to
// distinguish a real query marker from a question mark that happens to appear
// inside data.
//
// Two sources of false positives are handled, because both occur in normal
// traffic from the instruments this bridge is aimed at:
//
// Quoted strings. SCPI string arguments are single or double quoted and may
// contain anything, including question marks.
//
// Definite-length arbitrary block data, the IEEE 488.2 "#<digit><length><bytes>"
// form used for waveform and screen-image transfers. The payload is binary and
// will contain question marks by chance; roughly one byte in 256.
//
// What it does not handle, and what §48's full lexical detector must:
// indefinite-length blocks terminated by a newline with EOI, and instruments whose
// query syntax departs from IEEE 488.2. Those are why GOTMC_EXPLICIT and
// ALWAYS_READ exist as alternatives.
type SCPIQueryPolicy struct{}

// Disposition implements ResponsePolicy.
func (SCPIQueryPolicy) Disposition(msg []byte) Disposition {
	for i := 0; i < len(msg); i++ {
		switch c := msg[i]; c {
		case '\'', '"':
			// Skip to the matching quote. An unterminated quote consumes the
			// rest of the message, which is the safe reading: a question mark
			// after it is inside a malformed string, not a query header.
			i = skipQuoted(msg, i)
		case '#':
			// Possibly arbitrary block data. Skip its payload if so.
			next, isBlock := skipBlock(msg, i)
			if isBlock {
				i = next
			}
		case '?':
			return ResponseExpected
		}
	}
	return NoResponseExpected
}

// skipQuoted returns the index of the closing quote that matches the one at
// start, or the last index of msg when the string is unterminated.
//
// SCPI escapes a quote by doubling it, so a doubled quote inside a string does not
// end it.
func skipQuoted(msg []byte, start int) int {
	q := msg[start]
	for i := start + 1; i < len(msg); i++ {
		if msg[i] != q {
			continue
		}
		if i+1 < len(msg) && msg[i+1] == q {
			i++ // a doubled quote is an escaped quote
			continue
		}
		return i
	}
	return len(msg) - 1
}

// skipBlock returns the index of the final byte of a definite-length arbitrary
// block beginning at start, and whether one was found.
//
// The form is '#', one digit giving the number of length digits, that many length
// digits, then exactly that many bytes of payload. "#0" introduces an
// indefinite-length block, which this policy cannot skip because its length is not
// declared; it is reported as not a block, so the scan continues and may produce a
// false positive. That limitation is why §48 calls for a fuller detector.
func skipBlock(msg []byte, start int) (int, bool) {
	if start+1 >= len(msg) {
		return start, false
	}
	digits := msg[start+1]
	if digits < '1' || digits > '9' {
		return start, false
	}
	count := int(digits - '0')
	lenStart := start + 2
	if lenStart+count > len(msg) {
		return start, false
	}
	length := 0
	for i := lenStart; i < lenStart+count; i++ {
		if msg[i] < '0' || msg[i] > '9' {
			return start, false
		}
		length = length*10 + int(msg[i]-'0')
	}
	end := lenStart + count + length
	if end > len(msg) {
		// The declared payload runs past the message. Skip what there is rather
		// than scanning binary data for query markers.
		return len(msg) - 1, true
	}
	return end - 1, true
}

// AlwaysReadPolicy expects a response to every program message.
//
// This is the ALWAYS_READ policy of §48, for legacy instruments whose response
// behaviour cannot be inferred from syntax. It makes every command wait for the
// response timeout, so it is slow by construction; it is a correctness escape
// hatch, not a default.
type AlwaysReadPolicy struct{}

// Disposition implements ResponsePolicy.
func (AlwaysReadPolicy) Disposition([]byte) Disposition { return ResponseExpected }

// NeverReadPolicy expects no response to anything. It is useful for a write-only
// device and for tests.
type NeverReadPolicy struct{}

// Disposition implements ResponsePolicy.
func (NeverReadPolicy) Disposition([]byte) Disposition { return NoResponseExpected }

var (
	_ ResponsePolicy = SCPIQueryPolicy{}
	_ ResponsePolicy = AlwaysReadPolicy{}
	_ ResponsePolicy = NeverReadPolicy{}
)
