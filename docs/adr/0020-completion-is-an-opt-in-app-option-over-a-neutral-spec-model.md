# Completion is an opt-in App option over a neutral spec model

Status: accepted. It follows the shape of `0017-the-config-command-group-is-an-opt-in-app-option.md`
and keeps cobra internal as `0005-commands-are-a-framework-neutral-model-in-cli-spec-and-cobra-is-an-internal-adapter.md`
decides; it amends neither.

A tool built on `cli.App` passes `cli.WithCompletion()` and gets a `completion` command that writes
the completion script of bash, zsh, fish or powershell to standard output (`completion <shell>`).
A command declares how its flag values and positional arguments complete with helpers of `cli/spec`,
and the script asks the tool for them at the time the user presses tab. Without the option the App
has no `completion` command and root `--help` does not list it; the helpers still answer.

## Decisions

- **Opt-in option.** `WithCompletion()` is an App option like `WithConfig` (ADR-0017). It sets a
  field of the adapter's `Config`, and the adapter re-enables cobra's own `completion` command
  (`DisableDefaultCmd` is false only then). The hidden help stub stays hidden. A tool that adds a
  command named `completion` of its own keeps it.
- **Neutral model.** `cli/spec` holds a `Completion` value built by `CompleteChoices(values...)`,
  `CompleteFiles(exts...)`, `CompleteDirs()` and `CompleteFunc(fn)`; the zero value is
  `NoCompletion`. A flag takes one through the `Complete(c)` `FlagOption` and the positional
  arguments of a command through `Command.ArgCompletion`. A `Completion` is immutable and reports
  its `Kind()` with the accessors `Choices()`, `Extensions()` and `Func()`; the adapter maps each
  kind to cobra. No cobra type, and none of cobra's directive vocabulary, appears in `spec`
  (ADR-0005, enforced by depguard).
- **Kinds.** Choices propose exactly the listed values that start with the word and no file names.
  Files propose file names, limited to the given extensions when there are any, written with or
  without the leading dot. Dirs propose directories only. Func proposes what the function returns
  and no file names. An `ArgCompletion` stops proposing once the command's `Args` accepts no more
  positional arguments.
- **`CompletionFunc` shape.** `func(ctx context.Context, in CompletionInput) ([]string, error)`,
  where `CompletionInput` carries the partial word (`ToComplete`) and the positional arguments
  before it (`Args`). It returns values and nothing else: whether file names are also offered is
  the kind's decision, not the function's. A non-nil error offers nothing, and still no file names;
  the error is not shown, since a completing shell has nowhere to show it.
- **Cobra's command, not a `spec.Command`.** The `completion` command is cobra's, not one built from
  a `*spec.Command` as the `config` group is. Cobra runs it outside `runHooked`, so a `config.yaml`
  that does not load, the update check and every other hook cannot stop a script from being
  written, and it needs neither `spec.SkipConfig` nor `spec.SkipUpdateCheck`. A spec command would
  run inside `runHooked` and carry both annotations to avoid that.
- **The opt-in gates the script only.** `WithCompletion()` governs the script-generating command.
  Cobra's hidden `__complete`, which a script runs, is live in every App and runs no hook, so
  nothing a hook writes can corrupt what a shell reads, and completion functions work in an App
  that never generated a script. The package-level `Execute` and `ExecuteArgs` entry points never
  get `completion`.
- **A `CompleteFunc` closes over what it needs.** The adapter rebuilds the cobra tree on every run
  and `__complete` runs no hook, so the function's context carries no configuration, logger or
  `Invocation`. A function that needs the loaded configuration or a client closes over it when the
  tool builds the command. The function writes nothing to the standard streams, which carry the
  completion protocol.
- **Validation.** `Command.Validate` rejects an `ArgCompletion` on a command whose `Args` accepts
  no argument (`NoArgs`), a `Complete(...)` on a bool flag, which takes no value to complete, and a
  `CompleteFunc` with a nil function on either. `speccheck` is where a tool tests its tree.
- **Shell-load test.** `cli/core` has one test, a subtest per shell, that generates each script and
  checks it is not empty. It loads the script in the real shell and asserts the shell registered
  completion for the command. Bash must be present and runs always, so a runner without it fails.
  zsh, fish and pwsh run when `exec.LookPath` finds them and are skipped with a message otherwise.
  Each shell starts with a scrubbed environment and empty home and config directories, so neither
  the runner's rc files nor its installed completions take part.
- **No CI change.** The test installs nothing and no `ci-*.yml` is edited: a change to a shared CI
  file selects every project (CLAUDE.md, "CI"), so installing a shell there would build and test
  all of them for one project's test.

## Considered options

- **Default-on `completion`, hidden or visible.** It reverses the deliberate disabling of cobra's
  command and widens the command surface of every tool built on the library, including those that
  never ship a script.
- **A function-only hook.** A single hook returning values and a directive copies cobra's directive
  vocabulary into `spec` and makes every tool state, for a flag that takes three fixed values, what
  cobra would otherwise derive. Choices, files and dirs are data an adapter can map; the function
  covers the values that cannot be listed.
- **An enum flag kind.** A flag type that carries its allowed values would complete itself, but it
  changes how the flag parses and reports bad values, a change to the flag model beyond completion.
  `CompleteChoices` completes a string flag and leaves the parsing to the command.
- **A `spec.Command` for `completion`.** It would run inside `runHooked` and reach the problems the
  decision above avoids.
- **Installing zsh and fish in CI to make every shell mandatory.** It needs an edit to a shared CI
  file and so selects every project. Only bash, present on every runner, must run.
- **Gating `__complete` with the option.** Completion functions would then work only in an App that
  opted in, while a script generated by another build of the tool, or written by hand, would hit an unknown
  command. A request that a script makes is not a command a user runs.

## Consequences

- **The tree has one more reserved name.** Under `WithCompletion` the root holds cobra's
  `completion` unless the tool defines its own, so a tool's own `completion` command replaces the
  generated one.
- **`__complete` is part of every tool's behaviour.** It is hidden and takes no option, so an App
  answers completion requests whether or not it can print a script.
- **No installation.** `completion <shell>` writes the script and `completion <shell> --help`
  describes how to load it. An installer that places the script, and an environment variable that
  turns the command off, are not covered here.
