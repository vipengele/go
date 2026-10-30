# cli/spec

`go.vipengele.org/cli/spec`: the framework-neutral model of a command-line tool (`Identity`, `Command`,
`Flag`, `Invocation`, `Env`) that `cli/core`, `update` and `daemon` share. Read `doc.go` first, then
`docs/adr/0005-commands-are-a-framework-neutral-model-in-cli-spec-and-cobra-is-an-internal-adapter.md`,
`docs/adr/0009-platform-paths-live-in-cli-spec-paths.md`,
`docs/adr/0010-version-comparison-lives-in-cli-spec-version.md` and
`docs/adr/0015-table-helper-lives-in-cli-spec-table.md` and
`docs/adr/0020-completion-is-an-opt-in-app-option-over-a-neutral-spec-model.md` and
`docs/adr/0021-the-completion-installer-is-cli-spec-completioninstall-with-an-update-post-hook.md` at
the repo root.

## Commands (run from `source/cli/spec`)

```bash
go build ./...
go vet ./...
go test -race -shuffle=on ./...
golangci-lint run
```

## Structure

- Root package `spec` — performs no I/O: it imports no `os` and touches neither the environment nor the
  file system. Anything that does belongs in a subpackage.
- `Coded(code, err)` / `Reported(err)` (`exit.go`) — the exit code and the already-shown mark a run
  function returns; they live here so `update` and `daemon` need not import `cli/core`, which maps
  them to an exit code (ADR-0010).
- `UsageError{Question, Flag}` (`usage.go`) — the error a run function returns when a question needs
  an answer nobody can type; `cli/core` exits 2 for it, found by `errors.As` (ADR-0015). It is
  returned by pointer, wraps nothing, and `Coded(2, err)` stays ignored: a run function claims exit 2
  only through this type. Bare it renders as one line, `<Question> requires interactive input; pass
  <Flag>` (without the `; pass` clause when `Flag` is empty); wrapped it renders as a two-row tree.
- `Invocation.Confirm(question, ConfirmOptions)` and `Invocation.Prompt(question, PromptOptions)`
  (`prompt.go`) — ask on `Env.Stderr` and read one line from `Env.Stdin`, byte by byte so nothing
  after the newline is consumed. They ask only when `Env.IsTerminal(Stdin)`; quiet and the JSON output mode
  do not change that, and nothing is written to stdout. Not a terminal: a question with a default and
  not `Required` answers with the default and writes nothing; any other returns a bare
  `*UsageError` naming `Flag`. `ConfirmDefault` is `NoDefault` (hint `[y/n]`, the zero value),
  `DefaultYes` (`[Y/n]`) or `DefaultNo` (`[y/N]`); `Confirm` accepts y, yes, n and no in any case.
  End of input before any byte returns `ErrAborted` (exit 1, printed as `<tool>: aborted`; 130 stays
  signal-only), after some bytes it takes them as the answer. A refused answer is an error, never
  asked twice. The Invocation has no run context, so an interrupt is not seen until Enter: a run
  function checks its context after the answer. A nil `Env` never asks.
- `ColorAllowed(noColor, identity, env, stream)` (`colour.go`) — the colour decision for one stream
  and its precedence; `Output.Styler(stream)`, `Styler` and the `Glyph*` constants (`style.go`) —
  coloured text and role glyphs that follow `Output.Color(stream)`.
- `FlagOf.Get(inv)` / `FlagOf.IsSet(inv)` (`flag.go`) — a flag's value, and whether the command
  line sets it explicitly, so `--json=false` is set and a `--json` not given is not. The adapter
  names the set flags with the variadic `NewInvocation(env, args, values, ExplicitFlags(names...))`
  (`invocation.go`); without the option nothing reads as set. `IsSet` is false for a value of
  another type under the name, as `Get` then returns the default.
- `Completion` (`completion.go`) — how a shell completes a flag value (`spec.Complete(c)` as a
  `FlagOption`, read back by `Flag.Completion()`) or a command's positional arguments
  (`Command.ArgCompletion`). Built by `CompleteChoices`, `CompleteFiles(exts...)`, `CompleteDirs()` or
  `CompleteFunc(fn)`; the zero value is `NoCompletion`, and an adapter switches on `Kind()` and reads
  `Choices()`, `Extensions()` or `Func()`. `Validate` rejects a completion on a bool flag, a
  `CompleteFunc` with a nil function, and an `ArgCompletion` on a command that accepts no arguments
  or has no `Run`. A `CompletionFunc` runs in the shell's own completion process, outside any command
  run: no `Invocation`, configuration, logger or hook, so it closes over what it reads (ADR-0020).
- `WriteJSON(w, schema, v)` (`json.go`) — the JSON envelope: one indented object, `schema` first,
  then the keys of `v`, which must marshal to an object with no `schema` key (ADR-0014). `Output.Format()` is the
  resolved `Format` (`FormatText` or `FormatJSON`). `speccheck.AssertJSONKeys(t, v, golden)`
  pins the key paths of a payload to a golden file; `SPECCHECK_UPDATE=1` rewrites it.
- `Invocation.Info`, `Success`, `Warning`, `Error`, `Detail` and `Data` (`message.go`) — printf-style
  output helpers. The first five write one styled line to `Env.Stderr`; `Detail` is dim and only
  written when verbose; quiet drops all but `Error`; `--json` changes none of them. `Data` writes its
  text to `Env.Stdout` exactly, is never dropped, and returns the write error. A dynamic string is
  passed as `inv.Info("%s", msg)`. A nil `Env` discards. See
  `docs/adr/0013-stdout-carries-data-stderr-carries-messages.md`.
- `paths/` — `paths.Resolve(spec.Identity, paths.Options, kinds...)` resolves the requested config,
  state, cache, data and log directories per OS layout (XDG, darwin, windows), honouring
  `<PREFIX>_HOME`, `Options.Home` and explicit per-kind options, and `paths.New` resolves all five.
  A kind not requested stays "" and nothing is looked up for it, so a caller that needs only state
  does not fail on a home directory only the other kinds need; a requested kind still resolves the
  kind it derives from (log from state on XDG, state and data from config on darwin). Resolving
  creates and stats nothing; `Dirs.Ensure` creates directories 0700 on demand and reports a kind
  that was not resolved. `InContainer` is a heuristic over injectable `Probes` and changes no path.
- `terminal/` — `terminal.Detect(stdin, stdout, stderr)` returns the `func(spec.Stream) bool` for
  `Env.Terminal`; `DetectWith(Probes, ...)` takes the probes as a seam. Each stream is probed lazily
  and at most once, and anything that is not an `*os.File` is not a terminal. It is the only importer
  of `golang.org/x/term`.
- `version/` — `version.IsRelease(v)` and `version.Newer(current, latest)`, pure string-to-bool
  functions on `golang.org/x/mod/semver`: the one definition of a release version and of "newer"
  (ADR-0010). A non-release current version is a dev build, older than every release; invalid input
  means no update. It performs no I/O.
- `table/` — `table.New(cols...)`, `Add(values...)`, `Render(w, st)` and `Rows()`: aligned text and
  the same rows as `[]Row` for JSON, which marshal with the columns' keys in column order. The
  command owns the JSON envelope and the key the rows sit under (ADR-0014). A style sees the unpadded
  cell and padding follows the measured width, so colour never alters alignment; width is the rune
  count. `New` with no columns and `Add` with the wrong number of values panic. See
  `docs/adr/0015-table-helper-lives-in-cli-spec-table.md`.
- `completioninstall/` — `Install(Options)` writes the script `<exe> completion <shell>` prints to the
  per-shell file `Target(shell, name, Env)` chooses (zsh, bash, fish, powershell), and `Snippet(name)`
  renders the same table as POSIX sh; a test holds the two equal. Files only: no rc file or profile
  is edited, a `Note` says what the user adds. Needs `core.WithCompletion()` in the tool, else
  `Install` errors. Opt-out is `<PREFIX>_NO_COMPLETION=1` or `Options.Disabled`
  (`NoCompletionFlag`); every side effect is an `Options` seam. See
  `docs/adr/0021-the-completion-installer-is-cli-spec-completioninstall-with-an-update-post-hook.md`.

## Conventions

- Environment and home directory reach `paths` only through `Options` seams (`LookupEnv`,
  `UserHomeDir`, `GOOS`), never `os.Getenv` directly in the resolving code, so tests substitute them.
  The layout is selected by `Options.GOOS`, not by build tags.
- No package-level mutable state: `gochecknoglobals` is enabled.
- `depguard` forbids `github.com/spf13/cobra` here; the cobra adapter lives in `cli/core/internal/cobra`.
  It also forbids `golang.org/x/term` outside `cli/spec/terminal`.
- `Env.IsTerminal` with a nil `Env.Terminal` answers false for a non-file stream and panics for a
  stream with an `Fd() uintptr` method: build the `Env` with `cli/core` or set `Env.Terminal`.
- `Env.Interrupts` is a plain `<-chan struct{}`: the first value cancels the run's context, the second
  abandons the run with exit code 130, and nil watches nothing. `spec` registers no signal handler;
  `cli/core` does and feeds the channel (ADR-0012). `Resolved` copies it unchanged.
- `commons` is a sibling reached by a directory `replace` in `go.mod` (`../../commons`); see the
  repo-root rule on sibling replaces.
