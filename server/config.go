// Copyright (c) 2026 The hislip developers. All rights reserved.
// Project site: https://github.com/gotmc/hislip
// Use of this source code is governed by a MIT-style license that
// can be found in the LICENSE.txt file for the project.

package server

import (
	"errors"

	"github.com/gotmc/hislip/protocol"
)

// Config is a server profile.
//
// The zero Config is usable and yields the PoE-to-GPIB device profile of the
// design specification: one session, Synchronized Mode only, HiSLIP 1.0, and the
// conventional sub-address. Defaults are applied by New, so a caller that wants
// the profile need not restate it.
type Config struct {
	// SubAddresses are the sub-addresses this server answers to. A null
	// sub-address always selects the default logical instrument and need not be
	// listed. Empty means only the conventional "hislip0".
	SubAddresses []string

	// MaxVersion is the highest protocol version this server implements. It
	// defaults to 1.0 rather than 2.0, deliberately: the codec understands the
	// 2.0 message namespace, but the firmware must not claim 2.0 behaviour
	// before the additional 2.0 transactions are validated. See §4.
	MaxVersion protocol.Version

	// SupportsOverlap enables Overlap Mode. It defaults to false and the
	// PoE-to-GPIB profile requires it to stay false; see §11 and R-SRV-012.
	SupportsOverlap bool

	// MaxSessions is the number of concurrent logical sessions. It defaults to
	// 1. A second session is refused with a fatal error rather than stalled, so
	// that a user connecting from a second machine gets a diagnosable failure;
	// see R-FW-042.
	MaxSessions int

	// MaxRxPayload is the largest payload this server will accept, and the
	// value it advertises in the Maximum Message Size transaction. Zero means
	// the default. A logical instrument transfer may be arbitrarily larger than
	// this; only the per-packet payload is bounded. See §22 and R-FW-130.
	MaxRxPayload uint64

	// MaxTxPayload is the largest payload this server will send in one message.
	// Zero means the default.
	MaxTxPayload uint64

	// Policy decides whether a completed program message expects a response. It
	// defaults to SCPIQueryPolicy. See §48 and the discussion of the generic
	// GPIB-read problem in §17.
	Policy ResponsePolicy

	// VendorID is the two-character vendor identifier reported in
	// AsyncInitializeResponse. It defaults to zero, meaning unidentified,
	// because the project holds no IVI Foundation vendor ID yet; see
	// R-PROTO-055. Sending a guessed identifier would risk colliding with
	// another vendor's.
	VendorID [2]byte
}

// Default payload sizes. §22 suggests a conservative 4096 or 8192 bytes for the
// synchronous channel. 8192 is chosen so that a single packet holds a useful
// waveform fragment while still fitting comfortably in RP2350 SRAM alongside the
// GPIB response ring.
const (
	DefaultMaxRxPayload = 8192
	DefaultMaxTxPayload = 8192
)

// Config validation errors.
var (
	// ErrInvalidConfig indicates a Config that cannot be used.
	ErrInvalidConfig = errors.New("hislip: invalid server configuration")

	// ErrOverlapNotImplemented indicates a Config requesting Overlap Mode from
	// a build that does not implement it.
	ErrOverlapNotImplemented = errors.New("hislip: overlap mode is not implemented")
)

// withDefaults returns c with zero fields replaced by their defaults.
func (c Config) withDefaults() Config {
	if len(c.SubAddresses) == 0 {
		c.SubAddresses = []string{protocol.DefaultSubAddress}
	}
	if c.MaxVersion == (protocol.Version{}) {
		c.MaxVersion = protocol.Version10
	}
	if c.MaxSessions == 0 {
		c.MaxSessions = 1
	}
	if c.MaxRxPayload == 0 {
		c.MaxRxPayload = DefaultMaxRxPayload
	}
	if c.MaxTxPayload == 0 {
		c.MaxTxPayload = DefaultMaxTxPayload
	}
	return c
}

// validate reports whether c can be used. It is called by New after defaults
// are applied.
func (c Config) validate() error {
	if c.MaxSessions < 1 {
		return ErrInvalidConfig
	}
	if c.MaxVersion.Less(protocol.Version10) {
		return ErrInvalidConfig
	}
	if c.MaxRxPayload < protocol.HeaderSize || c.MaxTxPayload < protocol.HeaderSize {
		return ErrInvalidConfig
	}
	if c.SupportsOverlap {
		// Overlap Mode is not implemented. Refusing at construction is better
		// than advertising a mode and then failing to honour it, which would
		// leave a client waiting for responses that never arrive in order.
		return ErrOverlapNotImplemented
	}
	return nil
}

// acceptsSubAddress reports whether this server answers to the given
// sub-address.
//
// A null sub-address selects the only or default logical instrument and is
// always accepted, per §10. Comparison is case-sensitive: IVI-6.1 notes that
// "hislip" is case-sensitive in the VISA resource string.
func (c Config) acceptsSubAddress(sub string) bool {
	if sub == "" {
		return true
	}
	for _, s := range c.SubAddresses {
		if s == sub {
			return true
		}
	}
	return false
}

// preferOverlapBit returns the InitializeResponse control code advertising this
// server's mode preference. Zero means Synchronized Mode is preferred.
func (c Config) preferOverlapBit() uint8 {
	if c.SupportsOverlap {
		return protocol.ControlPreferOverlap
	}
	return 0
}

// featureBitmap returns the feature bitmap this server proposes and accepts
// during Device Clear negotiation.
//
// The server must support every capability it proposes, so a profile that does
// not implement Overlap Mode must hold bit 0 clear in both
// AsyncDeviceClearAcknowledge and DeviceClearAcknowledge, whatever the client
// requests. That is the entire implementation of the Overlap Mode prohibition;
// see R-SRV-012.
func (c Config) featureBitmap(clientRequest uint8) uint8 {
	var out uint8
	if c.SupportsOverlap && clientRequest&protocol.FeatureOverlapped != 0 {
		out |= protocol.FeatureOverlapped
	}
	// Encryption features are not implemented and are therefore never agreed,
	// regardless of the request.
	return out
}
