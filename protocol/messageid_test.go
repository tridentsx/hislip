// Copyright (c) 2026 The hislip developers. All rights reserved.
// Project site: https://github.com/tridentsx/hislip
// Use of this source code is governed by a MIT-style license that
// can be found in the LICENSE.txt file for the project.

package protocol

import "testing"

func TestMessageIDConstants(t *testing.T) {
	// The literal values are the requirement. Deriving them here would let a
	// wrong constant pass.
	if InitialMessageID != 0xffffff00 {
		t.Errorf("InitialMessageID = %#x, want 0xffffff00", uint32(InitialMessageID))
	}
	if NoMessageID != 0xffffffff {
		t.Errorf("NoMessageID = %#x, want 0xffffffff", uint32(NoMessageID))
	}
	if StatusQueryInitialMessageID != 0xfffffefe {
		t.Errorf(
			"StatusQueryInitialMessageID = %#x, want 0xfffffefe",
			uint32(StatusQueryInitialMessageID),
		)
	}
	// IVI-6.1 states the status query value as InitialMessageID - 2.
	if got := InitialMessageID.Previous(); got != StatusQueryInitialMessageID {
		t.Errorf("InitialMessageID-2 = %#x, want %#x", uint32(got), uint32(StatusQueryInitialMessageID))
	}
}

func TestMessageIDIncrementsByTwo(t *testing.T) {
	want := []MessageID{
		0xffffff00, 0xffffff02, 0xffffff04, 0xffffff06, 0xffffff08,
	}
	c := NewCounter()
	for i, w := range want {
		if got := c.Take(); got != w {
			t.Fatalf("Take() %d = %#x, want %#x", i, uint32(got), uint32(w))
		}
	}
}

// TestMessageIDWrap checks the wrap-around that IVI-6.1 explicitly permits.
// Starting at 0xffffff00 and stepping by two, the sequence reaches 0xfffffffe
// and then wraps to 0x00000000, never landing on the reserved 0xffffffff.
func TestMessageIDWrap(t *testing.T) {
	if got := MessageID(0xfffffffe).Next(); got != 0 {
		t.Errorf("0xfffffffe.Next() = %#x, want 0x0", uint32(got))
	}
	if got := MessageID(0).Previous(); got != 0xfffffffe {
		t.Errorf("0.Previous() = %#x, want 0xfffffffe", uint32(got))
	}

	// Walk the whole sequence from the initial value and confirm that the
	// reserved NoMessageID value is never produced.
	id := InitialMessageID
	for i := 0; i < 1<<12; i++ {
		if id == NoMessageID {
			t.Fatalf("sequence produced NoMessageID after %d steps", i)
		}
		id = id.Next()
	}
}

func TestCounterLastBeforeFirstTake(t *testing.T) {
	c := NewCounter()
	if got := c.Last(); got != StatusQueryInitialMessageID {
		t.Errorf(
			"Last() before first Take = %#x, want %#x",
			uint32(got), uint32(StatusQueryInitialMessageID),
		)
	}
	first := c.Take()
	if got := c.Last(); got != first {
		t.Errorf("Last() = %#x, want %#x", uint32(got), uint32(first))
	}
}

func TestCounterReset(t *testing.T) {
	c := NewCounter()
	for i := 0; i < 5; i++ {
		c.Take()
	}
	c.Reset()
	if got := c.Current(); got != InitialMessageID {
		t.Errorf("Current() after Reset = %#x, want %#x", uint32(got), uint32(InitialMessageID))
	}
	if got := c.Last(); got != StatusQueryInitialMessageID {
		t.Errorf(
			"Last() after Reset = %#x, want %#x",
			uint32(got), uint32(StatusQueryInitialMessageID),
		)
	}
}

// TestZeroCounterDoesNotStartAtZero guards the trap that a zero-value Counter
// would otherwise create: a sequence starting at 0x00000000 looks plausible and
// interoperates incorrectly.
func TestZeroCounterDoesNotStartAtZero(t *testing.T) {
	var c Counter
	if got := c.Take(); got != InitialMessageID {
		t.Errorf("zero Counter Take() = %#x, want %#x", uint32(got), uint32(InitialMessageID))
	}
}
