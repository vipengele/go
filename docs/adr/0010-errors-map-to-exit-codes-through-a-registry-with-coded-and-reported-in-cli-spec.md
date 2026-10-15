# Errors map to exit codes through a per-App registry, with Coded and Reported in `cli/spec`

Status: amends ADR-0005.

A tool's scripts and callers read its exit code, and a tool has errors worth telling apart: not
found, conflict, nothing to update. ADR-0005 left the code to `cli/core` with two values, 1 for a
run error and 2 for a usage error. A run function can ask for a code, or the tool's `main` can map its
errors to codes, and `cli/core` chooses one.

## The codes

- 0 is success and `--help`, 2 is a command line the tree does not accept, and 130, the
  conventional 128 plus SIGINT, is a run that was interrupted. No error can be given them.
- 1 is the general error. It is also registrable, so a tool can say that an error it classifies is
  a plain failure and not whatever a later rule would pick.
- 3 through 125 are the tool's own. 125 is the top because a shell reports 126 and above for its own
  failures (126 not executable, 127 not found, 128 and up signals), and a code above 255 does not
  survive the exit status, which keeps only its low eight bits.

## Precedence

The exit code of an error is, first match wins:

1. 130, with nothing written, when the root context is cancelled and the error is
   `context.Canceled`. A run function's own timeout or cancelled derived context is not an
   interrupt.
2. The code of a `spec.CodedError` the error carries, when it is 1 or 3 through 125. Any other code
   is ignored.
3. The first registered `WithExitCode(code, sentinel)` or `WithExitCodeAs(fn)` that matches, in
   option order, also within a joined error.
4. 2 for a usage error.
5. 1.

An error that says its own code outranks a tool-wide mapping, and a mapping outranks the default.
`spec.Reported` suppresses the `<name>: <message>` line and nothing else: the code is still chosen
by rules 2 to 5, so a reported usage error exits 2 silently. The `invalid command tree` message is
never suppressed, because no run function has shown anything.

## Where the types live

`spec.Coded` and `spec.Reported` are in `cli/spec`, with `CodedError` and `ReportedError` for
`errors.As`. `update` and `daemon` require `cli/spec` and may not require `cli/core` (ADR-0005), so
an error they return that asks for a code or marks itself shown can only be built from `spec`. Both
are plain wrappers that unwrap to the error, perform no I/O and import no framework, which keeps the
root package within what ADR-0009 allows it.

## The registry

`WithExitCode` and `WithExitCodeAs` are options of the `App`. The registry is a per-App ordered
list that nothing changes after `New`, so concurrent executions share it. A code that cannot be
registered makes `New` panic, naming the code, because it is a programming error found at start-up.
`WithExitCodeAs` learns the code only when an error matches, so a code it returns that is not
registrable is no match. The package-level `Execute` and `ExecuteArgs` take no options and have no
registry; they honour `spec.Coded`, `spec.Reported` and the reserved codes.

## Hooks

A pre-run hook's error is mapped as a run function's error is. A post-run hook's error after a
successful run is opaque: it exits 1 and is printed, and what it wraps has no effect, because the
run has succeeded and a hook is the tool's cross-cutting concern, not a command saying how it
failed. When the run fails, its error decides.

## Considered options

- **Error types in `cli/core` only.** A `core.ExitError` would make every command that wants a code
  import `cli/core`, and with it `update`, `daemon` and every sibling project, against ADR-0005 and
  ADR-0008. Types in `cli/spec` keep a command independent of the shell that runs it.
- **A code on the command, not the error.** One command fails in several ways, and the code
  belongs to the failure.
- **A package-level registry.** Shared mutable state that `gochecknoglobals` forbids, and two Apps
  in one process, as in tests, would map one another's errors.
- **Registering codes above 125 or the reserved ones.** 126 and above collide with the shell's own
  statuses, and 2 and 130 would stop meaning a usage error and an interruption.
