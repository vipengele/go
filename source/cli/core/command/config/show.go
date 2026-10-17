package config

import (
	"context"
	"errors"
	"fmt"

	"go.vipengele.org/cli/spec"
	"go.vipengele.org/cli/spec/table"
)

// ErrNotLoaded reports that the context of a command reading the configuration carries none: no
// configuration of its type was stored with [With] before the command ran.
var ErrNotLoaded = errors.New("configuration is not loaded")

// showJSON is the JSON payload of "config show".
type showJSON struct {
	// Path is the configuration file.
	Path string `json:"path"`
	// Values holds one object per key, with the keys "key", "value" and "origin".
	Values []table.Row `json:"values"`
}

// newShow returns "config show", which prints every key of the configuration the context
// carries, its value and its origin, in the order of T's fields. Text is a table with the
// columns KEY, VALUE and ORIGIN; with JSON output the payload holds the path and one object per
// key. Without a configuration of type T in the context it returns an error matching
// [ErrNotLoaded].
func newShow[T any](opts Options[T]) *spec.Command {
	return &spec.Command{
		Name:    "show",
		Summary: "Print every configuration value and where it came from",
		Args:    spec.NoArgs(),
		Run: func(ctx context.Context, inv *spec.Invocation) error {
			res, ok := From[T](ctx)
			if !ok {
				return fmt.Errorf("config show: %w", ErrNotLoaded)
			}
			tbl := table.New(
				table.Column{Key: "key", Header: "KEY"},
				table.Column{Key: "value", Header: "VALUE"},
				table.Column{Key: "origin", Header: "ORIGIN"},
			)
			for _, s := range res.Sources() {
				tbl.Add(s.Key, s.Value, s.Origin)
			}
			if inv.Output.Format() == spec.FormatJSON {
				return spec.WriteJSON(inv.Env.Stdout, schema, showJSON{Path: opts.Path, Values: tbl.Rows()})
			}
			return tbl.Render(inv.Env.Stdout, inv.Output.Styler(spec.Stdout))
		},
	}
}
