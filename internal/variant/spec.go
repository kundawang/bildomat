// Package variant defines export variants: additional image sizes and formats derived from one
// generated or downloaded image after the original artifact has been saved.
package variant

import (
	"fmt"
	"strings"

	"github.com/shdeen/bildomat/internal/errs"
	"github.com/shdeen/bildomat/internal/media"
	"github.com/shdeen/bildomat/internal/params"
)

// Fit strategies select how an image fills a target frame while its aspect ratio is preserved.
//   - FitContain: scale the image inside the frame and pad the remaining margins
//   - FitCover: scale the image to cover the frame and center-crop the overflow
const (
	FitContain = "contain"
	FitCover   = "cover"
)

// Spec describes one export variant.
//   - Width, Height: the exact target frame in pixels; both must be positive
//   - Fit: FitContain or FitCover, applied while preserving the source aspect ratio
//   - Format: one of media.FormatPNG, media.FormatJPG, or media.FormatWebP
type Spec struct {
	Width  int    `yaml:"width"`
	Height int    `yaml:"height"`
	Fit    string `yaml:"fit"`
	Format string `yaml:"format"`
}

// dimensionForm renders a width and height as a dimension value.
const dimensionForm = "%dx%d"

// invalidSizeForm carries the offending dimensions with the size classification.
const invalidSizeForm = "%dx%d: %w"

// variantIndexForm names the 1-based entry that failed validation.
const variantIndexForm = "export variant %d: %w"

// suffixForm renders the distinguishing filename suffix for one variant.
const suffixForm = "-%dx%d-%s"

// exportFormats are the image encodings a variant may request. JPEG accepts both the jpg and jpeg
// spelling and is canonicalized to the jpg extension spelling.
//
//nolint:gochecknoglobals // immutable format lookup, initialized once
var exportFormats = map[string]string{
	media.FormatPNG:  media.FormatPNG,
	media.FormatJPG:  media.FormatJPG,
	media.FormatJPEG: media.FormatJPG,
	media.FormatWebP: media.FormatWebP,
}

// ParseSpec validates one configured variant and returns its canonical form. The raw fields carry
// the YAML spellings; the size text supplies the WxH frame.
func ParseSpec(raw Spec) (Spec, error) {
	width, height, valid := params.ParseDimensions(fmt.Sprintf(dimensionForm, raw.Width, raw.Height))
	if !valid {
		return Spec{}, fmt.Errorf(invalidSizeForm, raw.Width, raw.Height, errs.ErrInputMediaSize)
	}

	fit := strings.ToLower(strings.TrimSpace(raw.Fit))
	if fit == "" {
		fit = FitContain
	}

	if fit != FitContain && fit != FitCover {
		return Spec{}, fmt.Errorf("%q: %w", raw.Fit, errs.ErrInputMediaFit)
	}

	format, supported := exportFormats[strings.ToLower(strings.TrimSpace(raw.Format))]
	if !supported {
		return Spec{}, fmt.Errorf("%q: %w", raw.Format, errs.ErrInputMediaFormat)
	}

	return Spec{Width: width, Height: height, Fit: fit, Format: format}, nil
}

// ParseSpecs validates configured variants in order and returns their canonical forms. An empty
// list is valid and disables variant export. The first invalid entry stops parsing.
func ParseSpecs(raw []Spec) ([]Spec, error) {
	specs := make([]Spec, 0, len(raw))

	for specIndex := range raw {
		parsed, err := ParseSpec(raw[specIndex])
		if err != nil {
			return nil, fmt.Errorf(variantIndexForm, specIndex+1, err)
		}

		specs = append(specs, parsed)
	}

	return specs, nil
}

// NameSuffix renders the filename suffix that distinguishes one variant from the original artifact:
// -1280x720-contain, -300x300-cover, and so on.
func (spec Spec) NameSuffix() string {
	return fmt.Sprintf(suffixForm, spec.Width, spec.Height, spec.Fit)
}

// Ext returns the canonical file extension for the variant's format, including its leading period.
func (spec Spec) Ext() string {
	return "." + spec.Format
}
