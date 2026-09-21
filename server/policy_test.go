// Copyright (c) 2026 The hislip developers. All rights reserved.
// Project site: https://github.com/gotmc/hislip
// Use of this source code is governed by a MIT-style license that
// can be found in the LICENSE.txt file for the project.

package server

import (
	"bytes"
	"testing"
)

func TestSCPIQueryPolicy(t *testing.T) {
	tests := []struct {
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

		// Quoted strings. A question mark inside one is data, not a query
		// marker, and this is the first false positive a naive detector hits.
		{"question mark in double quotes", "DISP:TEXT \"what?\"\n", NoResponseExpected},
		{"question mark in single quotes", "DISP:TEXT 'what?'\n", NoResponseExpected},
		{"query after a quoted string", "DISP:TEXT \"hello\";*IDN?\n", ResponseExpected},
		{"query before a quoted string", "*IDN?;DISP:TEXT \"x?\"\n", ResponseExpected},
		{"doubled quote escape", "DISP:TEXT \"say \"\"what?\"\"\"\n", NoResponseExpected},
		{"unterminated quote swallows the rest", "DISP:TEXT \"what?\n", NoResponseExpected},
		{"nested other quote", "DISP:TEXT \"it's a ?\"\n", NoResponseExpected},

		// Definite-length arbitrary block data. The payload is binary and will
		// contain question marks by chance, which is the second false positive.
		{"block with no query", "DATA #14ABCD\n", NoResponseExpected},
		{"block containing a question mark", "DATA #14AB?D\n", NoResponseExpected},
		{"query after a block", "DATA #14ABCD;*IDN?\n", ResponseExpected},
		{"query before a block", "*IDN?;DATA #14ABCD\n", ResponseExpected},
		{"two-digit length", "DATA #210AB?DEFGHIJ\n", NoResponseExpected},
		{"block length runs past the message", "DATA #19ABC", NoResponseExpected},

		// A hash that is not block data must not swallow anything.
		{"hash then query", "FUNC #;*IDN?\n", ResponseExpected},
		{"indefinite block marker then query", "DATA #0ABC;*IDN?\n", ResponseExpected},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := (SCPIQueryPolicy{}).Disposition([]byte(tc.msg)); got != tc.want {
				t.Errorf("Disposition(%q) = %v, want %v", tc.msg, got, tc.want)
			}
		})
	}
}

// TestSCPIQueryPolicyBinaryBlock uses a realistic waveform transfer: a long
// binary payload containing every byte value, which a naive detector would
// misread as a query roughly once in 256 bytes.
func TestSCPIQueryPolicyBinaryBlock(t *testing.T) {
	payload := make([]byte, 512)
	for i := range payload {
		payload[i] = byte(i)
	}
	if !bytes.Contains(payload, []byte{'?'}) {
		t.Fatal("the test payload contains no question mark, so it proves nothing")
	}

	var msg bytes.Buffer
	msg.WriteString("WAV:DATA #3512")
	msg.Write(payload)
	msg.WriteString("\n")

	if got := (SCPIQueryPolicy{}).Disposition(msg.Bytes()); got != NoResponseExpected {
		t.Errorf("Disposition() = %v for a binary waveform write, want NoResponseExpected", got)
	}

	// The same transfer followed by a query must still be detected.
	msg.WriteString("*OPC?\n")
	if got := (SCPIQueryPolicy{}).Disposition(msg.Bytes()); got != ResponseExpected {
		t.Errorf("Disposition() = %v with a trailing query, want ResponseExpected", got)
	}
}

// TestSCPIQueryPolicyQuotedBinaryIsNotConfused checks the two skip rules do not
// interfere: a quote inside a block payload must not start a string.
func TestSCPIQueryPolicyQuotedBinaryIsNotConfused(t *testing.T) {
	msg := []byte("DATA #14\"?\"x\n")
	if got := (SCPIQueryPolicy{}).Disposition(msg); got != NoResponseExpected {
		t.Errorf("Disposition(%q) = %v, want NoResponseExpected", msg, got)
	}
}

func TestAlwaysAndNeverReadPolicies(t *testing.T) {
	msgs := []string{"", "*RST\n", "*IDN?\n"}
	for _, m := range msgs {
		if got := (AlwaysReadPolicy{}).Disposition([]byte(m)); got != ResponseExpected {
			t.Errorf("AlwaysReadPolicy.Disposition(%q) = %v", m, got)
		}
		if got := (NeverReadPolicy{}).Disposition([]byte(m)); got != NoResponseExpected {
			t.Errorf("NeverReadPolicy.Disposition(%q) = %v", m, got)
		}
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

// TestDefaultPolicyIsSCPI confirms the server default, so that a zero Config
// yields a server usable with ordinary SCPI instruments.
func TestDefaultPolicyIsSCPI(t *testing.T) {
	srv, err := New(&nopDevice{}, Config{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if _, ok := srv.policy.(SCPIQueryPolicy); !ok {
		t.Errorf("default policy = %T, want SCPIQueryPolicy", srv.policy)
	}
}

func TestConfiguredPolicyIsUsed(t *testing.T) {
	srv, err := New(&nopDevice{}, Config{Policy: AlwaysReadPolicy{}})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if _, ok := srv.policy.(AlwaysReadPolicy); !ok {
		t.Errorf("policy = %T, want AlwaysReadPolicy", srv.policy)
	}
}
