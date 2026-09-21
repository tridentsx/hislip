// Copyright (c) 2026 The hislip developers. All rights reserved.
// Project site: https://github.com/gotmc/hislip
// Use of this source code is governed by a MIT-style license that
// can be found in the LICENSE.txt file for the project.

// Package server implements the HiSLIP session state machine and the Device
// abstraction that a HiSLIP server presents to its instrument backend.
//
// Not yet implemented. This is Milestone 2 of the design specification.
//
// The package is device-side code and is compiled under TinyGo, so it is
// subject to the import restrictions enforced by TestDeviceImportGraph in the
// root package of this module. In particular it must not depend on net: the
// session works over the Stream interface, which a host adapter satisfies with
// net.Conn and a firmware adapter satisfies with a W5500 socket.
//
// The pieces that are easy to omit and expensive to omit:
//
//   - RMT-expected and RMT-delivered tracking, which is what Synchronized Mode
//     actually consists of, and which distinguishes the silent interrupted
//     error from the one that sends both Interrupted and AsyncInterrupted;
//   - MAV computed from the AsyncStatusQuery MessageID rather than from buffer
//     occupancy;
//   - the full four-message Device Clear sequence with its feature bitmap.
package server
