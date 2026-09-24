// Copyright (c) 2026 The hislip developers. All rights reserved.
// Project site: https://github.com/tridentsx/hislip
// Use of this source code is governed by a MIT-style license that
// can be found in the LICENSE.txt file for the project.

// Package client implements a HiSLIP client for desktop and server use.
//
// Not yet implemented. This is Milestone 1 of the design specification.
//
// Unlike protocol and server, this package is host-only and may use net and the
// rest of the standard library freely. It must never be imported by device-side
// code; TestDeviceImportGraph in the root package of this module enforces that.
//
// A HiSLIP client is required by IVI-6.1 to support both Synchronized and
// Overlap Mode, whereas a server may implement either or both. This client
// therefore implements both, even though the PoE-to-GPIB device profile it was
// written alongside is Synchronized-only.
package client
