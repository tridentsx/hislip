// Copyright (c) 2026 The hislip developers. All rights reserved.
// Project site: https://github.com/tridentsx/hislip
// Use of this source code is governed by a MIT-style license that
// can be found in the LICENSE.txt file for the project.

package protocol

// MessageType identifies a HiSLIP message. The numeric values are defined by
// IVI-6.1 Table 4, "Message Type Value Definitions".
type MessageType uint8

// Message types defined by IVI-6.1 Table 4.
const (
	Initialize                      MessageType = 0
	InitializeResponse              MessageType = 1
	FatalError                      MessageType = 2
	Error                           MessageType = 3
	AsyncLock                       MessageType = 4
	AsyncLockResponse               MessageType = 5
	Data                            MessageType = 6
	DataEnd                         MessageType = 7
	DeviceClearComplete             MessageType = 8
	DeviceClearAcknowledge          MessageType = 9
	AsyncRemoteLocalControl         MessageType = 10
	AsyncRemoteLocalResponse        MessageType = 11
	Trigger                         MessageType = 12
	Interrupted                     MessageType = 13
	AsyncInterrupted                MessageType = 14
	AsyncMaximumMessageSize         MessageType = 15
	AsyncMaximumMessageSizeResponse MessageType = 16
	AsyncInitialize                 MessageType = 17
	AsyncInitializeResponse         MessageType = 18
	AsyncDeviceClear                MessageType = 19
	AsyncServiceRequest             MessageType = 20
	AsyncStatusQuery                MessageType = 21
	AsyncStatusResponse             MessageType = 22
	AsyncDeviceClearAcknowledge     MessageType = 23
	AsyncLockInfo                   MessageType = 24
	AsyncLockInfoResponse           MessageType = 25
	GetDescriptors                  MessageType = 26
	GetDescriptorsResponse          MessageType = 27
	StartTLS                        MessageType = 28
	AsyncStartTLS                   MessageType = 29
	AsyncStartTLSResponse           MessageType = 30
	EndTLS                          MessageType = 31
	AsyncEndTLS                     MessageType = 32
	AsyncEndTLSResponse             MessageType = 33
	GetSaslMechanismList            MessageType = 34
	GetSaslMechanismListResponse    MessageType = 35
	AuthenticationStart             MessageType = 36
	AuthenticationExchange          MessageType = 37
	AuthenticationResult            MessageType = 38
)

// Bounds of the reserved and vendor-specific message type ranges.
const (
	// ReservedMin is the lowest message type reserved for future HiSLIP
	// extensions. Types from ReservedMin to VendorSpecificMin-1 are reserved.
	ReservedMin MessageType = 39

	// VendorSpecificMin and VendorSpecificMax bound the vendor-specific range.
	// Vendor-specific types are not namespaced by vendor ID, so a vendor
	// extension must negotiate before use. See the design specification, §17.4.
	VendorSpecificMin MessageType = 128
	VendorSpecificMax MessageType = 255
)

// Channel reports which of the two HiSLIP TCP connections a message is legal
// on.
type Channel uint8

// Channel values.
const (
	ChannelNone Channel = iota
	ChannelSynchronous
	ChannelAsynchronous
	ChannelEither
)

// Capability reports whether a message type is available on every server or
// only when the server advertises the Secure Connection capability.
type Capability uint8

// Capability values.
const (
	CapabilityGeneral Capability = iota
	CapabilitySecureConnection
)

// Control code bit definitions.
const (
	// ControlRMTDelivered is control code bit 0 of the client-to-server Data,
	// DataEnd, Trigger, AsyncStatusQuery, AsyncStartTLS and AsyncEndTLS
	// messages. It is set when this is the first RMT-delivered flag-carrying
	// message since the client delivered a response message terminator to its
	// application layer. See IVI-6.1 section 3.1.1.
	ControlRMTDelivered uint8 = 1 << 0

	// ControlPreferOverlap is control code bit 0 of InitializeResponse. Zero
	// means the server prefers Synchronized Mode.
	ControlPreferOverlap uint8 = 1 << 0
)

// Feature bitmap bits, carried in the control code of
// AsyncDeviceClearAcknowledge, DeviceClearComplete and DeviceClearAcknowledge.
// See IVI-6.1 Table 31.
const (
	// FeatureOverlapped selects Overlap Mode when set and Synchronized Mode
	// when clear.
	FeatureOverlapped uint8 = 1 << 0

	// FeatureEncryptionMandatory indicates encryption is mandatory.
	FeatureEncryptionMandatory uint8 = 1 << 1

	// FeatureInitialEncryption indicates the Establish Secure Connection
	// transaction must follow initialization.
	FeatureInitialEncryption uint8 = 1 << 2
)

// FatalErrorCode is the control code of a FatalError message. Values are
// defined by IVI-6.1 Table 14.
type FatalErrorCode uint8

// Fatal error codes defined by IVI-6.1 Table 14.
const (
	FatalUnidentified           FatalErrorCode = 0
	FatalPoorlyFormedHeader     FatalErrorCode = 1
	FatalChannelsNotEstablished FatalErrorCode = 2
	FatalInvalidInitSequence    FatalErrorCode = 3
	FatalMaxClientsExceeded     FatalErrorCode = 4
	FatalSecureConnectionFailed FatalErrorCode = 5

	// FatalReservedMin is the lowest code reserved for HiSLIP extensions.
	// Codes from FatalReservedMin to FatalDeviceDefinedMin-1 must not be used
	// by an implementation.
	FatalReservedMin FatalErrorCode = 6

	// FatalDeviceDefinedMin is the lowest device-defined fatal error code.
	FatalDeviceDefinedMin FatalErrorCode = 128
)

// ErrorCode is the control code of a non-fatal Error message. Values are
// defined by IVI-6.1 Table 16.
type ErrorCode uint8

// Non-fatal error codes defined by IVI-6.1 Table 16.
const (
	NonFatalUnidentified            ErrorCode = 0
	NonFatalUnrecognizedMessageType ErrorCode = 1
	NonFatalUnrecognizedControlCode ErrorCode = 2
	NonFatalUnrecognizedVendorMsg   ErrorCode = 3
	NonFatalMessageTooLarge         ErrorCode = 4
	NonFatalAuthenticationFailed    ErrorCode = 5

	// NonFatalReservedMin is the lowest code reserved for HiSLIP extensions.
	NonFatalReservedMin ErrorCode = 6

	// NonFatalDeviceDefinedMin is the lowest device-defined error code.
	NonFatalDeviceDefinedMin ErrorCode = 128
)

// DefaultPort is the conventional HiSLIP TCP port. It is a convention rather
// than a requirement, so an implementation must not infer the protocol from
// the port number.
const DefaultPort = 4880

// DefaultSubAddress is the conventional HiSLIP sub-address for the first
// logical instrument.
const DefaultSubAddress = "hislip0"

// messageInfo holds the per-type properties of Table 4.
type messageInfo struct {
	name       string
	channel    Channel
	capability Capability
	minVersion Version
}

// messages is indexed by message type. The zero value, with ChannelNone,
// marks a type that is not defined by the standard.
//
// DeviceClearAcknowledge is listed here as synchronous. IVI-6.1 Table 4 gives
// its channel as asynchronous, which contradicts the Device Clear transaction
// description in section 6.12, where step 7 of the client procedure reads
// "Wait for the server to respond with DeviceClearAcknowledge on the
// synchronous channel", and contradicts the message summary table. The
// transaction description is followed here. See the design specification, §4.4.
var messages = [...]messageInfo{
	Initialize:                      {"Initialize", ChannelSynchronous, CapabilityGeneral, Version10},
	InitializeResponse:              {"InitializeResponse", ChannelSynchronous, CapabilityGeneral, Version10},
	FatalError:                      {"FatalError", ChannelEither, CapabilityGeneral, Version10},
	Error:                           {"Error", ChannelEither, CapabilityGeneral, Version10},
	AsyncLock:                       {"AsyncLock", ChannelAsynchronous, CapabilityGeneral, Version10},
	AsyncLockResponse:               {"AsyncLockResponse", ChannelAsynchronous, CapabilityGeneral, Version10},
	Data:                            {"Data", ChannelSynchronous, CapabilityGeneral, Version10},
	DataEnd:                         {"DataEnd", ChannelSynchronous, CapabilityGeneral, Version10},
	DeviceClearComplete:             {"DeviceClearComplete", ChannelSynchronous, CapabilityGeneral, Version10},
	DeviceClearAcknowledge:          {"DeviceClearAcknowledge", ChannelSynchronous, CapabilityGeneral, Version10},
	AsyncRemoteLocalControl:         {"AsyncRemoteLocalControl", ChannelAsynchronous, CapabilityGeneral, Version10},
	AsyncRemoteLocalResponse:        {"AsyncRemoteLocalResponse", ChannelAsynchronous, CapabilityGeneral, Version10},
	Trigger:                         {"Trigger", ChannelSynchronous, CapabilityGeneral, Version10},
	Interrupted:                     {"Interrupted", ChannelSynchronous, CapabilityGeneral, Version10},
	AsyncInterrupted:                {"AsyncInterrupted", ChannelAsynchronous, CapabilityGeneral, Version10},
	AsyncMaximumMessageSize:         {"AsyncMaximumMessageSize", ChannelAsynchronous, CapabilityGeneral, Version10},
	AsyncMaximumMessageSizeResponse: {"AsyncMaximumMessageSizeResponse", ChannelAsynchronous, CapabilityGeneral, Version10},
	AsyncInitialize:                 {"AsyncInitialize", ChannelAsynchronous, CapabilityGeneral, Version10},
	AsyncInitializeResponse:         {"AsyncInitializeResponse", ChannelAsynchronous, CapabilityGeneral, Version10},
	AsyncDeviceClear:                {"AsyncDeviceClear", ChannelAsynchronous, CapabilityGeneral, Version10},
	AsyncServiceRequest:             {"AsyncServiceRequest", ChannelAsynchronous, CapabilityGeneral, Version10},
	AsyncStatusQuery:                {"AsyncStatusQuery", ChannelAsynchronous, CapabilityGeneral, Version10},
	AsyncStatusResponse:             {"AsyncStatusResponse", ChannelAsynchronous, CapabilityGeneral, Version10},
	AsyncDeviceClearAcknowledge:     {"AsyncDeviceClearAcknowledge", ChannelAsynchronous, CapabilityGeneral, Version10},
	AsyncLockInfo:                   {"AsyncLockInfo", ChannelAsynchronous, CapabilityGeneral, Version10},
	AsyncLockInfoResponse:           {"AsyncLockInfoResponse", ChannelAsynchronous, CapabilityGeneral, Version10},
	GetDescriptors:                  {"GetDescriptors", ChannelEither, CapabilityGeneral, Version20},
	GetDescriptorsResponse:          {"GetDescriptorsResponse", ChannelEither, CapabilityGeneral, Version20},
	StartTLS:                        {"StartTLS", ChannelSynchronous, CapabilitySecureConnection, Version20},
	AsyncStartTLS:                   {"AsyncStartTLS", ChannelAsynchronous, CapabilitySecureConnection, Version20},
	AsyncStartTLSResponse:           {"AsyncStartTLSResponse", ChannelAsynchronous, CapabilitySecureConnection, Version20},
	EndTLS:                          {"EndTLS", ChannelSynchronous, CapabilitySecureConnection, Version20},
	AsyncEndTLS:                     {"AsyncEndTLS", ChannelAsynchronous, CapabilitySecureConnection, Version20},
	AsyncEndTLSResponse:             {"AsyncEndTLSResponse", ChannelAsynchronous, CapabilitySecureConnection, Version20},
	GetSaslMechanismList:            {"GetSaslMechanismList", ChannelSynchronous, CapabilitySecureConnection, Version20},
	GetSaslMechanismListResponse:    {"GetSaslMechanismListResponse", ChannelSynchronous, CapabilitySecureConnection, Version20},
	AuthenticationStart:             {"AuthenticationStart", ChannelSynchronous, CapabilitySecureConnection, Version20},
	AuthenticationExchange:          {"AuthenticationExchange", ChannelSynchronous, CapabilitySecureConnection, Version20},
	AuthenticationResult:            {"AuthenticationResult", ChannelSynchronous, CapabilitySecureConnection, Version20},
}

// Defined reports whether t is a message type defined by IVI-6.1. Reserved
// types in the range 39 to 127 are not defined, and neither are
// vendor-specific types, whose meaning is agreed out of band.
func (t MessageType) Defined() bool {
	return int(t) < len(messages) && messages[t].channel != ChannelNone
}

// VendorSpecific reports whether t lies in the vendor-specific range.
func (t MessageType) VendorSpecific() bool {
	return t >= VendorSpecificMin
}

// Reserved reports whether t lies in the range reserved for future HiSLIP
// extensions. A reserved type must be rejected, never interpreted.
func (t MessageType) Reserved() bool {
	return t >= ReservedMin && t < VendorSpecificMin
}

// Channel returns the channel on which t is legal. It returns ChannelEither
// for vendor-specific types, whose channel is defined by the vendor, and
// ChannelNone for reserved types.
func (t MessageType) Channel() Channel {
	if t.VendorSpecific() {
		return ChannelEither
	}
	if !t.Defined() {
		return ChannelNone
	}
	return messages[t].channel
}

// LegalOn reports whether t may be received on the given channel.
func (t MessageType) LegalOn(c Channel) bool {
	if c != ChannelSynchronous && c != ChannelAsynchronous {
		return false
	}
	switch t.Channel() {
	case ChannelEither:
		return true
	case c:
		return true
	default:
		return false
	}
}

// MinVersion returns the lowest negotiated protocol version at which t may be
// used. It returns the zero Version for types that are not defined.
func (t MessageType) MinVersion() Version {
	if !t.Defined() {
		return Version{}
	}
	return messages[t].minVersion
}

// Capability reports whether t requires the Secure Connection capability.
func (t MessageType) Capability() Capability {
	if !t.Defined() {
		return CapabilityGeneral
	}
	return messages[t].capability
}

// String returns the IVI-6.1 name of the message type, or "VendorSpecific" or
// "Reserved" for types in those ranges.
func (t MessageType) String() string {
	switch {
	case t.Defined():
		return messages[t].name
	case t.VendorSpecific():
		return "VendorSpecific"
	default:
		return "Reserved"
	}
}
