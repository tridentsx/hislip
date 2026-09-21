// Copyright (c) 2026 The hislip developers. All rights reserved.
// Project site: https://github.com/gotmc/hislip
// Use of this source code is governed by a MIT-style license that
// can be found in the LICENSE.txt file for the project.

// Package gotmc implements an optional HiSLIP vendor extension that lets a
// client request a read explicitly.
//
// Not yet implemented. This is Milestone 7 of the design specification.
//
// The extension exists because a generic GPIB bridge cannot always know when
// the application has called viRead. A native HiSLIP instrument knows when its
// parser has produced response data; a bridge in front of a pre-SCPI instrument
// does not, and inferring it from query syntax is unreliable for exactly the
// old instruments that most need bridging.
//
// Vendor-specific message types are not namespaced by vendor ID, so the
// extension must negotiate before use. A server that does not implement it
// answers ExtHello with non-fatal Error code 3, Unrecognized Vendor Defined
// Message, which is the negative result the client needs. The extension must
// never be sent unconditionally to a third-party instrument, and it is disabled
// by default until the project holds an IVI Foundation vendor ID.
package gotmc
