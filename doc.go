// Copyright (c) 2026 The hislip developers. All rights reserved.
// Project site: https://github.com/gotmc/hislip
// Use of this source code is governed by a MIT-style license that
// can be found in the LICENSE.txt file for the project.

// Package hislip provides VISA resource parsing for the HiSLIP transport and
// holds the module-wide invariants.
//
// The implementation is split so that the device-side code can be compiled for
// microcontrollers with TinyGo:
//
//	protocol/          wire format; TinyGo-safe
//	server/            session state machine and Device interface; TinyGo-safe
//	client/            desktop client; uses net
//	extension/gotmc/   optional vendor extension for explicit reads
//
// Firmware imports only protocol and server. That boundary is enforced by
// TestDeviceImportGraph in this package, which walks the transitive import
// graph of the device-side packages and fails when it reaches anything outside
// a small allowlist.
//
// HiSLIP is specified by IVI-6.1, "IVI High-Speed LAN Instrument Protocol",
// revision 2.0.
package hislip
