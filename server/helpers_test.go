// Copyright (c) 2026 The hislip developers. All rights reserved.
// Project site: https://github.com/tridentsx/hislip
// Use of this source code is governed by a MIT-style license that
// can be found in the LICENSE.txt file for the project.

package server

import (
	"context"
	"testing"
)

// nopDevice is a minimal Device for in-package tests.
//
// internal/testdevice imports this package, so an in-package test cannot use it
// without an import cycle. Tests that need a recording mock belong in the
// external test package, "package server_test"; tests of the protocol state
// machinery need only something that satisfies the interface, which this is.
type nopDevice struct {
	status byte
}

func (d *nopDevice) Write(context.Context, []byte, bool) error { return nil }

func (d *nopDevice) Read(context.Context, []byte) (int, bool, error) { return 0, true, nil }

func (d *nopDevice) Clear(context.Context) error { return nil }

func (d *nopDevice) Trigger(context.Context) error { return nil }

func (d *nopDevice) ReadStatusByte(context.Context) (byte, error) { return d.status, nil }

func (d *nopDevice) RemoteLocal(context.Context, RemoteLocalMode) error { return nil }

var _ Device = (*nopDevice)(nil)

// newTestServer returns a server with the default profile.
func newTestServer(t *testing.T) *Server {
	t.Helper()
	srv, err := New(&nopDevice{}, Config{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return srv
}
