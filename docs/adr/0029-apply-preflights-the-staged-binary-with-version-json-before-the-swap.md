# Apply preflights the staged binary with `version --json` before the swap

Status: accepted. It extends
`docs/adr/0022-install-replaces-the-executable-with-two-renames-and-keeps-a-backup.md`, whose
install step it now precedes, and relies on the `version --json` contract of
`docs/adr/0007-the-cli-project-owns-the-version-json-contract.md` and on the app shipping `version`
of `docs/adr/0009-the-app-owns-version-and-takes-hooks-as-options.md`.

`Config.Apply` verifies the archive against the release's checksums and signature, but a verified
archive can still hold a binary that is the wrong release, the wrong platform, or broken. Installing
it replaces a working executable with one that cannot run. `Apply` therefore runs
`<staged> version --json` on the extracted binary before the two renames of ADR-0022 and refuses a
binary that does not answer as the selected release for this platform. A refusal leaves the
installed executable untouched, removes the staged file and does not advance `HighestApplied`
(issue #63).

## The rules

- **Always on.** The check runs inside `Apply` and has no opt-out field. ADR-0009 makes `version`
  part of every app, so a binary that cannot answer it is not an app this updater can install.
- **App veto hooks.** `Config.Preflight []Preflight`, with
  `type Preflight func(ctx context.Context, b StagedBinary) error` and
  `StagedBinary{Path, Version, Schema, OS, Arch}` as decoded. Hooks run in order, only after the
  built-in check passes; the first error vetoes and later hooks do not run. A hook is
  side-effect-free or cleans up after itself, because a later hook or the install step may still
  refuse.
- **Schema.** The output is accepted when `schema` is 1 or greater and `version` decodes as a
  non-empty string; a missing or zero `schema` is refused. It is forward compatible on purpose: an
  updater must be able to install a binary that emits a newer schema, and an app-level rule (a
  database schema, say) belongs in a hook. The output is decoded into a private struct, so `update`
  does not import `cli/core` (ADR-0007, ADR-0008; the depguard rule in `source/update/.golangci.yml`
  denies `go.vipengele.org/cli/core`).
- **Identity.** The reported `version`, with a leading `v` added when it is missing, equals the
  selected tag exactly, as a string. Tags are canonical and `cli/spec/version` has no equality
  function. The reported `os` and `arch` equal the resolved `GOOS` and `GOARCH`.
- **Execution.**
  - `Config.PreflightTimeout`: zero is a 10 second default, a negative value is refused by
    `Validate`, and there is no upper cap. It follows `DownloadTimeout` through `checkNotNegative`,
    not the capped `CheckWait`.
  - The arguments are `version --json`. The environment is inherited (`exec.Cmd.Env` nil) and stdin
    is nil, which is the null device; `os.Stdin` is never wired.
  - Stdout is read up to 64 KiB; more is a refusal. Stderr is cut to `maxErrorBody` on a UTF-8
    boundary, as `newStatusError` does.
  - The exec goes through an unexported field of `fsSteps`. That is the third exception to the
    convention that a new seam is a `Config` field.
- **Errors.** `*PreflightError` has the `update: ` prefix, the exported documented fields `Path`,
  `Reason` and `Stderr`, and an `Unwrap` to the cause: the exec error, `context.DeadlineExceeded`
  when `PreflightTimeout` expires, or the hook's error. When the parent context is done its error
  passes through unwrapped, as it does from `Download` and `Extract`.
- **The staged file is closed before exec.** `Extract` returns the `*Staged` with its write
  descriptor open, and `execve` of a file open for writing fails with `ETXTBSY` on Linux; Windows
  refuses too. `Keep` would close it but settles the `*Staged`, after which `Discard` is a no-op and
  a refusal would leave the file behind. `Staged` has an unexported close that does not settle:
  `Discard` still removes the file and `Keep` in `Install` works on an already-closed file.
- **The staged name carries the binary's extension.** The temporary file is created with the
  extension of `binaryFileName` for every format, raw included, so Windows can run it: `CreateProcess`
  appends `.exe` to an extensionless path. `checkStagedDir` compares directories and the backup is
  `<target>.old`, so nothing else depends on the name.
- **A new step.** `(Config).CheckStaged(ctx, *Staged, Selection) (StagedBinary, error)` is exported
  and runs the built-in check and the hooks, for a caller that composes the steps by hand, as
  `CheckManaged`, `CheckWritable` and `CheckDowngrade` are. `Apply` calls it between `Extract` and
  `Install`, which makes nine steps. Nothing at the executable's path changes before `Install`.
- **`updatetest`.** The binary of `updatetest.NewRelease` is a `#!/bin/sh` script that prints
  schema-1 version JSON for its tag and `runtime.GOOS` and `runtime.GOARCH` on `version --json`, and
  echoes the tag otherwise. The JSON is hard-coded, because `updatetest` cannot import `update`.
  It is unix-only. The tests of `updatecmd` live in an external package and cannot stub the exec
  seam, so without it every install test there would be refused.

## Considered options

- **An opt-out field.** A consumer that switches the check off ships binaries the updater cannot
  vouch for. Every app ships `version` (ADR-0009), so there is no app the check cannot run against,
  and an opt-out would only be a way to install a binary the updater has not seen answer as the
  selected release.
- **Exact schema match.** A binary built from a newer release emits a newer schema, and an old
  updater would refuse to install it, which is the update that most needs to succeed.
- **Version-only compare.** A tag-correct binary built for another OS or architecture passes, and
  fails only after the swap.
- **Hooks as `ApplyOption`.** The hooks are configuration of the update, like `PostUpdate`, which
  lives on `Config`, and an option is per call. A hook passed only to `Apply` would also be
  unavailable to `CheckStaged`.
- **A scrubbed environment.** The child is the binary the user is about to run with this very
  environment, and a scrubbed one reports a different answer for any app that reads it (a home
  directory, a proxy). Stdin is the null device rather than the updater's stdin, which is the input that could block it.
- **A `Config` field for the exec seam.** No consumer swaps how a process is started; the field
  would be public API for a test.

## Consequences

- **A refused binary never reaches the executable's path.** The target is byte-identical, no rename
  is attempted and the staged file is gone.
- **The staged binary runs once before it is installed.** It runs with the updater's privileges and
  environment, so a hook is the place for any further rule, not a place to start work that needs
  undoing.
- **Windows may fail transiently.** A scanner can hold the `.exe` briefly after the child has
  exited, so the removal on the refusal path, or the rename of `Install`, can fail until it lets go.
  Execution on Windows is not tested here; it is left to issue #60.
- **`updatetest` releases need a shell.** A test of a consumer that runs on Windows has to supply
  its own binary.
