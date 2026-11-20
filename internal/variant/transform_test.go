package variant

// Invariants tested:
//  1. Spec parsing: ParseSpec must canonicalize fit (default contain, case-insensitive cover) and
//     format (jpeg→jpg), accept png/jpg/webp, and reject nonpositive sizes, unknown fits, and
//     unknown formats. ParseSpecs must prefix the 1-based entry index to the fault.
//  2. Contain geometry: A 4x2 RGBA image transformed into a 4x4 contain PNG must keep every source
//     pixel in the centered 4x2 band, leave transparent padding above and below, and return exactly
//     4x4 PNG bytes.
//  3. Cover geometry: The same source transformed into a 4x4 cover PNG must fill the whole frame
//     from the centered square crop with no transparent pixel.
//  4. Transparency flattening: A transparent source transformed to JPEG must produce an opaque
//     image whose padding and former transparent pixels flatten onto white.
//  5. Format round trips: Each requested format must decode through image.Decode with the named
//     format and exact target dimensions, including WebP.
//  6. Bad media and tiny sources: Undecodable bytes fail with ErrInputMediaDecode, and a 1x1 source
//     still produces an exact-size result under both fit strategies without an error.

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"

	"github.com/shdeen/bildomat/internal/errs"
	"github.com/shdeen/bildomat/internal/media"
)

// TestParseSpec verifies invariant #1: Spec parsing.
//
// Test class: Expanded.
// Test layer: Coverage.
func TestParseSpec(t *testing.T) {
	validCases := []struct {
		name string
		raw  Spec
		want Spec
	}{
		{"defaults fit to contain", Spec{Width: 100, Height: 200, Format: "png"}, Spec{Width: 100, Height: 200, Fit: FitContain, Format: media.FormatPNG}},
		{"uppercase fit", Spec{Width: 8, Height: 8, Fit: "COVER", Format: "PNG"}, Spec{Width: 8, Height: 8, Fit: FitCover, Format: media.FormatPNG}},
		{"jpeg spelling canonicalizes", Spec{Width: 8, Height: 8, Fit: "contain", Format: "JPEG"}, Spec{Width: 8, Height: 8, Fit: FitContain, Format: media.FormatJPG}},
		{"webp accepted", Spec{Width: 8, Height: 8, Fit: "cover", Format: "webp"}, Spec{Width: 8, Height: 8, Fit: FitCover, Format: media.FormatWebP}},
	}

	for _, testCase := range validCases {
		t.Run(testCase.name, func(t *testing.T) {
			got, err := ParseSpec(testCase.raw)
			if err != nil {
				t.Fatalf("💣 ParseSpec(%+v): %v", testCase.raw, err)
			}

			if got != testCase.want {
				t.Errorf("✗ ParseSpec(%+v) = %+v, want %+v", testCase.raw, got, testCase.want)
			}
		})
	}

	invalidCases := []struct {
		name      string
		raw       Spec
		wantClass error
	}{
		{"zero width", Spec{Width: 0, Height: 8, Fit: FitContain, Format: media.FormatPNG}, errs.ErrInputMediaSize},
		{"negative height", Spec{Width: 8, Height: -1, Fit: FitContain, Format: media.FormatPNG}, errs.ErrInputMediaSize},
		{"unknown fit", Spec{Width: 8, Height: 8, Fit: "stretch", Format: media.FormatPNG}, errs.ErrInputMediaFit},
		{"unknown format", Spec{Width: 8, Height: 8, Fit: FitContain, Format: "gif"}, errs.ErrInputMediaFormat},
		{"empty format", Spec{Width: 8, Height: 8, Fit: FitContain, Format: ""}, errs.ErrInputMediaFormat},
	}

	for _, testCase := range invalidCases {
		t.Run(testCase.name, func(t *testing.T) {
			if _, err := ParseSpec(testCase.raw); !errors.Is(err, testCase.wantClass) {
				t.Errorf("✗ ParseSpec(%+v) error = %v, want a %v classification", testCase.raw, err, testCase.wantClass)
			}
		})
	}

	_, err := ParseSpecs([]Spec{
		{Width: 8, Height: 8, Format: "png"},
		{Width: 0, Height: 8, Format: "png"},
	})
	if err == nil {
		t.Errorf("✗ ParseSpecs accepted a zero-width entry")
	} else if !errors.Is(err, errs.ErrInputMediaSize) || !strings.Contains(err.Error(), "export variant 2") {
		t.Errorf("✗ ParseSpecs fault = %v, want the second entry named with its 1-based index", err)
	}

	parsed, parseErr := ParseSpecs(nil)
	if parseErr != nil || len(parsed) != 0 {
		t.Errorf("✗ ParseSpecs(nil) = (%v, %v), want an empty, valid list", parsed, parseErr)
	}

	if !t.Failed() {
		t.Log("✓ variant specs validate dimensions, fit, and format and number invalid entries")
	}
}

// TestTransformContainGeometry verifies invariant #2: Contain geometry.
//
// Test class: Expanded.
// Test layer: Coverage.
func TestTransformContainGeometry(t *testing.T) {
	source := gradientPNG(t, 4, 2)

	spec := Spec{Width: 4, Height: 4, Fit: FitContain, Format: media.FormatPNG}

	transformed, err := Transform(source, spec)
	if err != nil {
		t.Fatalf("💣 contain transform: %v", err)
	}

	frame, format := decodeVariant(t, transformed)
	if format != media.FormatPNG || frame.Bounds().Dx() != 4 || frame.Bounds().Dy() != 4 {
		t.Fatalf("✗ transformed output = %s %v, want png 4x4", format, frame.Bounds())
	}

	// The 4x2 source letterboxes into rows 1 and 2; rows 0 and 3 stay fully transparent.
	for x := range 4 {
		for _, transparentRow := range []int{0, 3} {
			if _, _, _, alpha := frame.At(x, transparentRow).RGBA(); alpha != 0 {
				t.Errorf("✗ contain padding at (%d,%d) is opaque, want transparent", x, transparentRow)
			}
		}
	}

	// The center band carries source colors: the corner pixels survive in the band's corners.
	wantBlack := color.RGBA{R: 0, G: 0, B: 0, A: 255}
	if got := color.RGBAModel.Convert(frame.At(0, 1)).(color.RGBA); got != wantBlack {
		t.Errorf("✗ contain band top-left pixel = %v, want the source's black corner", got)
	}

	wantWhite := color.RGBA{R: 255, G: 0, B: 255, A: 255}
	if got := color.RGBAModel.Convert(frame.At(3, 2)).(color.RGBA); got != wantWhite {
		t.Errorf("✗ contain band bottom-right pixel = %v, want the source's magenta corner", got)
	}

	if !t.Failed() {
		t.Log("✓ contain preserves aspect ratio with transparent letterboxing at exact frame size")
	}
}

// TestTransformCoverGeometry verifies invariant #3: Cover geometry.
//
// Test class: Expanded.
// Test layer: Coverage.
func TestTransformCoverGeometry(t *testing.T) {
	source := gradientPNG(t, 4, 2)

	spec := Spec{Width: 4, Height: 4, Fit: FitCover, Format: media.FormatPNG}

	transformed, err := Transform(source, spec)
	if err != nil {
		t.Fatalf("💣 cover transform: %v", err)
	}

	frame, format := decodeVariant(t, transformed)
	if format != media.FormatPNG || frame.Bounds().Dx() != 4 || frame.Bounds().Dy() != 4 {
		t.Fatalf("✗ transformed output = %s %v, want png 4x4", format, frame.Bounds())
	}

	for y := range 4 {
		for x := range 4 {
			if _, _, _, alpha := frame.At(x, y).RGBA(); alpha == 0 {
				t.Fatalf("✗ cover left a transparent pixel at (%d,%d); the frame must be fully covered", x, y)
			}
		}
	}

	// A 4x2 source covering a square takes the centered 2x2 crop: x in [1, 3). Its corners are
	// mid-gray reds, never the source's outer black and white columns.
	corner := color.RGBAModel.Convert(frame.At(0, 0)).(color.RGBA)
	if corner.R == 0 || corner.R == 255 {
		t.Errorf("✗ cover frame corner = %v, want a centered-crop red rather than an outer column", corner)
	}

	if !t.Failed() {
		t.Log("✓ cover center-crops and fills the whole frame at the exact target size")
	}
}

// TestTransformFlattensTransparency verifies invariant #4: Transparency flattening.
//
// Test class: Expanded.
// Test layer: Coverage.
func TestTransformFlattensTransparency(t *testing.T) {
	// A 2x1 source in a 4x4 contain frame occupies the centered 4x2 band, leaving rows 0 and 3 as
	// white padding in addition to flattening the source's own transparent pixels.
	img := image.NewRGBA(image.Rect(0, 0, 2, 1))
	img.Set(0, 0, color.RGBA{R: 10, G: 20, B: 30, A: 255})
	img.Set(1, 0, color.RGBA{R: 10, G: 20, B: 30, A: 0})

	var source bytes.Buffer
	if err := png.Encode(&source, img); err != nil {
		t.Fatalf("💣 encode transparent fixture: %v", err)
	}

	transformed, err := Transform(source.Bytes(), Spec{Width: 4, Height: 4, Fit: FitContain, Format: media.FormatJPG})
	if err != nil {
		t.Fatalf("💣 JPEG transform of a transparent source: %v", err)
	}

	frame, format := decodeVariant(t, transformed)
	if format != media.FormatJPEG {
		t.Fatalf("✗ output format = %q, want jpeg", format)
	}

	white := color.RGBA{R: 255, G: 255, B: 255, A: 255}

	if got := color.RGBAModel.Convert(frame.At(0, 0)).(color.RGBA); got != white {
		t.Errorf("✗ JPEG corner padding = %v, want flattened white", got)
	}

	for y := range 4 {
		for x := range 4 {
			if _, _, _, alpha := frame.At(x, y).RGBA(); alpha != 0xffff {
				t.Fatalf("✗ JPEG pixel (%d,%d) retains alpha %#x; the frame must be opaque", x, y, alpha)
			}
		}
	}

	if !t.Failed() {
		t.Log("✓ transparent sources and contain padding flatten onto white under JPEG")
	}
}

// TestTransformFormats verifies invariant #5: Format round trips.
//
// Test class: Expanded.
// Test layer: Coverage.
func TestTransformFormats(t *testing.T) {
	source := gradientPNG(t, 6, 4)

	// image.Decode reports JPEG under its registered "jpeg" spelling; the spec keeps the jpg
	// extension spelling the output layer uses.
	decodedNames := map[string]string{media.FormatPNG: "png", media.FormatJPG: "jpeg", media.FormatWebP: "webp"}

	for _, format := range []string{media.FormatPNG, media.FormatJPG, media.FormatWebP} {
		t.Run(format, func(t *testing.T) {
			transformed, err := Transform(source, Spec{Width: 12, Height: 8, Fit: FitCover, Format: format})
			if err != nil {
				t.Fatalf("💣 %s transform: %v", format, err)
			}

			frame, decodedFormat := decodeVariant(t, transformed)
			if decodedFormat != decodedNames[format] || frame.Bounds().Dx() != 12 || frame.Bounds().Dy() != 8 {
				t.Errorf("✗ %s output = %s %v, want %s 12x8", format, decodedFormat, frame.Bounds(), decodedNames[format])
			}
		})
	}

	spec := Spec{Width: 12, Height: 8, Fit: FitContain, Format: media.FormatPNG}
	if got := spec.NameSuffix(); got != "-12x8-contain" {
		t.Errorf("✗ NameSuffix = %q, want -12x8-contain", got)
	}

	if got := spec.Ext(); got != ".png" {
		t.Errorf("✗ Ext = %q, want .png", got)
	}

	if !t.Failed() {
		t.Log("✓ png, jpg, and webp variants all decode at the exact target dimensions")
	}
}

// TestTransformBadAndTinySources verifies invariant #6: Bad media and tiny sources.
//
// Test class: Expanded.
// Test layer: Coverage.
func TestTransformBadAndTinySources(t *testing.T) {
	if _, err := Transform([]byte("not an image"), Spec{Width: 4, Height: 4, Fit: FitContain, Format: media.FormatPNG}); !errors.Is(err, errs.ErrInputMediaDecode) {
		t.Errorf("✗ bad media error = %v, want ErrInputMediaDecode", err)
	}

	tiny := gradientPNG(t, 1, 1)

	for _, fit := range []string{FitContain, FitCover} {
		transformed, err := Transform(tiny, Spec{Width: 16, Height: 16, Fit: fit, Format: media.FormatPNG})
		if err != nil {
			t.Errorf("💣 1x1 source under %s: %v", fit, err)

			continue
		}

		frame, _ := decodeVariant(t, transformed)
		if frame.Bounds().Dx() != 16 || frame.Bounds().Dy() != 16 {
			t.Errorf("✗ %s of a 1x1 source = %v, want a 16x16 frame", fit, frame.Bounds())
		}
	}

	if !t.Failed() {
		t.Log("✓ undecodable media fails classified, and a 1x1 source still yields exact-size variants")
	}
}

// gradientPNG builds a fixed RGBA image whose red and blue channels ramp across x and y so pixel
// positions remain observable after resampling.
func gradientPNG(t *testing.T, width, height int) []byte {
	t.Helper()

	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := range height {
		for x := range width {
			img.Set(x, y, color.RGBA{
				R: uint8(x * 255 / max(width-1, 1)), //nolint:gosec // bounded 0..255 by construction
				G: 0,
				B: uint8(y * 255 / max(height-1, 1)), //nolint:gosec // bounded 0..255 by construction
				A: 255,
			})
		}
	}

	var encoded bytes.Buffer
	if err := png.Encode(&encoded, img); err != nil {
		t.Fatalf("💣 encode gradient fixture: %v", err)
	}

	return encoded.Bytes()
}

// decodeVariant decodes variant bytes and returns the image alongside image.Decode's format name.
func decodeVariant(t *testing.T, data []byte) (image.Image, string) {
	t.Helper()

	img, format, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("💣 decode variant output: %v", err)
	}

	return img, format
}
