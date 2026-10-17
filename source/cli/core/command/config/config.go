// Package config provides the "config" command group, which locates, checks, shows and edits a
// tool's configuration file, and the context accessors through which a loaded configuration
// reaches a run function.
//
// [New] returns the group for a configuration of struct type T, as [cfg.Load] binds it. The group
// has no run function, so "config" alone prints its help. Its subcommands are:
//
//   - "path" prints the path of the configuration file. It carries [spec.SkipConfig].
//   - "validate" loads the defaults and the file, the file parsed strictly and the result checked
//     by T's [cfg.Validator], and reports every problem it finds. It carries [spec.SkipConfig], so
//     it runs while the file is broken. A missing file is valid: the defaults apply.
//   - "show" prints every key with its value and its origin: "default", the file and line, or the
//     environment variable that set it. It reads the configuration the context carries, stored by
//     [With], so the code that runs before it loads it.
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
	// Editor is the editor "config edit" opens the copy of the file in. Nil runs the program
	// $VISUAL names, else the one $EDITOR names, as the invocation's Env looks them up, split on
	// whitespace and given the copy's path as its last argument, with the invocation's standard
	// streams, each of which must then be an *os.File; with neither variable set, "config edit"
	// fails.
	Editor Editor
}

// Load returns the configuration of every layer: Defaults, under the file at Path, under the
// environment variables of EnvPrefix. inv supplies the environment when LookupEnv is nil. The
// error is the one [cfg.Load] returns, every problem of every layer joined.
func (o Options[T]) Load(inv *spec.Invocation) (cfg.Result[T], error) {
	lookup := o.LookupEnv
	if lookup == nil {
		lookup = inv.Env.LookupEnv
	}
	return cfg.Load(o.Defaults, cfg.File(o.Path), cfg.Env(o.EnvPrefix, lookup))
}

// loadFile returns the configuration of Defaults under the file at Path, without the environment
// layer: what the file itself makes of the configuration.
func (o Options[T]) loadFile() (cfg.Result[T], error) {
	return cfg.Load(o.Defaults, cfg.File(o.Path))
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
