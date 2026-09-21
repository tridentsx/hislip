// Copyright (c) 2026 The hislip developers. All rights reserved.
// Project site: https://github.com/gotmc/hislip
// Use of this source code is governed by a MIT-style license that
// can be found in the LICENSE.txt file for the project.

package protocol

// MessageID correlates a client message with the response it generates.
//
// In Synchronized Mode the MessageID is how a response is attributed to the
// program message that produced it. The arithmetic is defined by IVI-6.1
// section 3.1.2 and is a common source of interoperability failure, so it is
// implemented once here rather than at each call site.
type MessageID uint32

// MessageID values defined by IVI-6.1.
const (
	// InitialMessageID is the value a client's counter takes at
	// initialization and after Device Clear.
	InitialMessageID MessageID = 0xffffff00

	// NoMessageID is sent by a server in a Data message when it cannot
	// identify the client message that produced the response, which happens
	// when the end-of-message is not at the end of the identified message.
	// A client does not correlate a Data message carrying this value.
	NoMessageID MessageID = 0xffffffff

	// StatusQueryInitialMessageID is the value a client places in an
	// AsyncStatusQuery issued after initialization or Device Clear but before
	// it has sent any Data, DataEnd or Trigger message. It is
	// InitialMessageID - 2.
	StatusQueryInitialMessageID MessageID = 0xfffffefe

	// messageIDIncrement is the step between successive MessageIDs.
	messageIDIncrement MessageID = 2
)

// Next returns the MessageID that follows m. Addition wraps, which is
// permitted.
func (m MessageID) Next() MessageID {
	return m + messageIDIncrement
}

// Previous returns the MessageID that precedes m.
func (m MessageID) Previous() MessageID {
	return m - messageIDIncrement
}

// Counter generates the MessageID sequence for one session.
//
// The zero Counter is not ready for use; call Reset first, or use NewCounter.
// This is deliberate, because a counter that silently started at zero would
// produce a sequence that looks plausible and interoperates incorrectly.
type Counter struct {
	current MessageID
	started bool
}

// NewCounter returns a Counter positioned at InitialMessageID.
func NewCounter() *Counter {
	c := &Counter{}
	c.Reset()
	return c
}

// Reset returns the counter to InitialMessageID. It is called at
// initialization and after Device Clear completes.
func (c *Counter) Reset() {
	c.current = InitialMessageID
	c.started = true
}

// Current returns the MessageID that Take will return next.
func (c *Counter) Current() MessageID {
	if !c.started {
		c.Reset()
	}
	return c.current
}

// Take returns the current MessageID and advances the counter. It is called
// when sending Data, DataEnd or Trigger.
func (c *Counter) Take() MessageID {
	id := c.Current()
	c.current = id.Next()
	return id
}

// Last returns the MessageID most recently returned by Take, which is the
// value a client places in an AsyncStatusQuery. Before the first Take it
// returns StatusQueryInitialMessageID, as required by IVI-6.1 section 6.14.
func (c *Counter) Last() MessageID {
	return c.Current().Previous()
}
