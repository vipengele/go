# The completion installer is `cli/spec/completioninstall`, run by an `update` post-update hook

Status: accepted. It amends ADR-0003, whose assignment of install-script templates to `update` it
departs from for the completion snippet (see "Template"), and follows ADR-0020, which leaves the
installation of a script open. It keeps cobra internal as ADR-0005 decides and takes the
seam-injection shape of `cli/spec/paths` (ADR-0009).

A tool built with `core.WithCompletion()` (ADR-0020) writes its completion script for the user's
shell into a per-user location, and `update` runs that installer after it has replaced the binary,
so the script of the new version replaces the script of the old one. The package
`go.vipengele.org/cli/spec/completioninstall` holds a pure table of target paths and an `Install`
function that writes the script to the table's path. The `update` project holds the hook that calls
it, and imports nothing of it.

## Decisions

- **Placement.** `completioninstall` is a public subpackage of `cli/spec`: a path table, a renderer
  of the install snippet and `Install`, with every side effect behind an injected seam. It holds no
  cobra type. `Install` runs `<exe> completion <shell>` and writes what the process prints; a
  non-zero exit, which is what a tool built without `core.WithCompletion()` answers, is an error,
  and the package documentation says completion must be enabled. The `cli/spec` root forbids `os`
  imports (`doc_test.go`); its subpackages may do I/O. `cli/core`'s `internal/atomicfile` cannot be
  imported from `cli/spec`, so the package carries its own atomic writer.
- **Template.** The package exports a renderer that produces a POSIX-sh snippet from the same path
  table `Install` uses. A test runs the snippet under `sh` against real temporary directories, with
  `HOMEBREW_PREFIX` set so that `brew` is never executed, and asserts that the path it selects equals
  the Go `Target` for every shell. The full `install.sh` is out of scope; a tool embeds the snippet
  in its own. This departs from ADR-0003, which assigns install-script templates to `update`: the
  table and the snippet are one fact in two languages, and the equality test can only hold them
  together if both live in one package.
- **Opt-out.** `<PREFIX>_NO_COMPLETION`, named by `Identity.EnvVar("NO_COMPLETION")`, turns the
  installation off. Only the value `1` disables it; an empty value counts as unset, the
  `paths.Options.LookupEnv` convention. `Options.Disabled` carries the consumer's own flag, and the
  package exports the flag-name constant `no-completion`, which the consumer registers on its own
  `update` command. A disabled `Install` returns `Result{Skipped: true, Reason: ReasonDisabled}` and
  a nil error. `Options` takes the tool's `spec.Identity`: its `Name` names the completion file and
  its `EnvVar` names the opt-out variable, so no prefix is spelled out in the package. `Disabled`
  returns before the identity is validated.
- **Files only.** The installer writes one completion file and never edits an rc file or a profile.
  bash, zsh and fish skip on `GOOS=windows`. PowerShell writes
  `${XDG_CONFIG_HOME:-~/.config}/powershell/Completions/<name>.ps1` on Unix and
  `Documents/PowerShell/Completions/<name>.ps1` on Windows; its `Note` tells the user to dot-source
  the file from `$PROFILE`.
- **Paths.** The table and the snippet honour `HOMEBREW_PREFIX`, `XDG_DATA_HOME` and
  `XDG_CONFIG_HOME` identically.

  | Shell | Target |
  |-------|--------|
  | zsh | `$HOMEBREW_PREFIX/share/zsh/site-functions/_name` (the prefix is `brew --prefix` when the variable is unset, through the `BrewPrefix` seam) when that directory exists and is writable, else `~/.zsh/completions/_name` with a `Note` on adding the directory to `fpath` |
  | bash | `${XDG_DATA_HOME:-~/.local/share}/bash-completion/completions/name` |
  | fish | `${XDG_CONFIG_HOME:-~/.config}/fish/completions/name.fish` |
  | powershell | as above |

  Writable is decided in Go by creating a temporary file in the directory and removing it again
  (deferred), and in sh by `[ -d ]` and `[ -w ]`.
- **Result.** `Result{Shell, Path, Note, Skipped, Reason, Changed}`. `Reason` is an enumerated
  constant set: disabled, shell not detected, unsupported shell, no home, unsupported on this OS. A
  skip returns a nil error. An unset or empty `$SHELL` is "shell not detected", and a `$SHELL`
  naming any other program is "unsupported shell". An error is returned only when `Options.Identity`
  fails `Validate`, and for failing to locate the executable, to generate the script, or to write
  the file.
- **Writes.** A write is atomic: `MkdirAll` at 0755, a temporary file in the target directory, a
  rename onto the target, mode 0644. The temporary file is removed on every failure path. When the
  existing content is identical, nothing is written and `Changed` is false; an existing target that
  cannot be read is written anyway.
- **Seams.** `GOOS`, `LookupEnv` and `UserHomeDir` as in `cli/spec/paths`, plus `BrewPrefix`, `Run`,
  `WriteFile` and `Executable`. A zero `Options` uses the real process.
- **Post-update hook.** `update` defines `PostUpdate`, a function type that receives the path of the
  installed binary, and the field `Config.PostUpdate`. `RunPostUpdate` runs the hooks in order, joins
  their errors and runs the later hooks after one fails. A hook failure is non-fatal by contract: the
  update has been applied and is reported as such. The hook receives the path because it runs in
  the old process after the swap, where `os.Executable()` names the wrong file. `update` does not
  import `completioninstall` in non-test code; its `example_test.go` wires the two together and is
  the documented composition.

## Considered options

- **The installer in `cli/core`.** It would make every consumer of the installer, including a tool
  that only runs it from an install script, depend on the cobra-backed core. Running the tool's own
  `completion <shell>` keeps cobra on the other side of a process boundary.
- **A template owned by `update`**, as ADR-0003 assigns. It leaves two hand-maintained copies of the
  path table, one in Go and one in sh, that drift apart without a test able to see both.
- **Editing rc files and profiles.** It is invasive, hard to undo, and cannot be made idempotent
  without parsing a file the user owns. A file in a directory the shell already reads, plus a `Note`
  where it does not, needs none of that.
- **A single post-update callback.** It has no ordering and no composition: two tools of one process
  cannot both react. A list run by `RunPostUpdate` composes.
- **`update` owning the installer.** The installer shells out to the tool's own `completion <shell>`
  and is meaningful without self-update: an install script, or a tool that does not self-update,
  can use it. Kept in `cli/spec`, the path table and the sh snippet live beside the other
  spec-level completion helpers, and `update` exposes only a hook and holds no shell or path
  knowledge.

## Consequences

- **A tool without `core.WithCompletion()` fails the install.** `Install` returns the error of the
  non-zero exit; the post-update hook reports it without failing the update.
- **Windows is table-tested only.** The package has no `*_windows.go` file, so CI's windows vet does
  not run for it. The Windows paths are covered by tests of the table with an injected `GOOS`, and
  the atomic writer is unverified on Windows.
- **The path table has one more reader.** A change to a path changes `Install`, the snippet and the
  equality test together, and a tool that embeds an older snippet installs to the older path.
- **No rc file is touched.** zsh's fallback directory and PowerShell's profile need a step by the
  user, which the `Note` states.
