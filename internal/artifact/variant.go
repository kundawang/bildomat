package artifact

import (
	"errors"
	"os"
	"strings"

	"github.com/shdeen/bildomat/internal/errs"
	"github.com/shdeen/bildomat/internal/media"
	"github.com/shdeen/bildomat/internal/variant"
)

// variantSourceName labels an artifact in a variant-processing error.
func variantSourceName(generated Media) string {
	if generated.TmpPath != "" {
		return generated.TmpPath
	}

	return "generated " + generated.FileExt + " artifact"
}

// variantFailure classifies a failure while processing one artifact's variants.
func variantFailure(generated Media, cause error) error {
	return &errs.MediaError{Source: variantSourceName(generated), Cause: cause}
}

// mediaBytes returns an artifact's bytes, reading a temporary source when the provider downloaded
// the media to disk. Local and remote results therefore enter variant export through one function.
func mediaBytes(generated Media) ([]byte, error) {
	if generated.TmpPath == "" {
		return generated.Data, nil
	}

	// #nosec G304 -- the path is a bild-owned temporary download created this run.
	data, err := os.ReadFile(generated.TmpPath)
	if err != nil {
		return nil, errs.FileError(errs.FileOpRead, generated.TmpPath, errs.ErrOutputFileRead, err)
	}

	return data, nil
}

// variantName renders the destination stem for one artifact's variant. Batch artifacts keep their
// -N index ahead of the variant suffix, matching how WriteMedia names the originals.
func variantName(stem string, artifacts []Media, artifactIndex int, spec variant.Spec) string {
	return artifactName(stem, artifacts, artifactIndex) + spec.NameSuffix()
}

// WriteVariants derives and saves every requested variant for the supplied image artifacts. It
// returns the completed files in artifact then spec order. A non-image artifact is skipped without
// an error. Any decode, encode, or destination failure stops further writes; files already
// completed remain and are returned with the error, so the caller never reports a successful file
// that does not exist.
func WriteVariants(dir, stem string, artifacts []Media, specs []variant.Spec) (savedFiles []SavedFile, writeErr error) {
	if len(specs) == 0 {
		return nil, nil
	}

	savedFiles = make([]SavedFile, 0, len(artifacts)*len(specs))

	for artifactIndex := range artifacts {
		generated := &artifacts[artifactIndex]
		if !imageExtension(generated.FileExt) {
			continue
		}

		sourceBytes, err := mediaBytes(*generated)
		if err != nil {
			return savedFiles, errors.Join(writeErr, variantFailure(*generated, err))
		}

		for specIndex := range specs {
			spec := &specs[specIndex]

			variantBytes, transformErr := variant.Transform(sourceBytes, *spec)
			if transformErr != nil {
				return savedFiles, errors.Join(writeErr, variantFailure(*generated, transformErr))
			}

			savedFile, claimErr := writeArtifact(dir, variantName(stem, artifacts, artifactIndex, *spec), Media{
				Data: variantBytes, FileExt: spec.Ext(),
			})
			writeErr = errors.Join(writeErr, claimErr)

			if savedFile.Path == "" {
				return savedFiles, writeErr
			}

			savedFiles = append(savedFiles, savedFile)
		}
	}

	return savedFiles, writeErr
}

// imageExtension reports whether an extension names an image format bild can decode. The .jpeg
// spelling canonicalizes to .jpg like the rest of output handling.
func imageExtension(ext string) bool {
	switch strings.ToLower(ext) {
	case "." + media.FormatPNG, "." + media.FormatJPG, "." + media.FormatJPEG, "." + media.FormatWebP:
		return true
	}

	return false
}
