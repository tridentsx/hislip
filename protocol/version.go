// Copyright (c) 2026 The hislip developers. All rights reserved.
// Project site: https://github.com/tridentsx/hislip
// Use of this source code is governed by a MIT-style license that
// can be found in the LICENSE.txt file for the project.

package protocol

import "strconv"

// Version is a HiSLIP protocol version of the form <major>.<minor>.
//
// The version travels in the most significant 16 bits of the 32-bit message
// parameter: the major number is the first byte and the minor number the
// second, both as binary 8-bit integers. See IVI-6.1 section 6.1.
type Version struct {
	Major uint8
	Minor uint8
}

// Protocol versions of interest.
var (
	// Version10 is HiSLIP 1.0.
	Version10 = Version{Major: 1, Minor: 0}

	// Version11 is HiSLIP 1.1.
	Version11 = Version{Major: 1, Minor: 1}

	// Version20 is HiSLIP 2.0, which adds the secure connection message types.
	Version20 = Version{Major: 2, Minor: 0}
)

// Less reports whether v precedes other.
func (v Version) Less(other Version) bool {
	if v.Major != other.Major {
		return v.Major < other.Major
	}
	return v.Minor < other.Minor
}

// AtLeast reports whether v is other or later.
func (v Version) AtLeast(other Version) bool {
	return !v.Less(other)
}

// String returns the version as "major.minor".
func (v Version) String() string {
	return strconv.Itoa(int(v.Major)) + "." + strconv.Itoa(int(v.Minor))
}

// Negotiate returns the version a server should accept given the highest
// version the client supports and the highest the server implements. It is the
// lower of the two.
func Negotiate(client, server Version) Version {
	if client.Less(server) {
		return client
	}
	return server
}

// upperWord returns the version encoded into the upper 16 bits of a message
// parameter.
func (v Version) upperWord() uint32 {
	return uint32(v.Major)<<24 | uint32(v.Minor)<<16
}

// versionFromParameter extracts a version from the upper 16 bits of a message
// parameter.
func versionFromParameter(p uint32) Version {
	return Version{Major: uint8(p >> 24), Minor: uint8(p >> 16)}
}

// EncodeInitializeParameter builds the message parameter of an Initialize
// message from the highest version the client supports and its two-character
// vendor ID.
func EncodeInitializeParameter(v Version, vendorID [2]byte) uint32 {
	return v.upperWord() | uint32(vendorID[0])<<8 | uint32(vendorID[1])
}

// DecodeInitializeParameter splits the message parameter of an Initialize
// message into the client protocol version and the client vendor ID.
func DecodeInitializeParameter(p uint32) (Version, [2]byte) {
	return versionFromParameter(p), [2]byte{uint8(p >> 8), uint8(p)}
}

// EncodeInitializeResponseParameter builds the message parameter of an
// InitializeResponse message from the negotiated version and the session ID.
func EncodeInitializeResponseParameter(v Version, sessionID uint16) uint32 {
	return v.upperWord() | uint32(sessionID)
}

// DecodeInitializeResponseParameter splits the message parameter of an
// InitializeResponse message into the negotiated version and the session ID.
func DecodeInitializeResponseParameter(p uint32) (Version, uint16) {
	return versionFromParameter(p), uint16(p)
}
