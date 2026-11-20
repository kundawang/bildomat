package artifact

// Invariants tested:
//  1. Variant naming and facts: WriteVariants on one image must write one file per spec named with
//     the variant suffix and format extension, each returned with its path and byte count.
//  2. Batch naming: With two artifacts, each variant keeps the artifact index ahead of the variant
//     suffix.
//  3. Collision handling: An existing variant destination forces the same numbered -02 suffix
//     policy the regular artifact writer uses, without touching the existing file.
//  4. Bad media stops the run: An undecodable image returns an ErrInputMediaDecode failure and no
//     successful-entry list beyond files already completed; the run never reports a phantom success.
//  5. Non-image artifacts skip: A video (.mp4) artifact yields no variants and no error.
//  6. File-backed sources share the path: An artifact whose bytes live in a temporary file exports
//     the same variants as an in-memory artifact.

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/shdeen/bildomat/internal/errs"
	"github.com/shdeen/bildomat/internal/media"
	"github.com/shdeen/bildomat/internal/variant"
)

// TestWriteVariantsNamingAndFacts verifies invariant #1: Variant naming and facts.
//
// Test class: Expanded.
// Test layer: Coverage.
func TestWriteVariantsNamingAndFacts(t *testing.T) {
	dir := t.TempDir()
	artifacts := []Media{{Data: solidPNG(t, 4, 4, color.RGBA{R: 30, G: 60, B: 90, A: 255}), FileExt: ".png"}}
	specs := []variant.Spec{
		{Width: 8, Height: 8, Fit: variant.FitContain, Format: media.FormatPNG},
		{Width: 4, Height: 8, Fit: variant.FitCover, Format: media.FormatJPG},
	}

	savedFiles, err := WriteVariants(dir, "hero", artifacts, specs)
	if err != nil {
		t.Fatalf("💣 WriteVariants: %v", err)
	}

	wantNames := []string{"hero-8x8-contain.png", "hero-4x8-cover.jpg"}
	if len(savedFiles) != len(wantNames) {
		t.Fatalf("✗ saved %d variant files, want %d: %+v", len(savedFiles), len(wantNames), savedFiles)
	}

	for index, wantName := range wantNames {
		wantPath := filepath.Join(dir, wantName)
		if savedFiles[index].Path != wantPath {
			t.Errorf("✗ variant %d path = %q, want %q", index, savedFiles[index].Path, wantPath)
		}

		if savedFiles[index].Bytes <= 0 {
			t.Errorf("✗ variant %s reports %d bytes, want a positive count", wantName, savedFiles[index].Bytes)
		}

		// #nosec G304 -- the path is this test's own output.
		info, statErr := os.Stat(wantPath)
		if statErr != nil {
			t.Errorf("✗ expected variant %s is missing: %v", wantName, statErr)

			continue
		}

		if info.Size() != savedFiles[index].Bytes {
			t.Errorf("✗ reported bytes %d do not match on-disk size %d for %s", savedFiles[index].Bytes, info.Size(), wantName)
		}
	}

	if !t.Failed() {
		t.Log("✓ every spec lands under its suffixed name with truthful file facts")
	}
}

// TestWriteVariantsBatchNaming verifies invariant #2: Batch naming.
//
// Test class: Expanded.
// Test layer: Coverage.
func TestWriteVariantsBatchNaming(t *testing.T) {
	dir := t.TempDir()
	artifacts := []Media{
		{Data: solidPNG(t, 4, 4, color.RGBA{A: 255}), FileExt: ".png"},
		{Data: solidPNG(t, 4, 4, color.RGBA{R: 255, A: 255}), FileExt: ".png"},
	}
	specs := []variant.Spec{{Width: 2, Height: 2, Fit: variant.FitCover, Format: media.FormatPNG}}

	savedFiles, err := WriteVariants(dir, "run", artifacts, specs)
	if err != nil {
		t.Fatalf("💣 WriteVariants batch: %v", err)
	}

	wantNames := []string{"run-0-2x2-cover.png", "run-1-2x2-cover.png"}
	if len(savedFiles) != len(wantNames) {
		t.Fatalf("✗ saved %d files, want %d: %+v", len(savedFiles), len(wantNames), savedFiles)
	}

	for index, wantName := range wantNames {
		if filepath.Base(savedFiles[index].Path) != wantName {
			t.Errorf("✗ batch variant name = %q, want %q", filepath.Base(savedFiles[index].Path), wantName)
		}
	}

	if !t.Failed() {
		t.Log("✓ batch variants keep their -N artifact index before the variant suffix")
	}
}

// TestWriteVariantsCollision verifies invariant #3: Collision handling.
//
// Test class: Expanded.
// Test layer: Coverage.
func TestWriteVariantsCollision(t *testing.T) {
	dir := t.TempDir()
	collidingPath := filepath.Join(dir, "hero-8x8-contain.png")

	// #nosec G306 -- a test-owned seed file.
	if err := os.WriteFile(collidingPath, []byte("occupied"), 0o644); err != nil {
		t.Fatalf("💣 seed collision file: %v", err)
	}

	artifacts := []Media{{Data: solidPNG(t, 4, 4, color.RGBA{A: 255}), FileExt: ".png"}}
	specs := []variant.Spec{{Width: 8, Height: 8, Fit: variant.FitContain, Format: media.FormatPNG}}

	savedFiles, err := WriteVariants(dir, "hero", artifacts, specs)
	if err != nil {
		t.Fatalf("💣 WriteVariants with a collision: %v", err)
	}

	wantPath := filepath.Join(dir, "hero-8x8-contain-02.png")
	if len(savedFiles) != 1 || savedFiles[0].Path != wantPath {
		t.Fatalf("✗ collision result = %+v, want one file at %s", savedFiles, wantPath)
	}

	// #nosec G304 -- the seed belongs to this test's temporary directory.
	if kept, readErr := os.ReadFile(collidingPath); readErr != nil || string(kept) != "occupied" {
		t.Errorf("✗ the colliding file changed to %q (%v), want its original bytes", kept, readErr)
	}

	if !t.Failed() {
		t.Log("✓ variant destinations use the numbered collision suffix without overwriting anything")
	}
}

// TestWriteVariantsBadMedia verifies invariant #4: Bad media stops the run.
//
// Test class: Expanded.
// Test layer: Coverage.
func TestWriteVariantsBadMedia(t *testing.T) {
	dir := t.TempDir()
	artifacts := []Media{{Data: []byte("not an image"), FileExt: ".png"}}
	specs := []variant.Spec{
		{Width: 8, Height: 8, Fit: variant.FitContain, Format: media.FormatPNG},
		{Width: 4, Height: 4, Fit: variant.FitCover, Format: media.FormatJPG},
	}

	savedFiles, err := WriteVariants(dir, "broken", artifacts, specs)
	if !errors.Is(err, errs.ErrInputMediaDecode) {
		t.Errorf("✗ bad-media error = %v, want ErrInputMediaDecode", err)
	}

	if len(savedFiles) != 0 {
		t.Errorf("✗ bad media returned %d saved-file entries, want none so no phantom success is reported", len(savedFiles))
	}

	entries, readErr := os.ReadDir(dir)
	if readErr != nil {
		t.Fatalf("💣 read output dir: %v", readErr)
	}

	if len(entries) != 0 {
		t.Errorf("✗ bad media left files %v; a failed transform must write nothing", entryNames(entries))
	}

	// No specs means the no-op path stays successful and writes nothing.
	if noopFiles, noopErr := WriteVariants(dir, "broken", artifacts, nil); noopErr != nil || len(noopFiles) != 0 {
		t.Errorf("✗ WriteVariants with no specs = (%+v, %v), want an empty no-op", noopFiles, noopErr)
	}

	if !t.Failed() {
		t.Log("✓ undecodable media fails classified with an empty saved list and no files on disk")
	}
}

// TestWriteVariantsMidwayFailure verifies that a transform failure on a later artifact stops further
// writes while the already completed variant stays reported and no phantom entries appear.
//
// Test class: Expanded.
// Test layer: Coverage.
func TestWriteVariantsMidwayFailure(t *testing.T) {
	dir := t.TempDir()
	artifacts := []Media{
		{Data: solidPNG(t, 4, 4, color.RGBA{A: 255}), FileExt: ".png"},
		{Data: []byte("not an image"), FileExt: ".png"},
	}
	specs := []variant.Spec{
		{Width: 8, Height: 8, Fit: variant.FitContain, Format: media.FormatPNG},
		{Width: 6, Height: 6, Fit: variant.FitCover, Format: media.FormatJPG},
	}

	savedFiles, err := WriteVariants(dir, "hero", artifacts, specs)
	if !errors.Is(err, errs.ErrInputMediaDecode) {
		t.Errorf("✗ midway error = %v, want ErrInputMediaDecode from the second artifact", err)
	}

	// The healthy first artifact completes both specs before the corrupt one is reached.
	if len(savedFiles) != 2 {
		t.Fatalf("✗ partial result = %+v, want exactly the first artifact's two variants", savedFiles)
	}

	wantNames := []string{"hero-0-8x8-contain.png", "hero-0-6x6-cover.jpg"}
	for index, wantName := range wantNames {
		if filepath.Base(savedFiles[index].Path) != wantName {
			t.Errorf("✗ partial variant %d = %q, want %q", index, filepath.Base(savedFiles[index].Path), wantName)
		}

		if _, statErr := os.Stat(savedFiles[index].Path); statErr != nil {
			t.Errorf("✗ reported variant %s does not exist: %v", wantName, statErr)
		}
	}

	// The corrupt artifact produced neither variants nor half-written files.
	for _, unwanted := range []string{"hero-1-8x8-contain.png", "hero-1-6x6-cover.jpg"} {
		if _, statErr := os.Stat(filepath.Join(dir, unwanted)); !errors.Is(statErr, os.ErrNotExist) {
			t.Errorf("✗ failed artifact left %s on disk", unwanted)
		}
	}

	if !t.Failed() {
		t.Log("✓ a mid-run transform failure stops further writes while completed files stay truthful")
	}
}

// TestWriteVariantsDestinationFailure verifies that an unwritable destination returns an output
// error and leaves no partial variant file.
//
// Test class: Expanded.
// Test layer: Coverage.
func TestWriteVariantsDestinationFailure(t *testing.T) {
	dir := t.TempDir()
	blockedDir := filepath.Join(dir, "readonly")

	// #nosec G301 -- a restricted test-owned directory; 0500 deliberately blocks new file creation.
	if err := os.MkdirAll(blockedDir, 0o500); err != nil {
		t.Fatalf("💣 create the blocked directory: %v", err)
	}

	t.Cleanup(func() {
		_ = os.Chmod(blockedDir, 0o755) //nolint:gosec // restoring cleanup permissions on a test-owned directory
	})

	artifacts := []Media{{Data: solidPNG(t, 4, 4, color.RGBA{A: 255}), FileExt: ".png"}}
	specs := []variant.Spec{{Width: 8, Height: 8, Fit: variant.FitContain, Format: media.FormatPNG}}

	savedFiles, err := WriteVariants(blockedDir, "hero", artifacts, specs)
	if !errors.Is(err, errs.ErrOutputFile) {
		t.Errorf("✗ destination error = %v, want an output-file classification", err)
	}

	if len(savedFiles) != 0 {
		t.Errorf("✗ a failed write reported %d files, want none", len(savedFiles))
	}

	if entries, _ := os.ReadDir(blockedDir); len(entries) != 0 {
		t.Errorf("✗ a failed variant write left %v behind", entryNames(entries))
	}

	if !t.Failed() {
		t.Log("✓ an unwritable destination fails classified without a partial variant file")
	}
}

// TestWriteVariantsSkipsVideo verifies invariant #5: Non-image artifacts skip.
//
// Test class: Expanded.
// Test layer: Coverage.
func TestWriteVariantsSkipsVideo(t *testing.T) {
	dir := t.TempDir()
	artifacts := []Media{{Data: []byte("video-bytes"), FileExt: ".mp4"}}
	specs := []variant.Spec{{Width: 8, Height: 8, Fit: variant.FitContain, Format: media.FormatPNG}}

	savedFiles, err := WriteVariants(dir, "clip", artifacts, specs)
	if err != nil {
		t.Errorf("✗ video artifact returned %v, want a clean skip", err)
	}

	if len(savedFiles) != 0 {
		t.Errorf("✗ video produced variants %+v, want none", savedFiles)
	}

	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Errorf("✗ a skipped video left files %v", entryNames(entries))
	}

	if !t.Failed() {
		t.Log("✓ non-image artifacts skip variant export without an error")
	}
}

// TestWriteVariantsFileBacked verifies invariant #6: File-backed sources share the path.
//
// Test class: Expanded.
// Test layer: Coverage.
func TestWriteVariantsFileBacked(t *testing.T) {
	dir := t.TempDir()
	sourceBytes := solidPNG(t, 6, 2, color.RGBA{R: 12, G: 34, B: 56, A: 255})
	sourcePath := srcFile(t, string(sourceBytes))

	artifacts := []Media{{TmpPath: sourcePath, FileExt: ".png"}}
	specs := []variant.Spec{{Width: 4, Height: 4, Fit: variant.FitCover, Format: media.FormatPNG}}

	savedFiles, err := WriteVariants(dir, "downloaded", artifacts, specs)
	if err != nil {
		t.Fatalf("💣 file-backed WriteVariants: %v", err)
	}

	if len(savedFiles) != 1 || filepath.Base(savedFiles[0].Path) != "downloaded-4x4-cover.png" {
		t.Fatalf("✗ file-backed result = %+v, want one downloaded-4x4-cover.png", savedFiles)
	}

	// The downloaded source is preserved: cleanup belongs to WriteMedia, not variant export.
	if _, statErr := os.Stat(sourcePath); statErr != nil {
		t.Errorf("✗ variant export removed the temporary source: %v", statErr)
	}

	decoded, _, decodeErr := image.Decode(bytes.NewReader(mustReadFile(t, savedFiles[0].Path)))
	if decodeErr != nil {
		t.Fatalf("💣 decode the file-backed variant: %v", decodeErr)
	}

	if decoded.Bounds().Dx() != 4 || decoded.Bounds().Dy() != 4 {
		t.Errorf("✗ file-backed variant dimensions = %v, want 4x4", decoded.Bounds())
	}

	if !t.Failed() {
		t.Log("✓ temporary-file downloads and in-memory images export through the same path")
	}
}

// solidPNG encodes a uniform RGBA image as PNG bytes.
func solidPNG(t *testing.T, width, height int, fill color.RGBA) []byte {
	t.Helper()

	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := range height {
		for x := range width {
			img.SetRGBA(x, y, fill)
		}
	}

	var encoded bytes.Buffer
	if err := png.Encode(&encoded, img); err != nil {
		t.Fatalf("💣 encode solid fixture: %v", err)
	}

	return encoded.Bytes()
}

// mustReadFile reads a path the test itself produced or fails the test.
func mustReadFile(t *testing.T, path string) []byte {
	t.Helper()

	// #nosec G304 -- path is a test-owned output under t.TempDir.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("💣 read %s: %v", path, err)
	}

	return data
}

// entryNames returns directory entry names for failure messages.
func entryNames(entries []os.DirEntry) []string {
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}

	return names
}
