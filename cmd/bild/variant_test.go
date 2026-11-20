package main

// Invariants tested:
//  1. Variant parsing from raw config: parseExportVariants must canonicalize valid entries
//     (default contain fit, JPEG spelling) and leave an empty input as a nil, fault-free result.
//  2. Invalid config entries: Every malformed entry must produce exactly one
//     ErrUserConfigInvalidVariant fault naming export-variants and the 1-based entry index, with no
//     specs returned so a partial variant set can never reach a run.

import (
	"errors"
	"strings"
	"testing"

	userconfig "github.com/shdeen/bildomat/internal/config"
	"github.com/shdeen/bildomat/internal/errs"
	"github.com/shdeen/bildomat/internal/media"
	"github.com/shdeen/bildomat/internal/variant"
)

// TestParseExportVariants verifies invariant #1: Variant parsing from raw config.
//
// Test class: Expanded.
// Test layer: Coverage.
func TestParseExportVariants(t *testing.T) {
	specs, faults := parseExportVariants(nil, "/home/t/.bildomat/config.yml")
	if len(specs) != 0 || len(faults) != 0 {
		t.Errorf("✗ empty input = (%+v, %v), want no specs and no faults", specs, faults)
	}

	raw := []userconfig.RawVariant{
		{Width: 1280, Height: 720, Format: media.FormatPNG},
		{Width: 300, Height: 300, Fit: "COVER", Format: "JPEG"},
	}

	specs, faults = parseExportVariants(raw, "/home/t/.bildomat/config.yml")
	if len(faults) != 0 {
		t.Fatalf("💣 valid entries produced faults: %v", faults)
	}

	want := []variant.Spec{
		{Width: 1280, Height: 720, Fit: variant.FitContain, Format: media.FormatPNG},
		{Width: 300, Height: 300, Fit: variant.FitCover, Format: media.FormatJPG},
	}
	if len(specs) != len(want) {
		t.Fatalf("✗ parsed %d specs, want %d: %+v", len(specs), len(want), specs)
	}

	for index := range want {
		if specs[index] != want[index] {
			t.Errorf("✗ spec %d = %+v, want %+v", index, specs[index], want[index])
		}
	}

	if !t.Failed() {
		t.Log("✓ raw config entries canonicalize into validated variant specs")
	}
}

// TestParseExportVariantsInvalid verifies invariant #2: Invalid config entries.
//
// Test class: Expanded.
// Test layer: Coverage.
func TestParseExportVariantsInvalid(t *testing.T) {
	cases := []struct {
		name string
		raw  userconfig.RawVariant
	}{
		{"nonpositive size", userconfig.RawVariant{Width: 0, Height: 720, Format: "png"}},
		{"unknown fit", userconfig.RawVariant{Width: 8, Height: 8, Fit: "stretch", Format: "png"}},
		{"unknown format", userconfig.RawVariant{Width: 8, Height: 8, Format: "gif"}},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			specs, faults := parseExportVariants([]userconfig.RawVariant{
				{Width: 8, Height: 8, Format: "png"},
				testCase.raw,
			}, "/home/t/.bildomat/config.yml")

			if len(specs) != 0 {
				t.Errorf("✗ invalid input returned specs %+v, want none", specs)
			}

			if len(faults) != 1 || !errors.Is(faults[0], errs.ErrUserConfigInvalidVariant) {
				t.Fatalf("✗ faults = %v, want one ErrUserConfigInvalidVariant", faults)
			}

			var configFault *errs.ConfigError
			if !errors.As(faults[0], &configFault) || configFault.Setting != exportVariantsKey {
				t.Errorf("✗ fault %v does not label the %s setting", faults[0], exportVariantsKey)
			}

			if !strings.Contains(faults[0].Error(), "export variant 2") {
				t.Errorf("✗ fault %q does not name the 1-based entry index", faults[0].Error())
			}
		})
	}

	if !t.Failed() {
		t.Log("✓ every malformed variant entry fails the whole list with an indexed config fault")
	}
}
