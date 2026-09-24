// Copyright (c) 2026 The hislip developers. All rights reserved.
// Project site: https://github.com/tridentsx/hislip
// Use of this source code is governed by a MIT-style license that
// can be found in the LICENSE.txt file for the project.

package server

import (
	"errors"
	"testing"

	"github.com/tridentsx/hislip/protocol"
)

// TestZeroConfigIsTheDeviceProfile confirms that the defaults are the
// PoE-to-GPIB profile, so a caller who wants that profile need not restate it and
// cannot get it subtly wrong.
func TestZeroConfigIsTheDeviceProfile(t *testing.T) {
	cfg := Config{}.withDefaults()

	if cfg.SupportsOverlap {
		t.Error("SupportsOverlap defaults to true; the device profile forbids Overlap Mode")
	}
	if cfg.MaxVersion != protocol.Version10 {
		t.Errorf("MaxVersion = %v, want 1.0; the firmware must not claim 2.0 behaviour", cfg.MaxVersion)
	}
	if cfg.MaxSessions != 1 {
		t.Errorf("MaxSessions = %d, want 1", cfg.MaxSessions)
	}
	if cfg.MaxRxPayload != DefaultMaxRxPayload || cfg.MaxTxPayload != DefaultMaxTxPayload {
		t.Errorf("payload defaults = %d/%d", cfg.MaxRxPayload, cfg.MaxTxPayload)
	}
	if len(cfg.SubAddresses) != 1 || cfg.SubAddresses[0] != protocol.DefaultSubAddress {
		t.Errorf("SubAddresses = %v, want [hislip0]", cfg.SubAddresses)
	}
	if cfg.VendorID != [2]byte{} {
		t.Errorf("VendorID = %q; it must default to unidentified until an IVI ID is assigned", cfg.VendorID)
	}
}

// TestOverlapIsRefusedAtConstruction checks that a profile requesting an
// unimplemented mode fails immediately. Advertising a mode and then not honouring
// it would leave a client waiting for responses that never arrive in order, which
// is far harder to diagnose than a refused constructor.
func TestOverlapIsRefusedAtConstruction(t *testing.T) {
	_, err := New(&nopDevice{}, Config{SupportsOverlap: true})
	if !errors.Is(err, ErrOverlapNotImplemented) {
		t.Errorf("New() with SupportsOverlap error = %v, want ErrOverlapNotImplemented", err)
	}
}

func TestConfigValidation(t *testing.T) {
	tests := []struct {
		name string
		cfg  Config
	}{
		{"negative sessions", Config{MaxSessions: -1}},
		{"version below 1.0", Config{MaxVersion: protocol.Version{Major: 0, Minor: 9}}},
		{"rx payload smaller than a header", Config{MaxRxPayload: 4}},
		{"tx payload smaller than a header", Config{MaxTxPayload: 4}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := New(&nopDevice{}, tc.cfg); err == nil {
				t.Error("New() error = nil, want a validation failure")
			}
		})
	}
}

func TestNewRejectsNilDevice(t *testing.T) {
	if _, err := New(nil, Config{}); !errors.Is(err, ErrInvalidConfig) {
		t.Errorf("New(nil) error = %v, want ErrInvalidConfig", err)
	}
}

// TestFeatureBitmapDeclinesOverlap is the binding half of the Overlap Mode
// prohibition. IVI-6.1 says the server accepts the client's proposal only if it
// is capable of supporting it, so a synchronized-only profile answers with bit 0
// clear however insistent the client is.
func TestFeatureBitmapDeclinesOverlap(t *testing.T) {
	cfg := Config{}.withDefaults()

	tests := []struct {
		name    string
		request uint8
	}{
		{"client requests synchronized", 0},
		{"client requests overlap", protocol.FeatureOverlapped},
		{"client requests overlap and encryption", protocol.FeatureOverlapped |
			protocol.FeatureEncryptionMandatory | protocol.FeatureInitialEncryption},
		{"client sets every bit", 0xff},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := cfg.featureBitmap(tc.request)
			if got&protocol.FeatureOverlapped != 0 {
				t.Errorf("agreed bitmap = %#x; bit 0 set agrees to Overlap Mode", got)
			}
			if got&protocol.FeatureEncryptionMandatory != 0 {
				t.Errorf("agreed bitmap = %#x; encryption is not implemented", got)
			}
			if got&protocol.FeatureInitialEncryption != 0 {
				t.Errorf("agreed bitmap = %#x; initial encryption is not implemented", got)
			}
		})
	}
}

func TestPreferOverlapBit(t *testing.T) {
	if got := (Config{}.withDefaults()).preferOverlapBit(); got != 0 {
		t.Errorf("preferOverlapBit() = %#x, want 0", got)
	}
}

func TestAcceptsSubAddress(t *testing.T) {
	cfg := Config{SubAddresses: []string{"hislip0", "hislip1"}}
	tests := map[string]bool{
		"":        true, // null selects the default instrument
		"hislip0": true,
		"hislip1": true,
		"hislip2": false,
		"Hislip0": false,
	}
	for sub, want := range tests {
		if got := cfg.acceptsSubAddress(sub); got != want {
			t.Errorf("acceptsSubAddress(%q) = %v, want %v", sub, got, want)
		}
	}
}
