// Copyright (c) 2026 The hislip developers. All rights reserved.
// Project site: https://github.com/tridentsx/hislip
// Use of this source code is governed by a MIT-style license that
// can be found in the LICENSE.txt file for the project.

package server

import "time"

// Stream is one of the two TCP connections of a HiSLIP session.
//
// The server core depends on this rather than on net.Conn so that the same
// session state machine runs over a host socket, a W5500 socket on a
// microcontroller, and an in-memory pipe in tests. TinyGo's net package is in
// fact available on the target, so this interface exists for testability and
// host/target symmetry rather than from necessity. See the design
// specification, §7 and the note in §5.1.
type Stream interface {
	Read(p []byte) (int, error)
	Write(p []byte) (int, error)
	Close() error
}

// DeadlineStream is a Stream that supports I/O deadlines.
//
// A transport that cannot set deadlines is still usable, but the session must
// then rely on the timeout categories of §49 enforced at a higher layer, and it
// cannot interrupt a read that is blocked in the transport. Implementations
// SHOULD provide this where the underlying transport allows it.
//
// The channel loop will type-assert for this once it exists. Until then nothing
// in this package calls SetDeadline, which is why there is no helper here yet.
type DeadlineStream interface {
	Stream
	SetDeadline(t time.Time) error
}
