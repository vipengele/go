package cli

import (
	"context"
	"fmt"
	"path/filepath"

	cfg "go.vipengele.org/config"

	"go.vipengele.org/cli/core/command/config"
	"go.vipengele.org/cli/spec"
	"go.vipengele.org/cli/spec/paths"
)

// configFile is the base name of the configuration file in the tool's configuration directory.
const configFile = "config.yaml"

// configBinding is the configuration of one execution: the "config" group its root holds and the
// pre-run hook that loads the configuration before every other hook.
type configBinding struct {
	group *spec.Command
	hook  PreRunHook
}

// configurer binds the configuration [WithConfig] describes to the file at path, with the
// environment variables of envPrefix.
type configurer func(path, envPrefix string) configBinding

// WithConfig gives the App a configuration of struct type T, as go.vipengele.org/config binds it,
// starting from defaults. Without it the App has no "config" command and reads no configuration
// file. Given more than once, the last one decides.
//
// The configuration file is config.yaml in the tool's configuration directory, the Config
// directory the cli/spec/paths package resolves for the App's [spec.Identity] from the
// environment of each execution. No flag and no environment variable names another file. The
// layers are defaults, under the file, under the environment variables named by the identity's
// EnvPrefix, looked up through [spec.Env.LookupEnv]. A missing file is valid: the defaults and the
// environment apply.
//
// Before every run function, ahead of every [WithPreRun] hook whatever the order of the options,
// a pre-run hook loads the configuration and stores it in the context, where [ConfigFrom] reads
// it. A command annotated [spec.SkipConfig] runs without it, as the App's "version" command does.
// A configuration that does not load stops the execution as any pre-run hook's error does: the
// error, "load config" over every problem of every layer, each on a row of its own, is written
// and the exit code is [ExitError] unless the error carries a code of its own. So does a configuration directory that cannot be resolved,
// such as one without a home directory; every subcommand of "config" then fails with that error.
//
// The root holds the "config" group of the go.vipengele.org/cli/core/command/config package
// after the added commands and before "version", so a tool does not add a command named "config":
// one it adds makes the tree invalid.
func WithConfig[T any](defaults T) Option {
	return func(s *settings) {
		s.config = func(path, envPrefix string) configBinding {
			opts := config.Options[T]{Path: path, Defaults: defaults, EnvPrefix: envPrefix}
			return configBinding{
				group: config.New(opts),
				hook: func(ctx context.Context, inv *spec.Invocation, cmd spec.CommandInfo) (context.Context, error) {
					if _, skip := cmd.Annotation(spec.SkipConfig); skip {
						return ctx, nil
					}
					res, err := opts.Load(inv)
					if err != nil {
						return ctx, fmt.Errorf("load config: %w", err)
					}
					return config.With(ctx, res), nil
				},
			}
		}
	}
}

// ConfigFrom returns the configuration of type T the hook of [WithConfig] loaded for the command
// that runs, from the context its run function, a later [PreRunHook] or a [PostRunHook] receives,
// and whether the context carries one. It reports false when the App has no [WithConfig] of type
// T and when the command carries [spec.SkipConfig].
func ConfigFrom[T any](ctx context.Context) (cfg.Result[T], bool) {
	return config.From[T](ctx)
}

// configFor binds the App's configuration for one execution against lookup, the environment the
// configuration directory is resolved from. It returns the zero binding when the App has no
// [WithConfig]. When the directory cannot be resolved, the hook and every run function of the
// group return that error.
func (a *App) configFor(lookup func(string) (string, bool)) configBinding {
	if a.settings.config == nil {
		return configBinding{}
	}
	dirs, err := paths.Resolve(a.identity, paths.Options{LookupEnv: lookup}, paths.Config)
	if err != nil {
		err = fmt.Errorf("config: %w", err)
		b := a.settings.config("", a.identity.EnvPrefix)
		failRuns(b.group, err)
		b.hook = func(ctx context.Context, _ *spec.Invocation, cmd spec.CommandInfo) (context.Context, error) {
			if _, skip := cmd.Annotation(spec.SkipConfig); skip {
				return ctx, nil
			}
			return ctx, err
		}
		return b
	}
	return a.settings.config(filepath.Join(dirs.Config(), configFile), a.identity.EnvPrefix)
}

// failRuns makes every run function of cmd and its subcommands return err.
func failRuns(cmd *spec.Command, err error) {
	if cmd.Run != nil {
		cmd.Run = func(context.Context, *spec.Invocation) error { return err }
	}
	for _, c := range cmd.Commands {
		failRuns(c, err)
	}
}
