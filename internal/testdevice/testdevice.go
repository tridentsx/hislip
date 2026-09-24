// Copyright (c) 2026 The hislip developers. All rights reserved.
// Project site: https://github.com/tridentsx/hislip
// Use of this source code is governed by a MIT-style license that
// can be found in the LICENSE.txt file for the project.

// Package testdevice provides a mock instrument for server tests.
//
// It records every operation in order, so a test can assert both that an
// operation happened and that it happened exactly once and in the right place
// relative to others. Ordering matters here more than in most mocks: the whole
// point of §47's channel-ordered dispatch is that a Trigger must not overtake a
// queued write, and a mock that only counted calls could not detect that.
//
// This package is test support and is not device-side code.
package testdevice

import (
	"bytes"
	"context"
	"errors"
	"sync"

	"github.com/tridentsx/hislip/server"
)

// Op is the kind of a recorded operation.
type Op string

// Recorded operation kinds.
const (
	OpWrite          Op = "write"
	OpRead           Op = "read"
	OpClear          Op = "clear"
	OpTrigger        Op = "trigger"
	OpReadStatusByte Op = "status"
	OpRemoteLocal    Op = "remotelocal"
	OpInterfaceClear Op = "ifc"
)

// Call is one recorded operation.
type Call struct {
	Op Op

	// Data is the bytes written, for OpWrite, or returned, for OpRead.
	Data []byte

	// End is the end flag of a write, or the end flag returned by a read.
	End bool

	// Mode is the mode of OpRemoteLocal.
	Mode server.RemoteLocalMode
}

// Device is a mock instrument.
//
// The zero Device is usable: it accepts writes, returns no data from reads, and
// reports a zero status byte.
type Device struct {
	mu sync.Mutex

	// Responses are returned by successive Read calls. Each entry is one
	// response; a response longer than the caller's buffer is delivered across
	// several Read calls with End false until its last chunk.
	Responses [][]byte

	// Status is returned by ReadStatusByte.
	Status byte

	// Errors, when a key is present, makes that operation fail.
	Errors map[Op]error

	// Name is returned by DeviceName.
	Name string

	calls    []Call
	written  bytes.Buffer
	pending  []byte
	respIdx  int
	inFlight bool
}

// New returns a Device that will return the given responses in order.
func New(responses ...[]byte) *Device {
	return &Device{Responses: responses}
}

// ErrNoResponse is returned by Read when the device has no response queued. It
// stands in for a GPIB read that timed out, which is the condition §17.3 defines
// behaviour for.
var ErrNoResponse = errors.New("testdevice: no response queued")

func (d *Device) fail(op Op) error {
	if d.Errors == nil {
		return nil
	}
	return d.Errors[op]
}

func (d *Device) record(c Call) {
	d.calls = append(d.calls, c)
}

// Write implements server.Device.
func (d *Device) Write(_ context.Context, p []byte, end bool) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.fail(OpWrite); err != nil {
		return err
	}
	d.written.Write(p)
	d.record(Call{Op: OpWrite, Data: append([]byte(nil), p...), End: end})
	return nil
}

// Read implements server.Device. A response longer than p is delivered across
// several calls, with End set only on the final chunk.
func (d *Device) Read(_ context.Context, p []byte) (int, bool, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.fail(OpRead); err != nil {
		return 0, false, err
	}
	if !d.inFlight {
		if d.respIdx >= len(d.Responses) {
			return 0, false, ErrNoResponse
		}
		d.pending = d.Responses[d.respIdx]
		d.respIdx++
		d.inFlight = true
	}
	n := copy(p, d.pending)
	d.pending = d.pending[n:]
	end := len(d.pending) == 0
	if end {
		d.inFlight = false
	}
	d.record(Call{Op: OpRead, Data: append([]byte(nil), p[:n]...), End: end})
	return n, end, nil
}

// Clear implements server.Device.
func (d *Device) Clear(context.Context) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.fail(OpClear); err != nil {
		return err
	}
	// A device clear abandons any partially delivered response.
	d.pending = nil
	d.inFlight = false
	d.record(Call{Op: OpClear})
	return nil
}

// Trigger implements server.Device.
func (d *Device) Trigger(context.Context) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.fail(OpTrigger); err != nil {
		return err
	}
	d.record(Call{Op: OpTrigger})
	return nil
}

// ReadStatusByte implements server.Device.
func (d *Device) ReadStatusByte(context.Context) (byte, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.fail(OpReadStatusByte); err != nil {
		return 0, err
	}
	d.record(Call{Op: OpReadStatusByte, Data: []byte{d.Status}})
	return d.Status, nil
}

// RemoteLocal implements server.Device.
func (d *Device) RemoteLocal(_ context.Context, mode server.RemoteLocalMode) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.fail(OpRemoteLocal); err != nil {
		return err
	}
	d.record(Call{Op: OpRemoteLocal, Mode: mode})
	return nil
}

// InterfaceClear implements server.DeviceResetter.
func (d *Device) InterfaceClear(context.Context) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.fail(OpInterfaceClear); err != nil {
		return err
	}
	d.record(Call{Op: OpInterfaceClear})
	return nil
}

// DeviceName implements server.DeviceInfo.
func (d *Device) DeviceName() string { return d.Name }

// Calls returns the recorded operations in order.
func (d *Device) Calls() []Call {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]Call(nil), d.calls...)
}

// Ops returns just the kinds of the recorded operations, in order, which is the
// form most assertions want.
func (d *Device) Ops() []Op {
	d.mu.Lock()
	defer d.mu.Unlock()
	ops := make([]Op, len(d.calls))
	for i, c := range d.calls {
		ops[i] = c.Op
	}
	return ops
}

// Count returns how many times an operation was performed.
func (d *Device) Count(op Op) int {
	d.mu.Lock()
	defer d.mu.Unlock()
	n := 0
	for _, c := range d.calls {
		if c.Op == op {
			n++
		}
	}
	return n
}

// Written returns everything passed to Write, concatenated.
func (d *Device) Written() []byte {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.written.Bytes()
}

// Reset discards recorded calls and queued responses.
func (d *Device) Reset() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.calls = nil
	d.written.Reset()
	d.pending = nil
	d.respIdx = 0
	d.inFlight = false
}

// Compile-time proof that the mock satisfies the interfaces it claims.
var (
	_ server.Device         = (*Device)(nil)
	_ server.DeviceResetter = (*Device)(nil)
	_ server.DeviceInfo     = (*Device)(nil)
)
