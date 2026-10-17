---
about: "cli.WithConfig mounts the config group and prepends a hook that loads config for every command not annotated spec.SkipConfig; the result travels in the context via command/config.With/From, which never imports cli"
saw:
  - source/cli/core/config.go
  - source/cli/core/hooks.go
  - source/cli/core/app.go
  - source/cli/core/command/config/ctx.go
  - source/cli/core/command/config/config.go
  - docs/adr/0016-pre-run-hooks-return-the-context.md
  - docs/adr/0017-the-config-command-group-is-an-opt-in-app-option.md
---

Facts that break silently if changed:

- The config hook is passed to `settings.hookFuncs(first)` (`hooks.go`) and placed ahead of every `WithPreRun`
  hook, so a user hook added before `WithConfig` still sees the config in its context. A hook error stops the run
  (no run function, no post hooks) and exits 1.
- `spec.SkipConfig` is read per command from `cmd.Annotation`; annotations are not inherited, so `path`,
  `validate`, `edit` (`command/config`) and the App's `version` (`App.version` in `app.go`) each carry it. `show` is
  deliberately not skipped.
- The context key and `With`/`From` live in the leaf package `command/config` (`ctx.go`), which must not import
  package `cli` (cycle: `cli` imports it to mount the group). `cli.ConfigFrom` only wraps `config.From`.
- The path is computed per execution in `App.configFor` from `paths.Resolve(identity, ..., paths.Config)`; when the
  directory cannot be resolved every non-skipped command and every `config` subcommand fails with that error.
- `validate` and `edit` load the defaults-under-file layer only (`Options.loadFile`, `cfg.Load` in `edit.go`); only
  `show`, via the hook's `Options.Load`, adds the environment layer.
