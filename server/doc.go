// Copyright (c) 2026 The hislip developers. All rights reserved.
// Project site: https://github.com/tridentsx/hislip
// Use of this source code is governed by a MIT-style license that
// can be found in the LICENSE.txt file for the project.

// Package server implements the HiSLIP session state machine and the Device
// abstraction that a HiSLIP server presents to its instrument backend.
//
// This is Milestone 2 of the design specification, in progress. Implemented so
// far: the Stream and Device abstractions, the Synchronized Mode RMT mechanism,
// status byte and MAV computation, and the error model. Not yet implemented: the
// session lifecycle, initialization, operation dispatch, Device Clear, locks and
// the Interrupted transaction.
//
// # Device-side constraints
//
// This package is compiled for microcontroller targets under TinyGo and is
// subject to the import restrictions enforced by TestDeviceImportGraph in the
// root package of this module. In particular it must not depend on net: a
// session works over the Stream interface, which a host adapter satisfies with
// net.Conn and a firmware adapter with a W5500 socket.
//
// # What Synchronized Mode actually consists of
//
// It is tempting to read "Synchronized Mode" as merely "the client waits for
// each response", and to conclude that a server which answers in order is
// therefore conformant. It is not. IVI-6.1 section 3.1.1 requires two
// mechanisms, both of which this package implements and neither of which is
// visible in the message flow of a well-behaved client:
//
// RMT tracks RMT-expected against the client's RMT-delivered flag, and detects
// the case where a client sent a new program message without reading the
// previous response. See the documentation on RMT for the two interrupted errors
// this produces, which differ in whether the client is told.
//
// Status computes the message-available bit from the MessageID carried by
// AsyncStatusQuery, not from whether the server holds bytes. A bridge that
// reports MAV from buffer occupancy alone will disagree with a conforming client
// about when a response exists.
//
// # A note for test authors
//
// internal/testdevice imports this package, so an in-package test here cannot
// import it without creating an import cycle. Tests that need the mock device
// must use the external test package, "package server_test". The tests in this
// file's directory that exercise RMT, Status and the mode table need no mock and
// so remain in-package.
package server
