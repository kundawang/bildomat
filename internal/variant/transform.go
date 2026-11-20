package variant

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/jpeg" // registers the JPEG decoder and provides the JPEG encoder
	"image/png"
	"math"

	"github.com/HugoSmits86/nativewebp"
	xdraw "golang.org/x/image/draw"
	_ "golang.org/x/image/webp" // registers the WebP decoder used by image.Decode

	"github.com/shdeen/bildomat/internal/errs"
	"github.com/shdeen/bildomat/internal/media"
)

// jpegQuality is the fixed quality used when a variant requests JPEG.
const jpegQuality = 90

// opaqueWhite is the background onto which transparent pixels and contain padding are flattened for
// encodings without an alpha channel.
const opaqueWhite = 255

// Transform decodes source bytes, fits them into the spec's frame under the spec's fit strategy,
// and encodes the result in the spec's format. The source bytes are the same representation local
// reads and remote downloads produce, so both sources share this one path.
func Transform(source []byte, spec Spec) ([]byte, error) {
	decoded, _, err := image.Decode(bytes.NewReader(source))
	if err != nil {
		return nil, errors.Join(errs.ErrInputMediaDecode, err)
	}

	frame := fit(decoded, spec)

	return encode(frame, spec.Format)
}

// fit returns a Width x Height image holding the source under the requested strategy.
//   - cover scales the source to fill the frame and center-crops the overflow.
//   - contain scales the source inside the frame and pads the margins. JPEG and WebP frames are
//     opaque, so the padding and any source transparency flatten onto white.
func fit(source image.Image, spec Spec) image.Image {
	if spec.Fit == FitCover {
		return coverFrame(source, spec.Width, spec.Height)
	}

	return containFrame(source, spec.Width, spec.Height, opaqueFormat(spec.Format))
}

// coverFrame center-crops the source to the target ratio and scales the crop to the exact frame.
func coverFrame(source image.Image, width, height int) image.Image {
	frame := image.NewRGBA(image.Rect(0, 0, width, height))
	crop := coverCrop(source.Bounds(), width, height).Intersect(source.Bounds())
	xdraw.CatmullRom.Scale(frame, frame.Bounds(), source, crop, xdraw.Over, nil)

	return frame
}

// coverCrop returns the largest centered source rectangle matching the target frame ratio.
func coverCrop(bounds image.Rectangle, width, height int) image.Rectangle {
	sourceWidth, sourceHeight := bounds.Dx(), bounds.Dy()
	targetRatio := float64(width) / float64(height)
	sourceRatio := float64(sourceWidth) / float64(sourceHeight)

	if sourceRatio > targetRatio {
		croppedWidth := int(float64(sourceHeight) * targetRatio)
		cropLeft := bounds.Min.X + (sourceWidth-croppedWidth)/2

		return image.Rect(cropLeft, bounds.Min.Y, cropLeft+croppedWidth, bounds.Max.Y)
	}

	croppedHeight := int(float64(sourceWidth) / targetRatio)
	cropTop := bounds.Min.Y + (sourceHeight-croppedHeight)/2

	return image.Rect(bounds.Min.X, cropTop, bounds.Max.X, cropTop+croppedHeight)
}

// containFrame scales the source to the largest centered rectangle inside the frame. An opaque
// target starts white so transparency and padding never become black.
func containFrame(source image.Image, width, height int, opaque bool) image.Image {
	var frame *image.RGBA
	if opaque {
		frame = whiteFrame(width, height)
	} else {
		frame = image.NewRGBA(image.Rect(0, 0, width, height))
	}

	sourceWidth, sourceHeight := source.Bounds().Dx(), source.Bounds().Dy()
	scale := min(float64(width)/float64(sourceWidth), float64(height)/float64(sourceHeight))
	scaledWidth := max(int(math.Round(float64(sourceWidth)*scale)), 1)
	scaledHeight := max(int(math.Round(float64(sourceHeight)*scale)), 1)

	destination := image.Rect(
		(width-scaledWidth)/2, (height-scaledHeight)/2,
		(width-scaledWidth)/2+scaledWidth, (height-scaledHeight)/2+scaledHeight,
	)
	xdraw.CatmullRom.Scale(frame, destination, source, source.Bounds(), xdraw.Over, nil)

	return frame
}

// whiteFrame returns an RGBA frame prefilled with opaque white.
func whiteFrame(width, height int) *image.RGBA {
	frame := image.NewRGBA(image.Rect(0, 0, width, height))
	white := color.RGBA{R: opaqueWhite, G: opaqueWhite, B: opaqueWhite, A: opaqueWhite}

	for y := range height {
		for x := range width {
			frame.SetRGBA(x, y, white)
		}
	}

	return frame
}

// opaqueFormat reports whether the encoding has no alpha channel and therefore needs white padding.
func opaqueFormat(format string) bool {
	return format == media.FormatJPG || format == media.FormatWebP
}

// encode writes the frame in the requested format. JPEG and WebP encoders reject transparency, so
// their frames must already be opaque; PNG keeps the frame's alpha.
func encode(frame image.Image, format string) ([]byte, error) {
	var encoded bytes.Buffer

	var err error

	switch format {
	case media.FormatPNG:
		err = png.Encode(&encoded, frame)
	case media.FormatJPG:
		err = jpeg.Encode(&encoded, frame, &jpeg.Options{Quality: jpegQuality})
	case media.FormatWebP:
		err = nativewebp.Encode(&encoded, frame, nil)
	default:
		return nil, fmt.Errorf("%q: %w", format, errs.ErrInputMediaFormat)
	}

	if err != nil {
		return nil, errors.Join(errs.ErrInputMediaEncode, err)
	}

	return encoded.Bytes(), nil
}
