// Package config provides the "config" command group, which locates, checks, shows and edits a
// tool's configuration file, and the context accessors through which a loaded configuration
// reaches a run function.
//
// [New] returns the group for a configuration of struct type T, as [cfg.Load] binds it. The group
// has no run function, so "config" alone prints its help. Its subcommands are:
//
//   - "path" prints the path of the configuration file. It carries [spec.SkipConfig].
//   - "validate" loads the defaults, the file and the overlay file, each parsed strictly and the
//     result checked by T's [cfg.Validator], without the environment, and reports every problem it
//     finds. On success it names the full path of each file it covered. It carries
//     [spec.SkipConfig], so it runs while a file is broken. A missing file is valid: the defaults
//     apply.
//   - "show" names each layer, defaults, file, overlay and env, with its path, then prints every
//     key with its value and its origin: "default", the file and line, or the environment variable
//     that set it. It reads the configuration the context carries, stored by [With], so the code
//     that runs before it loads it.
//   - "edit" opens a copy of the file in an editor and moves it over the file only once the copy
//     is valid, checked as "validate" checks the file; the file it writes has mode 0600. It needs
//     a terminal on the standard input and carries [spec.SkipConfig], so it repairs a broken file.
//
// "path" and "show" write JSON when [spec.Output.Format] is [spec.FormatJSON], as it is under
// the standard --json or --output json of a cli.App; neither declares a flag of its own.
//
// The package never imports a CLI framework, and no subcommand but "edit" creates a directory.
package config

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"

	cfg "go.vipengele.org/config"

	"go.vipengele.org/cli/spec"
)

// schema is the version of the JSON shape every subcommand of the group writes.
const schema = 1

// Editor opens the file at path for the user to edit, with the streams and environment of env,
// and returns once the user is done with it.
type Editor func(ctx context.Context, env *spec.Env, path string) error

// Options describes the configuration the group of [New] works on.
type Options[T any] struct {
	// Path is the configuration file, such as "<config dir>/config.yaml". It need not exist.
	Path string
	// Defaults is the lowest layer: every key starts at the value of its field.
	Defaults T
	// EnvPrefix is the prefix of the environment variable of each key, without the joining
	// underscore, as [cfg.Env] takes it. "" derives each variable from its key alone.
	EnvPrefix string
	// LookupEnv looks up the environment variables of the environment layer, as os.LookupEnv
	// does. Nil reads the LookupEnv of the invocation's Env.
	LookupEnv func(key string) (string, bool)
	// Overlay resolves the path of the overlay file, a layer between the file at Path and the
	// environment, given the lookup of the environment layer. It runs on every [Options.Load], so
	// each execution resolves it afresh. A path that is "" or names no file gives no overlay
	// layer. Nil gives no overlay.
	Overlay func(lookup func(key string) (string, bool)) (string, error)
	// OverlayAllow lists the keys the overlay file may set, as [cfg.FileProvider.Allow] takes
	// them: any other key the overlay sets is a problem at its line. Empty allows every key.
	OverlayAllow []string
	// Editor is the editor "config edit" opens the copy of the file in. Nil runs the program
	// $VISUAL names, else the one $EDITOR names, as the invocation's Env looks them up, split on
	// whitespace and given the copy's path as its last argument, with the invocation's standard
	// streams, each of which must then be an *os.File; with neither variable set, "config edit"
	// fails.
	Editor Editor
}

// Load returns the configuration of every layer: Defaults, under the file at Path, under the
// overlay file Overlay resolves, restricted to OverlayAllow, under the environment variables of
// EnvPrefix. inv supplies the environment when LookupEnv is nil. The error is the one of
// [Options.files] when the overlay does not resolve, and otherwise the one [cfg.Load] returns,
// every problem of every layer joined.
func (o Options[T]) Load(inv *spec.Invocation) (cfg.Result[T], error) {
	files, err := o.files(inv)
	if err != nil {
		return cfg.Result[T]{}, err
	}
	return cfg.Load(o.Defaults, append(files, cfg.Env(o.EnvPrefix, o.lookup(inv)))...)
}

// lookup returns the lookup of the environment layer: LookupEnv, or the LookupEnv of inv's Env
// when it is nil.
func (o Options[T]) lookup(inv *spec.Invocation) func(string) (string, bool) {
	if o.LookupEnv != nil {
		return o.LookupEnv
	}
	return inv.Env.LookupEnv
}

// OverlayPath returns the path of the overlay file for inv, as [Options.Load] resolves it, or ""
// when there is no overlay layer. Each call runs Overlay afresh; its error is the one Load returns
// when the overlay does not resolve.
func (o Options[T]) OverlayPath(inv *spec.Invocation) (string, error) {
	path, _, err := o.overlay(inv)
	return path, err
}

// files returns the file layers, lowest first, without the environment layer: the file at Path,
// followed by the overlay file when [Options.overlay] finds one.
func (o Options[T]) files(inv *spec.Invocation) ([]cfg.Provider, error) {
	_, overlay, err := o.overlay(inv)
	if err != nil {
		return nil, err
	}
	return append([]cfg.Provider{cfg.File(o.Path)}, overlay...), nil
}

// overlay resolves the overlay file for inv and returns its path and its layer, restricted to
// OverlayAllow. It returns "" and no layer when Overlay is nil, and when the path is "" or names
// no file: an empty path stats as a missing file. An error of Overlay, and any error of the stat
// but a missing file, such as a path through a regular file, is returned naming the overlay. A
// path that stats but cannot be read, such as a directory, is the overlay layer's error when it
// loads.
func (o Options[T]) overlay(inv *spec.Invocation) (string, []cfg.Provider, error) {
	if o.Overlay == nil {
		return "", nil, nil
	}
	path, err := o.Overlay(o.lookup(inv))
	if err != nil {
		return "", nil, fmt.Errorf("config overlay: %w", err)
	}
	_, err = os.Stat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil, nil
	}
	if err != nil {
		return "", nil, fmt.Errorf("config overlay %s: %w", path, err)
	}
	return path, []cfg.Provider{cfg.File(path).Allow(o.OverlayAllow...)}, nil
}

// New returns the "config" group for opts, holding the subcommands commands builds, in order.
func New[T any](opts Options[T]) *spec.Command {
	group := &spec.Command{
		Name:        "config",
		Summary:     "Locate, check, show and edit the configuration file",
		Description: "Locate, check, show and edit the configuration file at " + opts.Path + ".",
	}
	for _, build := range commands[T]() {
		group.Commands = append(group.Commands, build(opts))
	}
	return group
}

// commands returns the constructors of the group's subcommands, in the order the group lists them.
func commands[T any]() []func(Options[T]) *spec.Command {
	return []func(Options[T]) *spec.Command{
		newPath[T],
		newValidate[T],
		newShow[T],
		newEdit[T],
	}
}

// skipConfig returns the annotations of a subcommand that runs without configuration loaded.
func skipConfig() map[string]string {
	return map[string]string{spec.SkipConfig: ""}
}
