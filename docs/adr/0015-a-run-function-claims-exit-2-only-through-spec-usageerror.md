# A run function claims exit 2 only through `spec.UsageError`, and an aborted prompt exits 1

Status: amends ADR-0010 (`0010-errors-map-to-exit-codes-through-a-registry-with-coded-and-reported-in-cli-spec.md`).

A command that asks a question cannot proceed when nothing can answer it: the standard input is
not a terminal, and the answer is neither a default nor a flag. That is a command line the tool
does not accept, the case ADR-0010 gives exit 2, but ADR-0010 lets only the framework's own
argument parsing reach that code. A run function that finds the problem has no way to say so.

## The rule

- `spec.UsageError{Question, Flag}` is the error a run function returns for it. `cli/core` exits 2
  for it wherever `errors.As` finds a `*spec.UsageError` in the chain, in the place ADR-0010 gives
  rule 4, beside the framework's own usage error, so rules 2 and 3 outrank it as they outrank that
  one. `Invocation.Confirm` and `Invocation.Prompt` return it when they must ask and cannot.
- `spec.Coded(2, err)` stays ignored and `WithExitCode(2, ...)` still panics in `New`. Exit 2 is
  claimed by the type of the error, not by a number a command or a tool chooses.
- `UsageError` wraps nothing. Returned bare it renders as one line; wrapped by a command's own
  message it renders as a two-row tree, the outer layer over the usage line.
- `spec.ErrAborted` is what `Confirm` and `Prompt` return when the standard input ends before an
  answer, as when a person presses Ctrl-D. It exits 1, the general error, and the App's renderer
  prints `<tool>: aborted`. 130 stays the code of an interrupt alone: rule 1 requires the root
  context to be cancelled, and an end of input cancels nothing.

## Considered options

- **A registrable code 2.** Letting a command or `WithExitCode` give 2 would end the guarantee that
  2 means the command line was not accepted: any error a tool classifies could exit 2, and a script
  could no longer read 2 as "change the arguments". ADR-0010 reserved it for that reason.
- **A sentinel per command.** Each command would export an error for `WithExitCode(2, ...)` to
  match, and a tool would register every one. The registry cannot take 2, and a sentinel carries
  neither the question nor the flag that answers it, which the message needs.
- **A hint field on `Coded`.** `Coded(2, err)` with a flag to say the code is deliberate would make
  the reserved code reachable by a boolean. It would split one meaning across two spellings, and
  `Coded` would carry a usage message it has no fields for.
- **Exit 130 for an end of input.** Ctrl-D is not SIGINT, and a script that reads 130 as an
  interruption would take an aborted prompt for one. The run was not interrupted; the person
  declined to answer, which is a failure of the run.
