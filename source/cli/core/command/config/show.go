package config

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

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
	// Layers lists the layers of the configuration, lowest first, each with the keys "name" and
	// "path". The names are "defaults", "file", "overlay" (only when there is an overlay file) and
	// "env"; the path is "" for a layer without a file.
	Layers []layerJSON `json:"layers"`
	// Values holds one object per key, with the keys "key", "value" and "origin".
	Values []table.Row `json:"values"`
}

// layerJSON is one layer of the configuration in the payload of "config show".
type layerJSON struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

// layers returns the layers of the configuration, lowest first.
func layers[T any](opts Options[T], overlayPath string) []layerJSON {
	out := []layerJSON{{Name: "defaults"}, {Name: "file", Path: opts.Path}}
	if overlayPath != "" {
		out = append(out, layerJSON{Name: "overlay", Path: overlayPath})
	}
	return append(out, layerJSON{Name: "env"})
}

// envLayer describes the variables of the environment layer in the text header: the prefix with
// its joining underscore, or every variable when there is no prefix.
func envLayer(prefix string) string {
	if prefix == "" {
		return "variables named after their keys"
	}
	return prefix + "_*"
}

// newShow returns "config show", which prints every key of the configuration the context
// carries, its value and its origin, in the order of T's fields. Text is a header naming each
// layer, defaults, file, overlay (when there is one) and env, with its path or prefix, a blank
// line and a table with the columns KEY, VALUE and ORIGIN; with JSON output the payload holds the
// path, the layers and one object per key. The overlay is resolved afresh, and an error of its
// resolver fails the command. Without a configuration of type T in the context it returns an error
// matching [ErrNotLoaded].
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
			overlayPath, _, err := opts.overlay(inv)
			if err != nil {
				return err
			}
			all := layers(opts, overlayPath)
			tbl := table.New(
				table.Column{Key: "key", Header: "KEY"},
				table.Column{Key: "value", Header: "VALUE"},
				table.Column{Key: "origin", Header: "ORIGIN"},
			)
			for _, s := range res.Sources() {
				tbl.Add(s.Key, s.Value, s.Origin)
			}
			if inv.Output.Format() == spec.FormatJSON {
				return spec.WriteJSON(inv.Env.Stdout, schema, showJSON{Path: opts.Path, Layers: all, Values: tbl.Rows()})
			}
			var header strings.Builder
			for _, l := range all {
				detail := l.Path
				switch l.Name {
				case "defaults":
					detail = "the values of the fields"
				case "env":
					detail = envLayer(opts.EnvPrefix)
				}
				fmt.Fprintf(&header, "%-9s%s\n", l.Name, detail)
			}
			header.WriteString("\n")
			if _, err := io.WriteString(inv.Env.Stdout, header.String()); err != nil {
				return err
			}
			return tbl.Render(inv.Env.Stdout, inv.Output.Styler(spec.Stdout))
		},
	}
}
