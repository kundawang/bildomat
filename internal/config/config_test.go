package config

import (
	"errors"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shdeen/bildomat/internal/errs"
)

// Invariants tested:
// 1. User configuration loading matrix: Load must decode valid settings and return the
//    configuration path under the home directory.
// 2. Unknown provider fault: Given a configuration path and unknown provider ID,
//    UnknownProviderFault must return an error matching ErrUserConfig and
//    ErrUserConfigUnknownProvider whose text includes both supplied values.

// TestLoad verifies invariant #1: User configuration loading matrix.
//
// What is being tested:
// Load must decode valid settings and return the configuration path under the home directory.
// Missing files and empty values must yield unset settings without faults; an unreadable config
// path or an invalid YAML file must yield unset settings and one ErrUserConfig fault. An unknown
// key must produce a fault naming that key while preserving valid settings.
//
// Test class: Expanded.
// Test layer: Coverage.
func TestLoad(t *testing.T) {
	cases := []struct {
		name          string
		fileContent   string
		hasFile       bool
		unreadable    bool
		wantModel     string
		wantOutDir    string
		wantKeys      map[string]string
		wantFault     bool
		wantFaultName string
	}{
		{
			name: "complete file decodes",
			fileContent: "default-model: gamma/fixture-model\noutput-dir: ~/Pictures/bild\n" +
				"api-keys:\n  alpha: alpha-test-value\n  beta: beta-test-value\n",
			hasFile:    true,
			wantModel:  "gamma/fixture-model",
			wantOutDir: "~/Pictures/bild",
			wantKeys:   map[string]string{"alpha": "alpha-test-value", "beta": "beta-test-value"},
		},
		{
			name: "missing config directory is silent",
		},
		{
			name:        "invalid yaml warns and yields nothing",
			fileContent: "default-model: [unclosed\n",
			hasFile:     true,
			wantFault:   true,
		},
		{
			name:       "unreadable config path warns and yields nothing",
			unreadable: true,
			wantFault:  true,
		},
		{
			name:          "unknown setting warns and the rest applies",
			fileContent:   "default-model: gamma/fixture-model\ndefault-modle: mistyped-value\n",
			hasFile:       true,
			wantModel:     "gamma/fixture-model",
			wantFault:     true,
			wantFaultName: "default-modle",
		},
		{
			name:        "empty values load as unset",
			fileContent: "default-model: \"\"\noutput-dir: \"\"\napi-keys:\n  alpha: \"\"\n",
			hasFile:     true,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			home := t.TempDir()
			setHomeDirectory(t, home)

			wantPath := filepath.Join(home, ".bildomat", "config.yml")
			if c.hasFile {
				writeConfigFile(t, home, c.fileContent)
			}

			// A directory at the config path fails the read on every platform; a permission
			// change would not, since Windows keeps a read-only file readable.
			if c.unreadable {
				if err := os.MkdirAll(wantPath, 0o750); err != nil {
					t.Fatalf("💣 directory at the config path: %v", err)
				}
			}

			settings, configPath, faults := Load()

			if configPath != wantPath {
				t.Errorf("✗ path = %q, want %q", configPath, wantPath)
			}

			if settings.DefaultModel != c.wantModel {
				t.Errorf("✗ DefaultModel = %q, want %q", settings.DefaultModel, c.wantModel)
			}

			if settings.DefaultOutputDir != c.wantOutDir {
				t.Errorf("✗ DefaultOutputDir = %q, want %q", settings.DefaultOutputDir, c.wantOutDir)
			}

			if !maps.Equal(settings.APIKeys, c.wantKeys) {
				t.Errorf("✗ APIKeys = %v, want %v", settings.APIKeys, c.wantKeys)
			}

			if c.wantFault {
				expectClassifiedFault(t, faults, c.wantFaultName)
			} else if len(faults) != 0 {
				t.Errorf("✗ faults = %v, want none", faults)
			}

			if !t.Failed() {
				t.Logf("✓ %s", c.name)
			}
		})
	}

	if !t.Failed() {
		t.Log("✓ loading decodes complete settings, stays silent on a missing file, classifies every fault, and drops empty values")
	}
}

// TestUnknownProviderFault verifies invariant #2: Unknown provider fault.
//
// What is being tested:
// Given a configuration path and unknown provider ID, UnknownProviderFault must return an error
// matching ErrUserConfig and ErrUserConfigUnknownProvider whose text includes both supplied values.
//
// Test class: Expanded.
// Test layer: Coverage.
func TestUnknownProviderFault(t *testing.T) {
	fault := UnknownProviderFault("/scratch-home/.bildomat/config.yml", "not-a-provider")

	if !errors.Is(fault, errs.ErrUserConfig) || !errors.Is(fault, errs.ErrUserConfigUnknownProvider) {
		t.Errorf("✗ fault %v does not classify as the unknown-provider user-config fault", fault)
	}

	for _, wantValue := range []string{"/scratch-home/.bildomat/config.yml", "not-a-provider"} {
		if !strings.Contains(fault.Error(), wantValue) {
			t.Errorf("✗ fault %q does not name %q", fault, wantValue)
		}
	}

	if !t.Failed() {
		t.Log("✓ the unknown-provider fault classifies correctly and names the path and the provider ID")
	}
}

// TestLoadExportVariants verifies invariant #3: Export variants.
//
// What is being tested:
// Load must retain the raw export-variants entries exactly as written, including an unset fit. The
// config layer performs no image-semantic validation, so even a zero-width entry decodes without a
// config fault; the variant parser validates it after loading.
//
// Test class: Expanded.
// Test layer: Coverage.
func TestLoadExportVariants(t *testing.T) {
	home := t.TempDir()
	setHomeDirectory(t, home)

	validConfig := "export-variants:\n" +
		"  - width: 1280\n    height: 720\n    format: png\n" +
		"  - width: 300\n    height: 300\n    fit: COVER\n    format: JPEG\n"
	writeConfigFile(t, home, validConfig)

	settings, _, faults := Load()
	if len(faults) != 0 {
		t.Fatalf("💣 variant entries produced config faults: %v", faults)
	}

	wantVariants := []RawVariant{
		{Width: 1280, Height: 720, Fit: "", Format: "png"},
		{Width: 300, Height: 300, Fit: "COVER", Format: "JPEG"},
	}
	if len(settings.ExportVariants) != len(wantVariants) {
		t.Fatalf("✗ loaded %d raw variants, want %d", len(settings.ExportVariants), len(wantVariants))
	}

	for index, want := range wantVariants {
		if settings.ExportVariants[index] != want {
			t.Errorf("✗ raw variant %d = %+v, want %+v", index, settings.ExportVariants[index], want)
		}
	}

	// Semantically invalid entries still decode at the config layer; validation happens later.
	home = t.TempDir()
	setHomeDirectory(t, home)
	writeConfigFile(t, home, "export-variants:\n  - width: 0\n    height: 720\n    format: png\n")

	invalidSettings, _, invalidFaults := Load()
	if len(invalidFaults) != 0 {
		t.Errorf("✗ raw decoding produced faults %v, want validation deferred to the variant parser", invalidFaults)
	}

	if len(invalidSettings.ExportVariants) != 1 || invalidSettings.ExportVariants[0].Width != 0 {
		t.Errorf("✗ invalid raw entry was not retained: %+v", invalidSettings.ExportVariants)
	}

	if !t.Failed() {
		t.Log("✓ the config layer decodes export-variants verbatim and leaves semantic validation to the variant package")
	}
}

// writeConfigFile writes a config file under the given home directory's .bildomat directory.
func writeConfigFile(t *testing.T, home, content string) {
	t.Helper()

	configDir := filepath.Join(home, ".bildomat")
	if err := os.MkdirAll(configDir, 0o750); err != nil {
		t.Fatalf("💣 config directory creation failed: %v", err)
	}

	configPath := filepath.Join(configDir, "config.yml")
	if err := os.WriteFile(configPath, []byte(content), 0o600); err != nil {
		t.Fatalf("💣 config file write failed: %v", err)
	}
}

// expectClassifiedFault asserts exactly one fault matching errs.ErrUserConfig whose text names
// wantFaultName; an empty wantFaultName requires no name.
func expectClassifiedFault(t *testing.T, faults []error, wantFaultName string) {
	t.Helper()

	if len(faults) != 1 || !errors.Is(faults[0], errs.ErrUserConfig) {
		t.Errorf("✗ faults = %v, want one fault under errs.ErrUserConfig", faults)

		return
	}

	// Contains holds trivially when no name is required.
	if !strings.Contains(faults[0].Error(), wantFaultName) {
		t.Errorf("✗ fault %q does not name %q", faults[0], wantFaultName)
	}
}

// setHomeDirectory points the home directory at the supplied path for the rest of the test, under
// the variable each platform reads: HOME on Unix and USERPROFILE on Windows.
func setHomeDirectory(t *testing.T, home string) {
	t.Helper()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
}
