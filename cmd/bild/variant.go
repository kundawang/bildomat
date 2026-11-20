package main

import (
	userconfig "github.com/shdeen/bildomat/internal/config"
	"github.com/shdeen/bildomat/internal/errs"
	"github.com/shdeen/bildomat/internal/variant"
)

// exportVariantsKey names the YAML setting in configuration-fault messages.
const exportVariantsKey = "export-variants"

// parseExportVariants canonicalizes the config file's raw variant entries. An invalid entry fails
// the whole list with one configuration fault, so a partially applied variant set can never reach a
// run.
func parseExportVariants(raw []userconfig.RawVariant, configPath string) ([]variant.Spec, []error) {
	if len(raw) == 0 {
		return nil, nil
	}

	specs := make([]variant.Spec, 0, len(raw))
	for index := range raw {
		specs = append(specs, variant.Spec{
			Width: raw[index].Width, Height: raw[index].Height,
			Fit: raw[index].Fit, Format: raw[index].Format,
		})
	}

	parsed, err := variant.ParseSpecs(specs)
	if err != nil {
		fault := &errs.ConfigError{
			Path: configPath, Setting: exportVariantsKey, Problem: err.Error(),
			Cause: errs.ErrUserConfigInvalidVariant,
		}

		return nil, []error{fault}
	}

	return parsed, nil
}
