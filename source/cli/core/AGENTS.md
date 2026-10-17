# cli/core

Module `go.vipengele.org/cli/core`, tag `source/cli/core/vX.Y.Z`. The base of the vipengele CLI
library. The `version --json` output is a contract owned by the `cli` project (ADR-0007).

## Packages

- `cli` (the root package) — `New(identity, opts...)` builds an `App`; `Add(cmd)` adds a command
  tree; `Execute()` / `ExecuteArgs(env, args)` run it, each execution building a fresh root named by
  `identity.Name`. Options: `WithVerbose`, `WithQuiet`, `WithNoColor`, `WithJSON`, `WithOutput` opt
  into the standard flags, declared as inherited flags on the root (`flags.go`); `WithPreRun` / `WithPostRun`
  add `PreRunHook` / `PostRunHook`s (`hooks.go`), which receive the `Invocation` and a
  `spec.CommandInfo` (path and `Annotations`); `WithBuildInfo` fixes what `version` prints (tests),
  otherwise `buildinfo.Read()`. A run function reads the standard flags and colour (per stream,
  `Output.Color(stream)`) from `inv.Output` (`spec.Output`, resolved in `output.go`), never from a
  handle. `Output.Format()` is resolved by `outputResolver.format`: `WithJSON`'s `--json` or
  `WithOutput`'s `--output text|json`, never both (`New` panics, whatever the option order). Under
  `--output`, not set is text (told from an explicit `text` by `FlagOf.IsSet`, the adapter's
  `pflag` `Changed`), and the running command's own `--json` must agree: `--output text` with a
  true `--json`, `--output json` with an explicit `--json=false`, and an unknown value are usage
  errors (exit 2). A command with a local `--json` reads it OR'd with `Output.Format()`. The App
  owns `version`
  (ADR-0009): every root holds `version.New(...)` after the added commands, with a local `--json`
  unless `WithJSON` is given, then `version.WithOutputJSON()`, so `version --json` writes JSON
  under every option set, and `version --output json` too under `WithOutput`; a tool's own `version` is a
  duplicate name. `cli/core` never imports `update`: the tool's `main` wires the update check as a
  hook, and `spec.SkipUpdateCheck` is the opt-out annotation.
  The package-level `Execute(root)` runs a `*spec.Command` tree for a tool's `main` against
  the process (`os.Args[1:]`, standard streams, working directory, environment) and returns the exit
  code; `ExecuteArgs(env, root, args)` is the same against a given `*spec.Env`, for tests. Both
  validate the tree on every call. Exit codes: 0 success and `--help`, 1 a run error or an invalid
  tree, 2 a command line the tree does not accept, 130 interrupted (the run's context, derived from
  the `executeArgsCtx` context, done and `context.Canceled`, nothing printed; also a second
  interrupt); 3-125 are registrable. Order, first match wins: 130,
  `spec.Coded` (1 or 3-125), `WithExitCode(code, sentinel)` / `WithExitCodeAs(fn)` in option order,
  usage 2, else 1. The registry is per-`App`, read-only after `New` (a bad code panics there); the
  package-level `Execute` / `ExecuteArgs` have none. Every error is written to stderr as a tree
  (`internal/errrender`, ADR-0011) unless it carries `spec.Reported` (the code is still chosen as
  above; the `invalid command tree` tree, `fmt.Errorf("invalid command tree: %w", err)` with a row per
  defect, is never suppressed). A post-run hook error after a successful run is an opaque
  `*HookError` (exit 1, printed); a pre-run hook error maps like a run error (ADR-0010).
  `WithErrorRenderer[E error](fn func(E) string)` renders an error of type `E` itself: each node of
  the tree is tried by a direct type assertion to `E` (not `errors.As`), the first registered match
  wins and its output replaces the node's whole subtree (first line the row, further lines detail
  rows indented beneath it). An empty string falls back to the default for that node and a nil `fn`
  is ignored. The renderers are per-`App`, read-only after `New`; the package-level `Execute` /
  `ExecuteArgs` have none. Three sites write to stderr: the run error and the invalid-tree error
  (both with the App's renderers), and a failed `os.Getwd` in `Execute` before any run (default
  rendering only). Errors are always written on the calling goroutine.
  `spec.Env.Interrupts` drives interruption (`execute.go`): the first value only cancels the run's
  context, which every hook and run function receives, and the error the run then returns is mapped
  by the order above (rule 1 reads the run's context, so a run returning `context.Canceled` exits
  130 silently; any other error keeps its code and is printed). The second returns 130 at once and
  abandons the run, which may still run its PostRun hooks and write to the Env's streams. Nil runs `cobra.Run` inline with
  no goroutine; otherwise one watcher goroutine and one run goroutine, both joined on every path
  but the abandoning one. `ExecuteArgs` registers no OS signal handler; `Execute` and
  `App.Execute` register SIGINT and SIGTERM once, in `executeProcess`, forward them as `Interrupts`
  values and unregister on return. A test injects its own `chan struct{}`.
- `internal/errrender` — `Render(w, tool, err, renderers)` prints the tree. The summary line is
  `<tool>: <outermost layer's own text>` (no prefix for an empty tool); each inner `%w` layer is a
  `→` row and each error of an `errors.Join` a `•` row, indented two spaces per depth; always UTF-8,
  no colour. A layer's own text is its message with `": "+child.Error()` cut off the end. Transparent
  (message equals the child's, e.g. `spec.CodedError`): skipped. Opaque (child's text is not a
  suffix, or a nil child): the whole message is one row and the walk stops. Empty own text: dropped.
  A multi-child node is a join only when its message is the newline-join of the children's;
  otherwise (a `fmt.Errorf` with several `%w`) it is opaque. A bare join has the summary `N errors`
  and a join of one renders as that child. `*cobra.UsageError` is a flat leaf, so a usage error stays
  `tool: greet: unknown flag: --bogus`.
- `internal/cobra` — the only code that imports cobra. `Run(ctx, env, root, args, Config)` builds a
  fresh cobra tree per execution from a validated `*spec.Command`, so one tree may run any number of
  times, concurrently. `Config` carries the per-run `Output` resolver and the `PreRun` / `PostRun`
  hooks as plain funcs; each invocation names the flags `pflag` reports `Changed` with
  `spec.ExplicitFlags`; a post-run error after a successful run is a `*HookError`, never a
  `*UsageError`.
- `buildinfo` — `Read()` returns an `Info` (version, commit, date, Go version, OS, arch). `Info.IsDev`
  delegates to `cli/spec/version`. `Info.String()` is the one-line text form; `Info.MarshalJSON()` is the
  JSON object without `schema`; `command/version` writes it with `spec.WriteJSON(inv.Env.Stdout, 1, info)`,
  which adds `schema` first and indents. The keys are pinned by
  `buildinfo/testdata/version-json.golden` (`speccheck.AssertJSONKeys`, `SPECCHECK_UPDATE=1`
  rewrites it). Adding a key keeps `schema`; renaming, removing or retyping a key increments it.
- `command/version` — `New(info, opts...)` returns the `*spec.Command` for `version`, annotated
  `spec.SkipUpdateCheck`. By default it has a local (not inherited) `--json` flag, OR'd with
  `inv.Output.Format() == spec.FormatJSON`;
  `WithOutputJSON()` declares none and reads `inv.Output.JSON()`, for a root with an inherited
  `--json` (a descendant flag of the same name makes the tree invalid).

## Gotchas

- Link-time values: `buildinfo.version`, `commit` and `date` are set with `-X` and must stay
  uninitialised package-level `string` variables that `Read` references. The linker ignores any
  other `-X` silently. `buildinfo/ldflags_test.go` builds a real binary
  (`buildinfo/testdata/ldflagsmain`) to prove it, so that test needs the `go` command.
- Cobra lives only in `internal/cobra`, imported under the alias `spf`; the depguard rule in
  `.golangci.yml` fails any import of `github.com/spf13/cobra` elsewhere. `cli/spec` is cobra-free.
  The same file confines `golang.org/x/term` to `cli/spec/terminal`.
- Commands are `cli/spec` data. Flags are read through typed handles (`asJSON.Get(inv)`) built inside
  the command's constructor: `gochecknoglobals` is on, so there are no package-level handles, and a
  package-level variable needs a justified `//nolint`.
- Sibling edges: `go.mod` requires `go.vipengele.org/cli/spec` and `go.vipengele.org/commons`
  (indirect, because spec imports it), each with a version-less directory `replace` (`../spec`,
  `../../commons`) while they are developed together (ADR-0006). The main module ignores a
  dependency's `replace`, which is why commons is listed here.
- Error output is pinned by golden files under `testdata/render/*.golden` (`render_golden_test.go`).
  Regenerate with `go test -update`; the `-update` flag is registered in `TestMain`
  (`execute_test.go`), so a test binary without that `TestMain` does not know it.
- A command writes its result with `inv.Data` (stdout) and everything about the run with the
  `inv.Info`/`Success`/`Warning`/`Error`/`Detail` helpers (stderr), so `--json` stdout stays a clean
  document; `message_test.go` pins it (ADR-0013).
- An `ExecuteArgs` test passes a fake `Env` (buffers, a fake `LookupEnv`, a `t.TempDir` `WorkDir`).
  A real environment variable set with `t.Setenv` is invisible to `Run`, which reads only
  `Env.LookupEnv`.
- `ExecuteArgs` fills a nil `Env.Terminal` on its own copy with `terminal.Detect` over the `Env`
  streams; a supplied `Terminal` is kept and the caller's `Env` is not mutated. A real file is
  probed and buffers are not terminals, so a test that needs a terminal sets `Env.Terminal`.
  Colour is decided per stream, stdout and stderr each by `spec.ColorAllowed` (`output.go`); the
  first rule that applies wins: `--no-color` off; `NO_COLOR` or `<EnvPrefix>_NO_COLOR` non-empty
  off; `FORCE_COLOR` non-empty and not `"0"` on; `TERM=dumb` off; otherwise on exactly when
  `env.IsTerminal` of that stream is true. Package-level `Execute` / `ExecuteArgs` have no flag and
  no identity, so they read no prefixed variable.
