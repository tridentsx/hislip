// Copyright (c) 2026 The hislip developers. All rights reserved.
// Project site: https://github.com/tridentsx/hislip
// Use of this source code is governed by a MIT-style license that
// can be found in the LICENSE.txt file for the project.

package server

import "context"

// Device is the instrument behind a HiSLIP server.
//
// The server must not know that the device is GPIB, or USB, or a simulation.
// Everything HiSLIP-specific stays above this line; everything bus-specific
// stays below it. See the design specification, §8.
//
// Two things deliberately absent from this interface:
//
// MAV. The message-available bit is a function of the MessageID carried by
// AsyncStatusQuery, which is not visible here. It is computed in the server
// layer and merged into whatever ReadStatusByte returns. A backend must never
// be asked to compute MAV; see §11.4.1 and R-SYNC-033.
//
// Read parameters. Read takes no maximum-byte count or terminator, because the
// standard HiSLIP path has no way for a client to express them. Backends that
// need them implement ReadRequester, which the vendor extension of §17.4 drives.
type Device interface {
	// Write delivers bytes of a program message. end reports whether this is
	// the end of the message: for a GPIB backend, end true means the final byte
	// is transferred with EOI asserted. A write with end true and no bytes
	// still terminates the logical input message.
	Write(ctx context.Context, p []byte, end bool) error

	// Read fills p with response bytes. end reports that the response is
	// complete, which for a GPIB backend means EOI was seen with the final byte
	// or a configured EOS terminator was accepted.
	Read(ctx context.Context, p []byte) (n int, end bool, err error)

	// Clear discards device state for a HiSLIP Device Clear. A GPIB backend
	// maps this to Selected Device Clear for the configured instrument, not to
	// the universal Device Clear command; see §13.
	Clear(ctx context.Context) error

	// Trigger maps to GPIB Group Execute Trigger; see §14.
	Trigger(ctx context.Context) error

	// ReadStatusByte returns the device's status byte, for a GPIB backend by
	// serial poll. The server may adjust bit 4 before reporting it; see §16.3.
	ReadStatusByte(ctx context.Context) (byte, error)

	// RemoteLocal applies a remote/local state change; see §15 and Effects.
	RemoteLocal(ctx context.Context, mode RemoteLocalMode) error
}

// RemoteLocalMode is the control code of an AsyncRemoteLocalControl message.
// Values are defined by IVI-6.1 Table 25.
type RemoteLocalMode uint8

// Remote/local modes, with the corresponding VISA viGpibControlREN mode.
const (
	// DisableRemote is VI_GPIB_REN_DEASSERT.
	DisableRemote RemoteLocalMode = 0

	// EnableRemote is VI_GPIB_REN_ASSERT.
	EnableRemote RemoteLocalMode = 1

	// DisableRemoteGoToLocal is VI_GPIB_REN_DEASSERT_GTL.
	DisableRemoteGoToLocal RemoteLocalMode = 2

	// EnableRemoteGoToRemote is VI_GPIB_REN_ASSERT_ADDRESS.
	EnableRemoteGoToRemote RemoteLocalMode = 3

	// EnableRemoteLockoutLocal is VI_GPIB_REN_ASSERT_LLO.
	EnableRemoteLockoutLocal RemoteLocalMode = 4

	// EnableRemoteGoToRemoteLockoutLocal is VI_GPIB_REN_ASSERT_ADDRESS_LLO.
	EnableRemoteGoToRemoteLockoutLocal RemoteLocalMode = 5

	// GoToLocal is VI_GPIB_REN_ADDRESS_GTL. It changes the remote state without
	// changing remote enable.
	GoToLocal RemoteLocalMode = 6

	// maxRemoteLocalMode is the highest defined mode.
	maxRemoteLocalMode = GoToLocal
)

// Effect is what a remote/local mode does to one state bit.
type Effect uint8

// Effect values. EffectNoChange is the "nc" of IVI-6.1 Table 25 and is not the
// same as EffectFalse: a backend must leave the state alone rather than clear
// it.
const (
	EffectNoChange Effect = iota
	EffectFalse
	EffectTrue
)

// Effects is the state change a remote/local mode requires, transcribed from
// IVI-6.1 Table 25.
//
// This exists so that a GPIB backend has something testable to implement
// against. §15 forbids emulating remote/local by sending SCPI strings such as
// SYST:REM, because many legacy instruments do not implement them, which leaves
// the backend needing an exact statement of what each mode means in terms of
// REN, local lockout and the addressed-to-listen state.
type Effects struct {
	RemoteEnable Effect
	LocalLockout Effect
	Remote       Effect
}

// remoteLocalEffects is indexed by mode.
//
// Modes 0 and 2 have identical effects in Table 25. That is what the table says,
// and it is not an error here: the distinction between them is in the VISA call
// that produced them, not in the resulting bus state.
var remoteLocalEffects = [maxRemoteLocalMode + 1]Effects{
	DisableRemote:                      {EffectFalse, EffectFalse, EffectFalse},
	EnableRemote:                       {EffectTrue, EffectNoChange, EffectNoChange},
	DisableRemoteGoToLocal:             {EffectFalse, EffectFalse, EffectFalse},
	EnableRemoteGoToRemote:             {EffectTrue, EffectNoChange, EffectTrue},
	EnableRemoteLockoutLocal:           {EffectTrue, EffectTrue, EffectNoChange},
	EnableRemoteGoToRemoteLockoutLocal: {EffectTrue, EffectTrue, EffectTrue},
	GoToLocal:                          {EffectNoChange, EffectNoChange, EffectFalse},
}

// Valid reports whether m is a mode defined by IVI-6.1 Table 25. An
// undefined mode must be answered with a non-fatal Error, control code 2,
// "Unrecognized control code".
func (m RemoteLocalMode) Valid() bool {
	return m <= maxRemoteLocalMode
}

// Effects returns the state change m requires. It returns the zero Effects,
// meaning no change to anything, for an undefined mode; callers must check
// Valid first.
func (m RemoteLocalMode) Effects() Effects {
	if !m.Valid() {
		return Effects{}
	}
	return remoteLocalEffects[m]
}

// String returns a short description of the mode.
func (m RemoteLocalMode) String() string {
	switch m {
	case DisableRemote:
		return "disable remote"
	case EnableRemote:
		return "enable remote"
	case DisableRemoteGoToLocal:
		return "disable remote and go to local"
	case EnableRemoteGoToRemote:
		return "enable remote and go to remote"
	case EnableRemoteLockoutLocal:
		return "enable remote and lock out local"
	case EnableRemoteGoToRemoteLockoutLocal:
		return "enable remote, go to remote, and set local lockout"
	case GoToLocal:
		return "go to local without changing remote enable"
	default:
		return "undefined remote/local mode"
	}
}

// ServiceRequestSource is an optional Device capability. The byte delivered is
// the status byte to report in AsyncServiceRequest.
//
// A fixed capacity of one is sufficient, and is required rather than merely
// permitted: IVI-6.1 section 6.13 says no bit of the reported status register is
// cleared, and the client clears RQS by performing AsyncStatusQuery. A further
// service request must therefore not be sent until the client has done so, so
// repeated events coalesce by specification. See §16.2 and R-SRV-023.
type ServiceRequestSource interface {
	ServiceRequests() <-chan byte
}

// DeviceResetter is an optional Device capability for interface-level recovery,
// which HiSLIP does not expose but the adapter needs. For a GPIB backend this
// is IFC, which is a bus-wide operation distinct from the Selected Device Clear
// that Device.Clear performs.
type DeviceResetter interface {
	InterfaceClear(ctx context.Context) error
}

// DeviceInfo is an optional Device capability supplying a name for diagnostics
// and for the mDNS TXT records of §44.
type DeviceInfo interface {
	DeviceName() string
}

// ReadRequester is an optional Device capability for backends that can accept
// explicit read parameters, which the standard HiSLIP path cannot express. It
// is driven by the vendor extension of §17.4.
type ReadRequester interface {
	ReadRequest(ctx context.Context, p []byte, opts ReadOptions) (n int, end bool, err error)
}

// ReadOptions are the parameters of an explicit read request.
type ReadOptions struct {
	// MaxBytes limits the read. Zero means the server policy decides.
	MaxBytes uint32

	// UseEOI accepts EOI as message termination.
	UseEOI bool

	// UseTermChar accepts TermChar as message termination.
	UseTermChar bool

	// SuppressTermChar removes an accepted terminator from the returned data.
	SuppressTermChar bool

	// TermChar is the terminator, valid only when UseTermChar is set.
	TermChar byte
}
