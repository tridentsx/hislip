// Copyright (c) 2026 The hislip developers. All rights reserved.
// Project site: https://github.com/tridentsx/hislip
// Use of this source code is governed by a MIT-style license that
// can be found in the LICENSE.txt file for the project.

package protocol

import "testing"

// TestMessageTypeTable is transcribed from IVI-6.1 Table 4 independently of
// constants.go, so that a transcription error in one has to be made twice to
// go unnoticed.
func TestMessageTypeTable(t *testing.T) {
	type want struct {
		value   uint8
		channel Channel
		secure  bool
		v20     bool
	}
	table := map[MessageType]want{
		Initialize:                      {0, ChannelSynchronous, false, false},
		InitializeResponse:              {1, ChannelSynchronous, false, false},
		FatalError:                      {2, ChannelEither, false, false},
		Error:                           {3, ChannelEither, false, false},
		AsyncLock:                       {4, ChannelAsynchronous, false, false},
		AsyncLockResponse:               {5, ChannelAsynchronous, false, false},
		Data:                            {6, ChannelSynchronous, false, false},
		DataEnd:                         {7, ChannelSynchronous, false, false},
		DeviceClearComplete:             {8, ChannelSynchronous, false, false},
		DeviceClearAcknowledge:          {9, ChannelSynchronous, false, false},
		AsyncRemoteLocalControl:         {10, ChannelAsynchronous, false, false},
		AsyncRemoteLocalResponse:        {11, ChannelAsynchronous, false, false},
		Trigger:                         {12, ChannelSynchronous, false, false},
		Interrupted:                     {13, ChannelSynchronous, false, false},
		AsyncInterrupted:                {14, ChannelAsynchronous, false, false},
		AsyncMaximumMessageSize:         {15, ChannelAsynchronous, false, false},
		AsyncMaximumMessageSizeResponse: {16, ChannelAsynchronous, false, false},
		AsyncInitialize:                 {17, ChannelAsynchronous, false, false},
		AsyncInitializeResponse:         {18, ChannelAsynchronous, false, false},
		AsyncDeviceClear:                {19, ChannelAsynchronous, false, false},
		AsyncServiceRequest:             {20, ChannelAsynchronous, false, false},
		AsyncStatusQuery:                {21, ChannelAsynchronous, false, false},
		AsyncStatusResponse:             {22, ChannelAsynchronous, false, false},
		AsyncDeviceClearAcknowledge:     {23, ChannelAsynchronous, false, false},
		AsyncLockInfo:                   {24, ChannelAsynchronous, false, false},
		AsyncLockInfoResponse:           {25, ChannelAsynchronous, false, false},
		GetDescriptors:                  {26, ChannelEither, false, true},
		GetDescriptorsResponse:          {27, ChannelEither, false, true},
		StartTLS:                        {28, ChannelSynchronous, true, true},
		AsyncStartTLS:                   {29, ChannelAsynchronous, true, true},
		AsyncStartTLSResponse:           {30, ChannelAsynchronous, true, true},
		EndTLS:                          {31, ChannelSynchronous, true, true},
		AsyncEndTLS:                     {32, ChannelAsynchronous, true, true},
		AsyncEndTLSResponse:             {33, ChannelAsynchronous, true, true},
		GetSaslMechanismList:            {34, ChannelSynchronous, true, true},
		GetSaslMechanismListResponse:    {35, ChannelSynchronous, true, true},
		AuthenticationStart:             {36, ChannelSynchronous, true, true},
		AuthenticationExchange:          {37, ChannelSynchronous, true, true},
		AuthenticationResult:            {38, ChannelSynchronous, true, true},
	}
	if len(table) != 39 {
		t.Fatalf("expectation table has %d entries, want 39", len(table))
	}
	for typ, w := range table {
		if uint8(typ) != w.value {
			t.Errorf("%v numeric value = %d, want %d", typ, uint8(typ), w.value)
		}
		if !typ.Defined() {
			t.Errorf("%v Defined() = false, want true", typ)
		}
		if got := typ.Channel(); got != w.channel {
			t.Errorf("%v Channel() = %d, want %d", typ, got, w.channel)
		}
		wantCap := CapabilityGeneral
		if w.secure {
			wantCap = CapabilitySecureConnection
		}
		if got := typ.Capability(); got != wantCap {
			t.Errorf("%v Capability() = %d, want %d", typ, got, wantCap)
		}
		wantVer := Version10
		if w.v20 {
			wantVer = Version20
		}
		if got := typ.MinVersion(); got != wantVer {
			t.Errorf("%v MinVersion() = %v, want %v", typ, got, wantVer)
		}
	}
}

// TestDeviceClearAcknowledgeChannel documents a defect in IVI-6.1. Table 4
// lists DeviceClearAcknowledge as asynchronous, while the Device Clear
// transaction description in section 6.12 directs the client to wait for it on
// the synchronous channel. We follow the transaction description. See the
// design specification, §4.4 and R-PROTO-040.
func TestDeviceClearAcknowledgeChannel(t *testing.T) {
	if got := DeviceClearAcknowledge.Channel(); got != ChannelSynchronous {
		t.Errorf("DeviceClearAcknowledge.Channel() = %d, want ChannelSynchronous", got)
	}
	if !DeviceClearAcknowledge.LegalOn(ChannelSynchronous) {
		t.Error("DeviceClearAcknowledge is not legal on the synchronous channel")
	}
	if DeviceClearAcknowledge.LegalOn(ChannelAsynchronous) {
		t.Error("DeviceClearAcknowledge is legal on the asynchronous channel, want not")
	}
}

func TestReservedAndVendorRanges(t *testing.T) {
	for typ := MessageType(39); typ < 128; typ++ {
		if !typ.Reserved() {
			t.Errorf("type %d Reserved() = false, want true", typ)
		}
		if typ.Defined() {
			t.Errorf("type %d Defined() = true, want false", typ)
		}
		if typ.VendorSpecific() {
			t.Errorf("type %d VendorSpecific() = true, want false", typ)
		}
		if got := typ.Channel(); got != ChannelNone {
			t.Errorf("type %d Channel() = %d, want ChannelNone", typ, got)
		}
		if typ.LegalOn(ChannelSynchronous) || typ.LegalOn(ChannelAsynchronous) {
			t.Errorf("reserved type %d is legal on a channel, want not", typ)
		}
	}
	for typ := 128; typ <= 255; typ++ {
		mt := MessageType(typ)
		if !mt.VendorSpecific() {
			t.Errorf("type %d VendorSpecific() = false, want true", typ)
		}
		if mt.Reserved() {
			t.Errorf("type %d Reserved() = true, want false", typ)
		}
		if !mt.LegalOn(ChannelSynchronous) || !mt.LegalOn(ChannelAsynchronous) {
			t.Errorf("vendor type %d should be legal on either channel", typ)
		}
	}
}

func TestLegalOnRejectsNonChannels(t *testing.T) {
	if Data.LegalOn(ChannelNone) {
		t.Error("LegalOn(ChannelNone) = true, want false")
	}
	if Data.LegalOn(ChannelEither) {
		t.Error("LegalOn(ChannelEither) = true, want false")
	}
}

func TestMessageTypeString(t *testing.T) {
	tests := []struct {
		typ  MessageType
		want string
	}{
		{Initialize, "Initialize"},
		{DataEnd, "DataEnd"},
		{AsyncStatusQuery, "AsyncStatusQuery"},
		{AuthenticationResult, "AuthenticationResult"},
		{50, "Reserved"},
		{128, "VendorSpecific"},
		{255, "VendorSpecific"},
	}
	for _, tc := range tests {
		if got := tc.typ.String(); got != tc.want {
			t.Errorf("MessageType(%d).String() = %q, want %q", tc.typ, got, tc.want)
		}
	}
}

func TestErrorCodeValues(t *testing.T) {
	fatal := map[FatalErrorCode]uint8{
		FatalUnidentified:           0,
		FatalPoorlyFormedHeader:     1,
		FatalChannelsNotEstablished: 2,
		FatalInvalidInitSequence:    3,
		FatalMaxClientsExceeded:     4,
		FatalSecureConnectionFailed: 5,
		FatalReservedMin:            6,
		FatalDeviceDefinedMin:       128,
	}
	for code, want := range fatal {
		if uint8(code) != want {
			t.Errorf("fatal code = %d, want %d", uint8(code), want)
		}
	}
	nonFatal := map[ErrorCode]uint8{
		NonFatalUnidentified:            0,
		NonFatalUnrecognizedMessageType: 1,
		NonFatalUnrecognizedControlCode: 2,
		NonFatalUnrecognizedVendorMsg:   3,
		NonFatalMessageTooLarge:         4,
		NonFatalAuthenticationFailed:    5,
		NonFatalReservedMin:             6,
		NonFatalDeviceDefinedMin:        128,
	}
	for code, want := range nonFatal {
		if uint8(code) != want {
			t.Errorf("non-fatal code = %d, want %d", uint8(code), want)
		}
	}
}

func TestErrorCodeMapping(t *testing.T) {
	if _, fatal := FatalCodeFor(ErrInvalidPrologue); !fatal {
		t.Error("ErrInvalidPrologue should be fatal")
	}
	if code, _ := FatalCodeFor(ErrInvalidPrologue); code != FatalPoorlyFormedHeader {
		t.Errorf("FatalCodeFor(ErrInvalidPrologue) = %d, want 1", code)
	}
	if _, fatal := FatalCodeFor(ErrMessageTooLarge); fatal {
		t.Error("ErrMessageTooLarge should not be fatal")
	}
	if got := ErrorCodeFor(ErrMessageTooLarge); got != NonFatalMessageTooLarge {
		t.Errorf("ErrorCodeFor(ErrMessageTooLarge) = %d, want 4", got)
	}
	if got := ErrorCodeFor(ErrWrongChannel); got != NonFatalUnrecognizedMessageType {
		t.Errorf("ErrorCodeFor(ErrWrongChannel) = %d, want 1", got)
	}
	// An unmapped condition must not land in the reserved range 6 to 127.
	got := ErrorCodeFor(ErrPayloadLength)
	if got >= NonFatalReservedMin && got < NonFatalDeviceDefinedMin {
		t.Errorf("ErrorCodeFor() = %d, which is in the reserved range", got)
	}
}

func TestVersionOrdering(t *testing.T) {
	if !Version10.Less(Version11) {
		t.Error("1.0 should precede 1.1")
	}
	if !Version11.Less(Version20) {
		t.Error("1.1 should precede 2.0")
	}
	if Version20.Less(Version10) {
		t.Error("2.0 should not precede 1.0")
	}
	if !Version20.AtLeast(Version20) {
		t.Error("2.0 should be at least 2.0")
	}
	if Version10.AtLeast(Version20) {
		t.Error("1.0 should not be at least 2.0")
	}
}

func TestVersionString(t *testing.T) {
	if got := Version10.String(); got != "1.0" {
		t.Errorf("Version10.String() = %q, want \"1.0\"", got)
	}
	if got := (Version{Major: 12, Minor: 34}).String(); got != "12.34" {
		t.Errorf("String() = %q, want \"12.34\"", got)
	}
}

func TestNegotiate(t *testing.T) {
	// A firmware profile advertising 1.0 must negotiate 1.0 with a 2.0 client.
	if got := Negotiate(Version20, Version10); got != Version10 {
		t.Errorf("Negotiate(2.0 client, 1.0 server) = %v, want 1.0", got)
	}
	if got := Negotiate(Version10, Version20); got != Version10 {
		t.Errorf("Negotiate(1.0 client, 2.0 server) = %v, want 1.0", got)
	}
	if got := Negotiate(Version20, Version20); got != Version20 {
		t.Errorf("Negotiate(2.0, 2.0) = %v, want 2.0", got)
	}
}

func TestInitializeParameterRoundTrip(t *testing.T) {
	vendor := [2]byte{'G', 'T'}
	p := EncodeInitializeParameter(Version10, vendor)
	if p != 0x01004754 {
		t.Errorf("EncodeInitializeParameter() = %#08x, want 0x01004754", p)
	}
	gotVer, gotVendor := DecodeInitializeParameter(p)
	if gotVer != Version10 {
		t.Errorf("version = %v, want 1.0", gotVer)
	}
	if gotVendor != vendor {
		t.Errorf("vendor = %q, want %q", gotVendor, vendor)
	}
}

func TestInitializeResponseParameterRoundTrip(t *testing.T) {
	p := EncodeInitializeResponseParameter(Version20, 0xbeef)
	if p != 0x0200beef {
		t.Errorf("EncodeInitializeResponseParameter() = %#08x, want 0x0200beef", p)
	}
	gotVer, gotSession := DecodeInitializeResponseParameter(p)
	if gotVer != Version20 {
		t.Errorf("version = %v, want 2.0", gotVer)
	}
	if gotSession != 0xbeef {
		t.Errorf("sessionID = %#x, want 0xbeef", gotSession)
	}
}

func TestControlCodeBits(t *testing.T) {
	// The RMT-delivered flag and the prefer-overlap flag are both bit 0, of
	// different messages. Keeping them as separate constants documents that.
	if ControlRMTDelivered != 1 {
		t.Errorf("ControlRMTDelivered = %d, want 1", ControlRMTDelivered)
	}
	if ControlPreferOverlap != 1 {
		t.Errorf("ControlPreferOverlap = %d, want 1", ControlPreferOverlap)
	}
	if FeatureOverlapped != 1 {
		t.Errorf("FeatureOverlapped = %d, want 1", FeatureOverlapped)
	}
	if FeatureEncryptionMandatory != 2 {
		t.Errorf("FeatureEncryptionMandatory = %d, want 2", FeatureEncryptionMandatory)
	}
	if FeatureInitialEncryption != 4 {
		t.Errorf("FeatureInitialEncryption = %d, want 4", FeatureInitialEncryption)
	}
}

func TestDefaults(t *testing.T) {
	if DefaultPort != 4880 {
		t.Errorf("DefaultPort = %d, want 4880", DefaultPort)
	}
	if DefaultSubAddress != "hislip0" {
		t.Errorf("DefaultSubAddress = %q, want \"hislip0\"", DefaultSubAddress)
	}
}
