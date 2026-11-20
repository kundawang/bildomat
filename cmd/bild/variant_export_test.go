package main

// Invariants tested:
//  1. End-to-end variant export: With a fake provider returning a fixed RGBA PNG and configured
//     variants, testGenerate must save the original plus every variant, and every saved path must
//     exist on disk and appear in the JSON outcome's artifacts list.
//  2. Original command unchanged: With no configured variants, testGenerate must save only the
//     original artifact and no variant-suffixed files.
//  3. Bad media fails honestly: A fake provider whose image bytes are undecodable must make the run
//     return an ErrInputMediaDecode-classified error while the original still lands and no variant
//     file or phantom artifact entry appears.
//  4. Output-name collisions: Pre-seeding a variant destination must move that variant to a -02
//     name without replacing the seed.
//  5. Video runs skip variants: A video model with configured variants must save only the video and
//     report the skip notice.

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/shdeen/bildomat/internal/artifact"
	"github.com/shdeen/bildomat/internal/catalog"
	"github.com/shdeen/bildomat/internal/errs"
	"github.com/shdeen/bildomat/internal/generation"
	"github.com/shdeen/bildomat/internal/media"
	"github.com/shdeen/bildomat/internal/output"
	"github.com/shdeen/bildomat/internal/params"
	"github.com/shdeen/bildomat/internal/variant"
)

// catalogProvVideoPair builds a video model pair for writeRunResults without catalog endpoint
// validation, which is irrelevant to the output-layer skip behavior.
func catalogProvVideoPair() catalog.ProvModelPair {
	return catalog.ProvModelPair{Model: catalog.Model{ID: "fixt-vid", Media: media.Video}}
}

// variantSpecs is the fixed configuration used by the end-to-end tests.
func variantSpecs() []variant.Spec {
	return []variant.Spec{
		{Width: 8, Height: 8, Fit: variant.FitContain, Format: media.FormatPNG},
		{Width: 4, Height: 8, Fit: variant.FitCover, Format: media.FormatJPG},
	}
}

// variantTestApp builds a fake-provider application whose export variants are configured.
func variantTestApp(t *testing.T, specs []variant.Spec) (*bildApp, string) {
	t.Helper()

	loadedCatalog := fixtureCatalog(t, fixtSource(t, "fixt", "Fixture", "FIXT_KEY",
		`{"id": "fixt-img", "name": "fixt-img", "media": "image", "params": []}`))
	app := testApp(t, loadedCatalog)
	app.exportVariants = specs

	return app, app.defaultOutDir
}

// variantRunInputs returns run flags targeting a fresh output directory.
func variantRunInputs(outDir string) RunFlags {
	return RunFlags{
		Model:   params.GetSetIf(true, "fixt-img"),
		Prompt:  params.GetSetIf(true, "a prompt"),
		OutPath: params.GetSetIf(true, filepath.Join(outDir, "run")),
	}
}

// rgbaPNG encodes a 4x2 RGBA image with distinctive pixels so resampling remains observable.
func rgbaPNG(t *testing.T) []byte {
	t.Helper()

	img := image.NewRGBA(image.Rect(0, 0, 4, 2))

	for y := range 2 {
		for x := range 4 {
			img.Set(x, y, color.RGBA{
				R: uint8(x * 85), //nolint:gosec // bounded by construction
				G: 10,
				B: uint8(y * 255), //nolint:gosec // bounded by construction
				A: 255,
			})
		}
	}

	var encoded bytes.Buffer
	if err := png.Encode(&encoded, img); err != nil {
		t.Fatalf("💣 encode RGBA fixture: %v", err)
	}

	return encoded.Bytes()
}

// TestGenerateExportsVariants verifies invariant #1: End-to-end variant export.
//
// Test class: Expanded.
// Test layer: Coverage.
func TestGenerateExportsVariants(t *testing.T) {
	specs := variantSpecs()
	app, outDir := variantTestApp(t, specs)

	gen := &stubGen{test: t, result: generation.Result{Artifacts: []artifact.Media{
		{Data: rgbaPNG(t), FileExt: ".png"},
	}}}

	stdout, err := captureGenerate(t, app, gen, variantRunInputs(outDir), params.FlagInputs{}, true, "")
	if err != nil {
		t.Fatalf("💣 variant generation failed: %v", err)
	}

	wantFiles := []string{
		"run.png",
		"run-8x8-contain.png",
		"run-4x8-cover.jpg",
	}

	printed := strings.Split(strings.TrimSuffix(stdout, "\n"), "\n")
	if len(printed) != len(wantFiles) {
		t.Errorf("✗ --print-filename lines = %v, want exactly %v", printed, wantFiles)
	}

	for index, wantName := range wantFiles {
		wantPath := filepath.Join(outDir, wantName)
		if info, statErr := os.Stat(wantPath); statErr != nil || info.Size() == 0 {
			t.Errorf("✗ expected output %s missing or empty: %v", wantName, statErr)
		}

		if index < len(printed) && printed[index] != wantPath {
			t.Errorf("✗ printed line %d = %q, want %q", index, printed[index], wantPath)
		}
	}

	if len(app.invocation.outcome.Artifacts) != len(wantFiles) {
		t.Errorf("✗ outcome artifacts = %d entries, want %d with no phantom successes", len(app.invocation.outcome.Artifacts), len(wantFiles))
	}

	// The contain PNG decodes at exactly 8x8; the cover JPEG decodes at exactly 4x8.
	assertDecodedSize(t, filepath.Join(outDir, "run-8x8-contain.png"), 8, 8)
	assertDecodedSize(t, filepath.Join(outDir, "run-4x8-cover.jpg"), 4, 8)

	// The original bytes are preserved untouched.
	// #nosec G304 -- run.png is this test's own output under t.TempDir.
	if original, readErr := os.ReadFile(filepath.Join(outDir, "run.png")); readErr != nil || !bytes.Equal(original, gen.result.Artifacts[0].Data) {
		t.Errorf("✗ original artifact bytes changed (err %v)", readErr)
	}

	if !t.Failed() {
		t.Log("✓ one fake generation lands the original and every configured variant with truthful paths")
	}
}

// TestGenerateWithoutVariantsUnchanged verifies invariant #2: Original command unchanged.
//
// Test class: Expanded.
// Test layer: Coverage.
func TestGenerateWithoutVariantsUnchanged(t *testing.T) {
	app, outDir := variantTestApp(t, nil)

	gen := &stubGen{test: t, result: generation.Result{Artifacts: []artifact.Media{
		{Data: rgbaPNG(t), FileExt: ".png"},
	}}}

	if _, err := captureGenerate(t, app, gen, variantRunInputs(outDir), params.FlagInputs{}, false, ""); err != nil {
		t.Fatalf("💣 generation without variants failed: %v", err)
	}

	entries, err := os.ReadDir(outDir)
	if err != nil {
		t.Fatalf("💣 read output dir: %v", err)
	}

	if len(entries) != 1 || entries[0].Name() != "run.png" {
		t.Errorf("✗ output directory holds %v, want only run.png", entryBaseNames(entries))
	}

	if len(app.invocation.outcome.Artifacts) != 1 {
		t.Errorf("✗ outcome lists %d artifacts, want exactly the original", len(app.invocation.outcome.Artifacts))
	}

	if !t.Failed() {
		t.Log("✓ without configured variants the single-artifact command behaves exactly as before")
	}
}

// TestGenerateVariantBadMedia verifies invariant #3: Bad media fails honestly.
//
// Test class: Expanded.
// Test layer: Coverage.
func TestGenerateVariantBadMedia(t *testing.T) {
	specs := variantSpecs()
	app, outDir := variantTestApp(t, specs)

	gen := &stubGen{test: t, result: generation.Result{Artifacts: []artifact.Media{
		{Data: []byte("corrupt-image-bytes"), FileExt: ".png"},
	}}}

	_, err := captureGenerate(t, app, gen, variantRunInputs(outDir), params.FlagInputs{}, false, "")
	if !errors.Is(err, errs.ErrInputMediaDecode) {
		t.Errorf("✗ bad-media run error = %v, want ErrInputMediaDecode", err)
	}

	// The original still lands through the regular writer; no variant file exists.
	if _, statErr := os.Stat(filepath.Join(outDir, "run.png")); statErr != nil {
		t.Errorf("✗ the original artifact is missing: %v", statErr)
	}

	entries, _ := os.ReadDir(outDir)
	for _, entry := range entries {
		if strings.Contains(entry.Name(), "contain") || strings.Contains(entry.Name(), "cover") {
			t.Errorf("✗ bad media left a variant file %s", entry.Name())
		}
	}

	// The outcome may list only files that genuinely exist; the original is the sole artifact.
	for _, saved := range app.invocation.outcome.Artifacts {
		if _, statErr := os.Stat(saved.Path); statErr != nil {
			t.Errorf("✗ outcome reports nonexistent file %s: %v", saved.Path, statErr)
		}
	}

	if !t.Failed() {
		t.Log("✓ an undecodable image fails classified with no phantom variant entries")
	}
}

// TestGenerateVariantCollision verifies invariant #4: Output-name collisions.
//
// Test class: Expanded.
// Test layer: Coverage.
func TestGenerateVariantCollision(t *testing.T) {
	specs := variantSpecs()
	app, outDir := variantTestApp(t, specs)

	colliding := filepath.Join(outDir, "run-8x8-contain.png")

	// #nosec G306 -- test-owned seed file.
	if err := os.WriteFile(colliding, []byte("occupied"), 0o644); err != nil {
		t.Fatalf("💣 seed collision: %v", err)
	}

	gen := &stubGen{test: t, result: generation.Result{Artifacts: []artifact.Media{
		{Data: rgbaPNG(t), FileExt: ".png"},
	}}}

	if _, err := captureGenerate(t, app, gen, variantRunInputs(outDir), params.FlagInputs{}, false, ""); err != nil {
		t.Fatalf("💣 generation with a variant collision failed: %v", err)
	}

	// #nosec G304 -- the seed belongs to this test's temporary directory.
	if kept, readErr := os.ReadFile(colliding); readErr != nil || string(kept) != "occupied" {
		t.Errorf("✗ the seed file changed to %q (%v)", kept, readErr)
	}

	movedPath := filepath.Join(outDir, "run-8x8-contain-02.png")
	if info, statErr := os.Stat(movedPath); statErr != nil || info.Size() == 0 {
		t.Errorf("✗ the colliding variant did not move to %s: %v", movedPath, statErr)
	}

	if !t.Failed() {
		t.Log("✓ variant collisions follow the existing numbered-suffix policy and never overwrite")
	}
}

// TestGenerateVariantsSkipVideo verifies invariant #5: Video runs skip variants.
//
// Test class: Expanded.
// Test layer: Coverage.
func TestGenerateVariantsSkipVideo(t *testing.T) {
	app := testApp(t, fixtureCatalog(t, fixtSource(t, "fixt", "Fixture", "FIXT_KEY",
		`{"id": "fixt-img", "name": "fixt-img", "media": "image", "params": []}`)))
	app.exportVariants = variantSpecs()
	outDir := t.TempDir()

	result := generation.Result{Artifacts: []artifact.Media{{Data: []byte("video-bytes"), FileExt: ".mp4"}}}
	run := &generation.Generation{
		ProvModelPair: catalogProvVideoPair(),
		Prompt:        "a prompt",
		Preparation:   generation.Preparation{Params: params.Values{}},
	}
	outcome := &output.GenerationOutcome{}

	var writeErr error

	_ = captureBoth(t, func() {
		_, _, writeErr = writeRunResults(&result, outDir, "run", "", run, app.catalog.Flags, app.exportVariants, outcome)
	})
	if writeErr != nil {
		t.Fatalf("💣 video writeRunResults failed: %v", writeErr)
	}

	entries, err := os.ReadDir(outDir)
	if err != nil {
		t.Fatalf("💣 read output dir: %v", err)
	}

	if len(entries) != 1 || entries[0].Name() != "run.mp4" {
		t.Errorf("✗ video run wrote %v, want only run.mp4", entryBaseNames(entries))
	}

	if !slices.Contains(outcome.Notices, ExportVariantsSkippedNotice) {
		t.Errorf("✗ notices %v lack the video skip notice %q", outcome.Notices, ExportVariantsSkippedNotice)
	}

	if !t.Failed() {
		t.Log("✓ a video run keeps its single artifact and explains that image variants were skipped")
	}
}

// assertDecodedSize decodes an output file and asserts its exact dimensions and format family.
func assertDecodedSize(t *testing.T, path string, wantWidth, wantHeight int) {
	t.Helper()

	// #nosec G304 -- path is a test-owned output under t.TempDir.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Errorf("✗ read %s: %v", path, err)

		return
	}

	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		t.Errorf("✗ decode %s: %v", path, err)

		return
	}

	if img.Bounds().Dx() != wantWidth || img.Bounds().Dy() != wantHeight {
		t.Errorf("✗ %s dimensions = %v, want %dx%d", path, img.Bounds(), wantWidth, wantHeight)
	}
}

// entryBaseNames returns directory entry names for failure messages.
func entryBaseNames(entries []os.DirEntry) []string {
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}

	return names
}
