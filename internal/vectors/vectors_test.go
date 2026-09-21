// Copyright (c) 2026 The hislip developers. All rights reserved.
// Project site: https://github.com/gotmc/hislip
// Use of this source code is governed by a MIT-style license that
// can be found in the LICENSE.txt file for the project.

package vectors

import "testing"

// headerSize is repeated here rather than imported from the protocol package,
// so that the vectors do not depend on the code they test.
const headerSize = 16

func TestHeadersLoad(t *testing.T) {
	hs, err := Headers()
	if err != nil {
		t.Fatalf("Headers() error = %v", err)
	}
	if len(hs) < 20 {
		t.Errorf("loaded %d header vectors, want at least 20", len(hs))
	}
	seen := map[string]bool{}
	for _, h := range hs {
		if seen[h.Name] {
			t.Errorf("duplicate vector name %q", h.Name)
		}
		seen[h.Name] = true
		if len(h.Wire) != headerSize {
			t.Errorf("%s: wire is %d bytes, want %d", h.Name, len(h.Wire), headerSize)
		}
		// The prologue is the one field not restated in the decoded columns, so
		// it is checked here.
		if len(h.Wire) >= 2 && (h.Wire[0] != 'H' || h.Wire[1] != 'S') {
			t.Errorf("%s: prologue = %q, want \"HS\"", h.Name, h.Wire[:2])
		}
	}
}

// TestHeadersCoverInterestingCases guards against a corpus that drifts into
// only testing easy values.
func TestHeadersCoverInterestingCases(t *testing.T) {
	hs, err := Headers()
	if err != nil {
		t.Fatalf("Headers() error = %v", err)
	}
	var (
		maxLength   bool
		noMessageID bool
		statusInit  bool
		rmtSet      bool
		vendor      bool
		nonZeroCtrl bool
	)
	for _, h := range hs {
		if h.Length == ^uint64(0) {
			maxLength = true
		}
		if h.Parameter == 0xffffffff {
			noMessageID = true
		}
		if h.Parameter == 0xfffffefe {
			statusInit = true
		}
		if h.Control&0x01 != 0 {
			rmtSet = true
		}
		if h.Type >= 128 {
			vendor = true
		}
		if h.Control != 0 {
			nonZeroCtrl = true
		}
	}
	for _, c := range []struct {
		name string
		got  bool
	}{
		{"a maximal 64-bit payload length", maxLength},
		{"the 0xffffffff no-MessageID value", noMessageID},
		{"the 0xfffffefe post-reset status query value", statusInit},
		{"control code bit 0 set", rmtSet},
		{"a vendor-specific message type", vendor},
		{"a non-zero control code", nonZeroCtrl},
	} {
		if !c.got {
			t.Errorf("corpus does not cover %s", c.name)
		}
	}
}

func TestInvalidsLoad(t *testing.T) {
	vs, err := Invalids()
	if err != nil {
		t.Fatalf("Invalids() error = %v", err)
	}
	if len(vs) < 8 {
		t.Errorf("loaded %d invalid vectors, want at least 8", len(vs))
	}
	var prologue, short int
	for _, v := range vs {
		switch v.Reason {
		case ReasonPrologue:
			prologue++
			if len(v.Wire) < headerSize {
				t.Errorf(
					"%s: reason is %q but the input is only %d bytes, so it would be rejected as short first",
					v.Name, ReasonPrologue, len(v.Wire),
				)
			}
		case ReasonShort:
			short++
			if len(v.Wire) >= headerSize {
				t.Errorf("%s: reason is %q but the input is %d bytes", v.Name, ReasonShort, len(v.Wire))
			}
		default:
			t.Errorf("%s: unexpected reason %q", v.Name, v.Reason)
		}
	}
	if prologue == 0 {
		t.Error("no prologue vectors")
	}
	if short == 0 {
		t.Error("no short vectors")
	}
}

func TestEmptyMarker(t *testing.T) {
	vs, err := Invalids()
	if err != nil {
		t.Fatalf("Invalids() error = %v", err)
	}
	for _, v := range vs {
		if v.Name == "empty" {
			if len(v.Wire) != 0 {
				t.Errorf("empty vector has %d bytes, want 0", len(v.Wire))
			}
			return
		}
	}
	t.Error("no vector named \"empty\"; the empty-input case is not covered")
}

func TestParseRejectsBadRecords(t *testing.T) {
	// The field-count check is what stops a hex token containing a space from
	// being silently split into two vectors.
	if _, err := parse("headers.txt", 99); err == nil {
		t.Error("parse() with the wrong field count returned nil error")
	}
}
