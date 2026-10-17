package config

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"

	"go.vipengele.org/cli/spec"
)

// pathJSON is the JSON payload of "config path".
type pathJSON struct {
	// Path is the configuration file.
	Path string `json:"path"`
	// Exists reports whether a file is at Path.
	Exists bool `json:"exists"`
}

// newPath returns "config path", which prints opts.Path and a newline, or with JSON output the
// path and whether a file is there. It only looks at the path: it creates nothing.
func newPath[T any](opts Options[T]) *spec.Command {
	return &spec.Command{
		Name:        "path",
		Summary:     "Print the path of the configuration file",
		Args:        spec.NoArgs(),
		Annotations: skipConfig(),
		Run: func(_ context.Context, inv *spec.Invocation) error {
			if inv.Output.Format() != spec.FormatJSON {
				return inv.Data("%s\n", opts.Path)
			}
			exists, err := fileExists(opts.Path)
			if err != nil {
				return err
			}
			return spec.WriteJSON(inv.Env.Stdout, schema, pathJSON{Path: opts.Path, Exists: exists})
		},
	}
}

// fileExists reports whether anything is at path. An error other than the path not existing,
// such as a directory that cannot be searched, is returned.
func fileExists(path string) (bool, error) {
	_, err := os.Stat(path)
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, fs.ErrNotExist):
		return false, nil
	default:
		return false, fmt.Errorf("config file %s: %w", path, err) // [lydite:exclude_from_mutation][path and validate return the error without reading the bool returned with it, so it is unobservable]
	}
}
