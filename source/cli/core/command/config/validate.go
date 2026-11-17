package config

import (
	"context"
	"fmt"

	cfg "go.vipengele.org/config"

	"go.vipengele.org/cli/spec"
)

// newValidate returns "config validate", which loads opts.Defaults under the file at opts.Path
// and under the overlay file opts.Overlay resolves, restricted to opts.OverlayAllow, and reports
// the result. The environment is not read: the command judges the files alone.
//
// A valid configuration prints a success line on stderr for each file it covered, naming its full
// path, and writes nothing on stdout. Otherwise the command returns the error of the load, which
// holds every problem found: every unknown, removed, repeated or misshapen key of the files, or,
// once they parse into values, every value that does not convert, or else every problem the
// configuration's Validator reports. A file that is not YAML is one such problem. With an overlay
// the error names both files, since a problem carries only the base name of its file. A missing
// file is valid when the defaults are, and an overlay that does not resolve fails the command.
func newValidate[T any](opts Options[T]) *spec.Command {
	return &spec.Command{
		Name:        "validate",
		Summary:     "Check the configuration files",
		Args:        spec.NoArgs(),
		Annotations: skipConfig(),
		Run: func(_ context.Context, inv *spec.Invocation) error {
			overlayPath, layer, err := opts.overlay(inv)
			if err != nil {
				return err
			}
			if _, err := cfg.Load(opts.Defaults, append([]cfg.Provider{cfg.File(opts.Path)}, layer...)...); err != nil {
				if overlayPath != "" {
					return fmt.Errorf("config files %s and %s: %w", opts.Path, overlayPath, err)
				}
				return err
			}
			exists, err := fileExists(opts.Path)
			if err != nil {
				return err
			}
			if exists {
				inv.Success("%s is valid", opts.Path)
			} else {
				inv.Success("%s does not exist; the defaults are valid", opts.Path)
			}
			if overlayPath != "" {
				inv.Success("%s is valid (overlay)", overlayPath)
			}
			return nil
		},
	}
}
