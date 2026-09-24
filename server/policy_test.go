// Copyright (c) 2026 The hislip developers. All rights reserved.
// Project site: https://github.com/tridentsx/hislip
// Use of this source code is governed by a MIT-style license that
// can be found in the LICENSE.txt file for the project.

package server

import (
	"bytes"
	"strings"
	"testing"

	"github.com/tridentsx/hislip/protocol"
)

// classify feeds a whole message in one chunk.
func classify(msg string) Disposition {
	p := NewSCPIQueryPolicy()
	p.Feed([]byte(msg))
	return p.Disposition()
}

// classifyChunked feeds a message in fixed-size chunks, which is how it arrives
// when a client splits a program message across Data packets.
func classifyChunked(msg string, size int) Disposition {
	p := NewSCPIQueryPolicy()
	for i := 0; i < len(msg); i += size {
		end := i + size
		if end > len(msg) {
			end = len(msg)
		}
		p.Feed([]byte(msg[i:end]))
	}
	return p.Disposition()
}

var policyCases = []struct {
	name string
	msg  string
	want Disposition
}{
	// Plain queries and commands.
	{"simple query", "*IDN?\n", ResponseExpected},
	{"simple command", "*RST\n", NoResponseExpected},
	{"command with argument", "SOUR:VOLT 3.3\n", NoResponseExpected},
	{"query with argument", "MEAS:VOLT? AUTO\n", ResponseExpected},
	{"empty message", "", NoResponseExpected},
	{"terminator only", "\n", NoResponseExpected},

	// Compound messages. A query anywhere means a response.
	{"command then query", "*CLS;*IDN?\n", ResponseExpected},
	{"query then command", "*IDN?;*CLS\n", ResponseExpected},
	{"two queries", "*IDN?;*OPC?\n", ResponseExpected},

	// Quoted strings, the first false positive a naive detector hits.
	{"question mark in double quotes", "DISP:TEXT \"what?\"\n", NoResponseExpected},
	{"question mark in single quotes", "DISP:TEXT 'what?'\n", NoResponseExpected},
	{"query after a quoted string", "DISP:TEXT \"hello\";*IDN?\n", ResponseExpected},
	{"query before a quoted string", "*IDN?;DISP:TEXT \"x?\"\n", ResponseExpected},
	{"doubled quote escape", "DISP:TEXT \"say \"\"what?\"\"\"\n", NoResponseExpected},
	{"query after a doubled quote escape", "DISP:TEXT \"a\"\"b\";*IDN?\n", ResponseExpected},
	{"unterminated quote swallows the rest", "DISP:TEXT \"what?\n", NoResponseExpected},
	{"other quote inside a string", "DISP:TEXT \"it's a ?\"\n", NoResponseExpected},
	{"query right after a closing quote", "DISP:TEXT \"x\"?\n", ResponseExpected},

	// Definite-length block data, the second false positive.
	{"block with no query", "DATA #14ABCD\n", NoResponseExpected},
	{"block containing a question mark", "DATA #14AB?D\n", NoResponseExpected},
	{"query after a block", "DATA #14ABCD;*IDN?\n", ResponseExpected},
	{"query before a block", "*IDN?;DATA #14ABCD\n", ResponseExpected},
	{"two-digit length", "DATA #210AB?DEFGHIJ\n", NoResponseExpected},
	{"zero-length block then query", "DATA #10;*IDN?\n", ResponseExpected},

	// A hash that does not introduce a definite-length block.
	{"hash then query", "FUNC #;*IDN?\n", ResponseExpected},
	{"indefinite block marker then query", "DATA #0ABC;*IDN?\n", ResponseExpected},
	{"malformed block header then query", "DATA #1X;*IDN?\n", ResponseExpected},
}

func TestSCPIQueryPolicy(t *testing.T) {
	for _, tc := range policyCases {
		t.Run(tc.name, func(t *testing.T) {
			if got := classify(tc.msg); got != tc.want {
				t.Errorf("Disposition(%q) = %v, want %v", tc.msg, got, tc.want)
			}
		})
	}
}

// TestSCPIQueryPolicyChunkBoundaries feeds every case at every chunk size, so a
// quoted string, a doubled-quote escape or a block header split across two Data
// messages must not change the answer. This is the property the streaming design
// exists for.
func TestSCPIQueryPolicyChunkBoundaries(t *testing.T) {
	for _, tc := range policyCases {
		t.Run(tc.name, func(t *testing.T) {
			for size := 1; size <= len(tc.msg)+1; size++ {
				if got := classifyChunked(tc.msg, size); got != tc.want {
					t.Errorf("chunk size %d: Disposition(%q) = %v, want %v",
						size, tc.msg, got, tc.want)
				}
			}
		})
	}
}

// TestSCPIQueryPolicyLongCompoundMessage is the regression test for the bug
// pyvisa-py found. A compound message longer than the negotiated maximum payload,
// whose only query marker falls in the final chunk, must still be classified as a
// query. The earlier accumulating implementation truncated at the payload limit
// and answered NoResponseExpected, so the response was never read and the client
// timed out.
func TestSCPIQueryPolicyLongCompoundMessage(t *testing.T) {
	msg := strings.Repeat("SOUR:VOLT 3.3;", 700) + "MEAS:VOLT?\n"
	if len(msg) <= DefaultMaxRxPayload {
		t.Fatalf("the test message is %d bytes, not longer than the payload limit", len(msg))
	}

	if got := classify(msg); got != ResponseExpected {
		t.Errorf("whole message: Disposition() = %v, want ResponseExpected", got)
	}
	// Split exactly as a client would at the negotiated maximum.
	if got := classifyChunked(msg, DefaultMaxRxPayload); got != ResponseExpected {
		t.Errorf("split at the payload limit: Disposition() = %v, want ResponseExpected", got)
	}
	for _, size := range []int{1, 7, 1024, 8191, 8192, 8193} {
		if got := classifyChunked(msg, size); got != ResponseExpected {
			t.Errorf("chunk size %d: Disposition() = %v, want ResponseExpected", size, got)
		}
	}
}

// TestSCPIQueryPolicyBinaryBlock uses a realistic waveform write: a binary payload
// containing every byte value, which a naive detector misreads about once per 256
// bytes.
func TestSCPIQueryPolicyBinaryBlock(t *testing.T) {
	payload := make([]byte, 512)
	for i := range payload {
		payload[i] = byte(i)
	}
	if !bytes.Contains(payload, []byte{'?'}) {
		t.Fatal("the payload contains no question mark, so it proves nothing")
	}

	var msg bytes.Buffer
	msg.WriteString("WAV:DATA #3512")
	msg.Write(payload)
	msg.WriteString("\n")

	p := NewSCPIQueryPolicy()
	p.Feed(msg.Bytes())
	if got := p.Disposition(); got != NoResponseExpected {
		t.Errorf("binary waveform write: Disposition() = %v, want NoResponseExpected", got)
	}

	// The same write split at every offset inside the block must behave the same.
	for _, size := range []int{1, 3, 13, 14, 15, 100, 511, 512, 513} {
		p := NewSCPIQueryPolicy()
		b := msg.Bytes()
		for i := 0; i < len(b); i += size {
			end := i + size
			if end > len(b) {
				end = len(b)
			}
			p.Feed(b[i:end])
		}
		if got := p.Disposition(); got != NoResponseExpected {
			t.Errorf("chunk size %d: Disposition() = %v, want NoResponseExpected", size, got)
		}
	}

	// A trailing query must still be found after the block.
	msg.WriteString("*OPC?\n")
	p = NewSCPIQueryPolicy()
	p.Feed(msg.Bytes())
	if got := p.Disposition(); got != ResponseExpected {
		t.Errorf("with a trailing query: Disposition() = %v, want ResponseExpected", got)
	}
}

// TestSCPIQueryPolicyStopsScanningOnceDecided covers the short-circuit: there is
// no point walking megabytes of binary payload after a query marker has settled
// the answer.
func TestSCPIQueryPolicyStopsScanningOnceDecided(t *testing.T) {
	p := NewSCPIQueryPolicy()
	p.Feed([]byte("*IDN?"))
	if p.Disposition() != ResponseExpected {
		t.Fatal("the query marker was not seen")
	}
	// A quote that would otherwise change the lexical state must be ignored.
	p.Feed([]byte("\"unterminated"))
	if got := p.Disposition(); got != ResponseExpected {
		t.Errorf("Disposition() = %v after further input, want ResponseExpected", got)
	}
}

func TestSCPIQueryPolicyReset(t *testing.T) {
	p := NewSCPIQueryPolicy()
	p.Feed([]byte("*IDN?\n"))
	if p.Disposition() != ResponseExpected {
		t.Fatal("first message not classified as a query")
	}
	p.Reset()
	if got := p.Disposition(); got != NoResponseExpected {
		t.Errorf("Disposition() after Reset = %v, want NoResponseExpected", got)
	}
	// Lexical state is cleared too, so an unterminated string does not leak into
	// the next message.
	p.Feed([]byte("DISP:TEXT \"open"))
	p.Reset()
	p.Feed([]byte("*OPC?\n"))
	if got := p.Disposition(); got != ResponseExpected {
		t.Errorf("Disposition() = %v; lexical state survived Reset", got)
	}
}

func TestAlwaysAndNeverReadPolicies(t *testing.T) {
	for _, m := range []string{"", "*RST\n", "*IDN?\n"} {
		a := AlwaysReadPolicy{}
		a.Feed([]byte(m))
		if got := a.Disposition(); got != ResponseExpected {
			t.Errorf("AlwaysReadPolicy on %q = %v", m, got)
		}
		a.Reset()

		n := NeverReadPolicy{}
		n.Feed([]byte(m))
		if got := n.Disposition(); got != NoResponseExpected {
			t.Errorf("NeverReadPolicy on %q = %v", m, got)
		}
		n.Reset()
	}
}

func TestDispositionString(t *testing.T) {
	for d := NoResponseExpected; d <= Unknown; d++ {
		if d.String() == "" {
			t.Errorf("Disposition(%d).String() is empty", d)
		}
	}
	if got := Disposition(9).String(); got != "invalid disposition" {
		t.Errorf("Disposition(9).String() = %q", got)
	}
}

// TestDefaultPolicyIsSCPI confirms a zero Config yields a server usable with
// ordinary SCPI instruments.
func TestDefaultPolicyIsSCPI(t *testing.T) {
	srv, err := New(&nopDevice{}, Config{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if _, ok := srv.newPolicy().(*SCPIQueryPolicy); !ok {
		t.Errorf("default policy = %T, want *SCPIQueryPolicy", srv.newPolicy())
	}
}

func TestConfiguredPolicyIsUsed(t *testing.T) {
	srv, err := New(&nopDevice{}, Config{
		Policy: func() ResponsePolicy { return AlwaysReadPolicy{} },
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if _, ok := srv.newPolicy().(AlwaysReadPolicy); !ok {
		t.Errorf("policy = %T, want AlwaysReadPolicy", srv.newPolicy())
	}
}

// TestEachSessionGetsItsOwnPolicy matters because a policy carries per-message
// lexical state. Sharing one between sessions would let an unterminated string in
// one session change the classification in another.
func TestEachSessionGetsItsOwnPolicy(t *testing.T) {
	srv, err := New(&nopDevice{}, Config{MaxSessions: 2})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	a, _, err := srv.Initialize(initializeHeader(protocol.Version10, ""), "", nil)
	if err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}
	b, _, err := srv.Initialize(initializeHeader(protocol.Version10, ""), "", nil)
	if err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}
	if a.policy == nil || b.policy == nil {
		t.Fatal("a session has no policy")
	}
	if a.policy == b.policy {
		t.Error("two sessions share one policy instance")
	}

	// An unterminated string in one session must not affect the other.
	a.feedProgramMessage([]byte("DISP:TEXT \"open"))
	b.feedProgramMessage([]byte("*IDN?\n"))
	if got := b.programDisposition(); got != ResponseExpected {
		t.Errorf("session B disposition = %v, want ResponseExpected", got)
	}
}
