// Copyright (c) 2026 The hislip developers. All rights reserved.
// Project site: https://github.com/gotmc/hislip
// Use of this source code is governed by a MIT-style license that
// can be found in the LICENSE.txt file for the project.

package testdevice_test

import (
	"context"
	"errors"
	"testing"

	"github.com/gotmc/hislip/internal/testdevice"
	"github.com/gotmc/hislip/server"
)

func TestZeroDeviceIsUsable(t *testing.T) {
	ctx := context.Background()
	var d testdevice.Device

	if err := d.Write(ctx, []byte("*RST\n"), true); err != nil {
		t.Errorf("Write() error = %v", err)
	}
	if got, err := d.ReadStatusByte(ctx); err != nil || got != 0 {
		t.Errorf("ReadStatusByte() = %#x, %v; want 0x0, nil", got, err)
	}
	// With no response queued, Read reports the condition §17.3 exists for.
	if _, _, err := d.Read(ctx, make([]byte, 8)); !errors.Is(err, testdevice.ErrNoResponse) {
		t.Errorf("Read() error = %v, want ErrNoResponse", err)
	}
}

// TestOperationOrderIsRecorded is the property that matters. §47 requires that a
// Trigger not overtake a queued write, and a mock that only counted calls could
// not detect a violation.
func TestOperationOrderIsRecorded(t *testing.T) {
	ctx := context.Background()
	d := testdevice.New()

	if err := d.Write(ctx, []byte("CONF:VOLT\n"), true); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if err := d.Trigger(ctx); err != nil {
		t.Fatalf("Trigger() error = %v", err)
	}
	if err := d.Clear(ctx); err != nil {
		t.Fatalf("Clear() error = %v", err)
	}

	want := []testdevice.Op{testdevice.OpWrite, testdevice.OpTrigger, testdevice.OpClear}
	got := d.Ops()
	if len(got) != len(want) {
		t.Fatalf("Ops() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("Ops()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
	if n := d.Count(testdevice.OpTrigger); n != 1 {
		t.Errorf("Trigger called %d times, want exactly 1", n)
	}
}

// TestReadChunksLongResponse confirms a response longer than the caller's buffer
// arrives across several reads with end set only once, which is what drives the
// Data then DataEnd framing on the wire.
func TestReadChunksLongResponse(t *testing.T) {
	ctx := context.Background()
	d := testdevice.New([]byte("0123456789"))

	var assembled []byte
	buf := make([]byte, 4)
	ends := 0
	for i := 0; i < 10; i++ {
		n, end, err := d.Read(ctx, buf)
		if err != nil {
			t.Fatalf("Read() error = %v", err)
		}
		assembled = append(assembled, buf[:n]...)
		if end {
			ends++
			break
		}
	}
	if string(assembled) != "0123456789" {
		t.Errorf("assembled %q", assembled)
	}
	if ends != 1 {
		t.Errorf("end was reported %d times, want 1", ends)
	}
}

func TestResponsesAreConsumedInOrder(t *testing.T) {
	ctx := context.Background()
	d := testdevice.New([]byte("first"), []byte("second"))
	buf := make([]byte, 16)

	for _, want := range []string{"first", "second"} {
		n, end, err := d.Read(ctx, buf)
		if err != nil {
			t.Fatalf("Read() error = %v", err)
		}
		if !end {
			t.Errorf("end = false for a response that fits the buffer")
		}
		if got := string(buf[:n]); got != want {
			t.Errorf("read %q, want %q", got, want)
		}
	}
	if _, _, err := d.Read(ctx, buf); !errors.Is(err, testdevice.ErrNoResponse) {
		t.Errorf("Read() past the last response = %v, want ErrNoResponse", err)
	}
}

// TestClearAbandonsPartialResponse mirrors §13: a device clear cancels a pending
// read and discards buffered response bytes.
func TestClearAbandonsPartialResponse(t *testing.T) {
	ctx := context.Background()
	d := testdevice.New([]byte("0123456789"), []byte("next"))
	buf := make([]byte, 4)

	if _, end, err := d.Read(ctx, buf); err != nil || end {
		t.Fatalf("first Read() = end %v, err %v", end, err)
	}
	if err := d.Clear(ctx); err != nil {
		t.Fatalf("Clear() error = %v", err)
	}
	// The remainder of the interrupted response must not leak into the next one.
	n, end, err := d.Read(ctx, buf)
	if err != nil {
		t.Fatalf("Read() after Clear error = %v", err)
	}
	if got := string(buf[:n]); got != "next" || !end {
		t.Errorf("after Clear read %q end %v, want \"next\" true", got, end)
	}
}

func TestInjectedErrors(t *testing.T) {
	ctx := context.Background()
	sentinel := errors.New("bus fault")
	d := testdevice.New()
	d.Errors = map[testdevice.Op]error{
		testdevice.OpTrigger: sentinel,
		testdevice.OpClear:   sentinel,
	}

	if err := d.Trigger(ctx); !errors.Is(err, sentinel) {
		t.Errorf("Trigger() error = %v, want the injected error", err)
	}
	if err := d.Clear(ctx); !errors.Is(err, sentinel) {
		t.Errorf("Clear() error = %v, want the injected error", err)
	}
	// A failed operation is not recorded, so a test cannot mistake an attempt
	// for a success.
	if n := d.Count(testdevice.OpTrigger); n != 0 {
		t.Errorf("a failed Trigger was recorded %d times, want 0", n)
	}
	// Operations without an injected error still work.
	if err := d.Write(ctx, []byte("x"), true); err != nil {
		t.Errorf("Write() error = %v", err)
	}
}

func TestRemoteLocalRecordsMode(t *testing.T) {
	ctx := context.Background()
	d := testdevice.New()
	if err := d.RemoteLocal(ctx, server.EnableRemoteLockoutLocal); err != nil {
		t.Fatalf("RemoteLocal() error = %v", err)
	}
	calls := d.Calls()
	if len(calls) != 1 {
		t.Fatalf("recorded %d calls, want 1", len(calls))
	}
	if calls[0].Mode != server.EnableRemoteLockoutLocal {
		t.Errorf("Mode = %v, want EnableRemoteLockoutLocal", calls[0].Mode)
	}
}

func TestWrittenAccumulates(t *testing.T) {
	ctx := context.Background()
	d := testdevice.New()
	// A program message delivered in pieces, as Data then DataEnd would.
	if err := d.Write(ctx, []byte("MEAS:"), false); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if err := d.Write(ctx, []byte("VOLT?\n"), true); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if got := string(d.Written()); got != "MEAS:VOLT?\n" {
		t.Errorf("Written() = %q", got)
	}
	calls := d.Calls()
	if len(calls) != 2 {
		t.Fatalf("recorded %d writes, want 2", len(calls))
	}
	if calls[0].End {
		t.Error("first write recorded end true, want false")
	}
	if !calls[1].End {
		t.Error("second write recorded end false, want true")
	}
}

func TestReset(t *testing.T) {
	ctx := context.Background()
	d := testdevice.New([]byte("data"))
	if err := d.Write(ctx, []byte("x"), true); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	d.Reset()
	if len(d.Calls()) != 0 {
		t.Errorf("Calls() after Reset = %v, want empty", d.Calls())
	}
	if len(d.Written()) != 0 {
		t.Errorf("Written() after Reset = %q, want empty", d.Written())
	}
	// Responses are re-armed from the start.
	buf := make([]byte, 8)
	if n, _, err := d.Read(ctx, buf); err != nil || string(buf[:n]) != "data" {
		t.Errorf("Read() after Reset = %q, %v", buf[:n], err)
	}
}

func TestOptionalInterfaces(t *testing.T) {
	ctx := context.Background()
	d := testdevice.New()
	d.Name = "mock instrument"

	var info server.DeviceInfo = d
	if got := info.DeviceName(); got != "mock instrument" {
		t.Errorf("DeviceName() = %q", got)
	}

	var resetter server.DeviceResetter = d
	if err := resetter.InterfaceClear(ctx); err != nil {
		t.Fatalf("InterfaceClear() error = %v", err)
	}
	if n := d.Count(testdevice.OpInterfaceClear); n != 1 {
		t.Errorf("InterfaceClear recorded %d times, want 1", n)
	}
}
