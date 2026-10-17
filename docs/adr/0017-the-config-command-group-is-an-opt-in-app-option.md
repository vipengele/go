# The config command group is an opt-in App option

Status: accepted. It builds on `0016-pre-run-hooks-return-the-context.md` and on
`0012-configuration-is-a-provider-based-project.md`, whose loader it binds to the App.

A tool built on `cli.App` passes `cli.WithConfig(defaults)` and gets two things: a `config` group
with the subcommands `path`, `show`, `validate` and `edit`, and the configuration loaded before
every command that is not annotated `spec.SkipConfig`, which a run function reads with
`cli.ConfigFrom[T](ctx)`. Without the option the App has no `config` command and reads no file.

The group is made of `*spec.Command` values (ADR-0005), not cobra commands: cobra lives only in
`internal/cobra`, so the issue's "cobra constructor" is `config.New`, which returns a `*spec.Command`.

## Decisions

- **Home.** The group is the public package `cli/core/command/config`, and `command/` is the root
  of every command the library provides. The `version` command moves there as `command/version`.
  `cli/core` has no release, so the move breaks no consumer.
- **Path.** The file is `<Dirs.Config()>/config.yaml`, the Config directory `cli/spec/paths` resolves
  for the App's `spec.Identity`. No `--config` flag and no environment variable names another
  file, so every tool has one place to look and `config path` always answers it.
- **Loading.** `WithConfig[T]` mounts the group and adds a pre-run hook that loads the layers
  defaults, under the file, under the environment, the environment read through `inv.Env.LookupEnv`
  with the identity's `EnvPrefix`. Config keys generate no flags. The hook seeds no default file; a
  missing file is valid and loads as the defaults. The hook runs ahead of every `WithPreRun` hook
  whatever the order of the options, so a user hook can read the configuration.
- **Exposure.** The hook stores the `cfg.Result[T]` in the context (ADR-0016). The context key and
  the accessors `config.With[T]` and `config.From[T]` live in the leaf package `command/config`,
  which never imports `cli`; `cli` imports it to mount the group and store the result, so there is
  no import cycle. `cli.ConfigFrom[T]` is a thin wrapper, so a tool need not import the package.
  `ok` is false when no configuration is loaded: the App has no `WithConfig` of type `T`, or the
  command is skipped.
- **Scope of the checks.** `validate` and the check inside `edit` load the file layer only, the
  defaults under the file, parsed strictly and checked by the type's `Validator`. They judge the
  file, not the shell it is run from. `show` reports the full stack with the origin of each value:
  `default`, the file and line, or the environment variable.
- **`edit`.** The editor is `$VISUAL`, then `$EDITOR`, read through `inv.Env.LookupEnv` and split on
  whitespace so `code -w` works; with neither, it fails. It needs a terminal on the standard input.
  It makes a temporary directory beside the file and edits a copy there, named `config.yaml` so
  problems read `config.yaml:3`; a missing file is seeded with the commented default. An invalid
  result prints its problems and prompts `re-edit? [Y/n]` on the same copy; declining, or leaving
  the copy unchanged, leaves the original untouched. A valid result is chmodded 0600 and renamed
  over the file by `internal/atomicfile`, which syncs the file and its directory; the chmod follows
  the editor because an editor that saves by rename leaves a new inode at the umask default. The
  temporary directory is removed on every exit. The default launcher hands the editor the
  invocation's standard streams as they are and so requires all three to be `*os.File`, because
  `exec.Cmd` copies any other reader or writer through a pipe and the editor then has no terminal;
  tests inject an `Editor` instead.
- **Skips.** `path`, `validate`, `edit` and the App's `version` carry `spec.SkipConfig`. Annotations
  belong to one command and are not inherited, so each says so. `show` is not skipped: it reads the
  loaded configuration and fails on a broken file, and `validate` is the diagnostic.
- **JSON.** `show --json` and `path --json` declare no flag. They read `inv.Output`, so they write
  JSON only when the tool opted into `WithJSON` or `WithOutput`; otherwise they write text.
- **Errors.** A configuration that does not load fails the pre-run hook and exits 1 (`ExitError`),
  with every problem of every layer in the error tree. The refusal of `edit` on a standard input
  that is not a terminal also exits 1, not 2: the usage-error type lives in `internal/cobra`, and
  the leaf package never imports a CLI framework.

## Considered options

- **A new project `source/cli/config`.** A project of its own needs a lydite component, a `gomod`
  entry, a `go.work` line and a Worker deploy, for a package that only joins `cli/spec` and the
  loader.
- **Config in `cli/spec`.** Its root package does no I/O (ADR-0012), and `edit` runs an editor and
  writes files.
- **A loader-owned `--config` flag or environment variable.** It would put a second source of the
  path in every tool and make `config path` ambiguous.
- **Typed values on `Invocation`.** `cli/spec` would name every tool's configuration type
  (ADR-0016).
- **Flags generated per config key.** The set of flags would depend on the type, collide with the
  standard flags, and duplicate what the file and the environment already say.
- **Validating the full stack in `validate` and `edit`.** A variable set in the shell would make a
  file look broken, or hide that it is, and `edit` would refuse a valid file because of the
  environment of whoever runs it.
- **A groups-only option.** Returning the group for the tool to add leaves the loading to every
  tool, and the hook order, the skip annotations and the context storage would be reimplemented in
  each `main`.

## Consequences

- **The tree has a reserved name.** Under `WithConfig` the root holds `config` after the added
  commands, so a tool's own `config` command makes the tree invalid, as `version` does.
- **`show` is text only without a standard output flag.** A tool that wants `show --json` takes
  `WithJSON` or `WithOutput`.
- **A broken file stops every command but the skipped ones.** The error exits 1 before the run
  function; `validate`, `edit`, `path` and `version` still run.
- **`cli/core` requires the config project.** It is a sibling dependency (ADR-0006); no other
  project imports the loader.
