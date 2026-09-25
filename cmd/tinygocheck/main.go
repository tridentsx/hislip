// Copyright (c) 2026 The hislip developers. All rights reserved.
// Project site: https://github.com/tridentsx/hislip
// Use of this source code is governed by a MIT-style license that
// can be found in the LICENSE.txt file for the project.

// Command tinygocheck exists only so `just tinygo` has a real main package
// to build: tinygo build, unlike go build, requires linking an actual
// executable and refuses to just type-check a library package on its own
// ("expected main package to have name main"). This exercises the real
// dependency graph of protocol and server for an actual embedded target
// (see the Justfile) with a minimal stub Device, and does nothing else --
// it is not firmware.
package main

import (
	"context"

	"github.com/tridentsx/hislip/server"
)

type stubDevice struct{}

func (stubDevice) Write(ctx context.Context, p []byte, end bool) error { return nil }
func (stubDevice) Read(ctx context.Context, p []byte) (int, bool, error) {
	return 0, true, nil
}
func (stubDevice) Clear(ctx context.Context) error                                    { return nil }
func (stubDevice) Trigger(ctx context.Context) error                                  { return nil }
func (stubDevice) ReadStatusByte(ctx context.Context) (byte, error)                   { return 0, nil }
func (stubDevice) RemoteLocal(ctx context.Context, mode server.RemoteLocalMode) error { return nil }

var _ server.Device = stubDevice{}

func main() {
	if _, err := server.New(stubDevice{}, server.Config{}); err != nil {
		panic(err)
	}
	for {
	}
}
