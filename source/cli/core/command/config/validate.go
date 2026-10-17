package config

import (
	"context"

	"go.vipengele.org/cli/spec"
)

// newValidate returns "config validate", which loads opts.Defaults under the file at opts.Path
// and reports the result. The environment is not read: the command judges the file alone.
//
// A valid configuration prints a success line on stderr and writes nothing on stdout. Otherwise
// the command returns the error of the load, which holds every problem found: every unknown,
// removed, repeated or misshapen key of the file, or, once the file parses into values, every
// value that does not convert, or else every problem the configuration's Validator reports. A
// file that is not YAML is one such problem. A missing file is valid when the defaults are.
func newValidate[T any](opts Options[T]) *spec.Command {
	return &spec.Command{
		Name:        "validate",
		Summary:     "Check the configuration file",
		Args:        spec.NoArgs(),
		Annotations: skipConfig(),
		Run: func(_ context.Context, inv *spec.Invocation) error {
			if _, err := opts.loadFile(); err != nil {
				return err
			}
			exists, err := fileExists(opts.Path)
			if err != nil {
				return err
			}
			if !exists {
				inv.Success("%s does not exist; the defaults are valid", opts.Path)
				return nil
			}
			inv.Success("%s is valid", opts.Path)
			return nil
		},
	}
}
