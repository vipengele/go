# Signals are registered by `Execute` and injected through `Env`

Status: accepted.

`Execute` and `App.Execute` register for SIGINT and SIGTERM in `executeProcess`, once per process
run, and unregister when it returns. Each signal reaches the run as a value of
`spec.Env.Interrupts`, a `<-chan struct{}`; the framework-neutral model in `cli/spec` never sees an
`os.Signal`. `ExecuteArgs` registers nothing and reads only the channel it is given.

## The rule

- **First value.** It cancels the context every hook and run function receives. The run decides
  how to stop; the framework does not kill it.
- **Second value.** `ExecuteArgs` returns 130 at once and abandons the run, without waiting for it.
  The abandoned run may still run its PostRun hooks and write to the Env's streams. Library code
  never calls `os.Exit`: the return value reaches `main`, and `main`'s `os.Exit` ends the process
  and the abandoned goroutines with it.
- **Exit code after an interrupt.** A run error after the first value exits 130. A usage error is
  still 2 and a nil error is still 0, so a run that finishes cleanly despite the interrupt reports
  success. SIGTERM is mapped to 130 like SIGINT; the framework does not tell the two apart.
- **Nil `Interrupts`.** The run is inline on the calling goroutine, with no watcher and no extra
  goroutine. A non-nil channel costs one watcher goroutine and one run goroutine, both joined on
  every path but the abandoning one.

`executeProcess` forwards signals through `notifyInterrupts` into a channel that holds two values,
the most a run reacts to, so two signals in quick succession both arrive. `watchInterrupts` is the
per-run goroutine that turns the values into the cancellation and the second-signal channel.

## Why `ExecuteArgs` registers nothing

`signal.Notify` is process-global state. An embedder calling `ExecuteArgs` already has its own
handling, and a handler registered inside the library would compete with it. One command tree may
run any number of times, concurrently, and each run would register and unregister its own handler
against the same process-wide signals. A test passes a `chan struct{}` it controls and sends
interrupts deterministically, with no real signal raised.

## Considered options

- **Registering in `ExecuteArgs`.** It is the function every caller reaches, but it makes a library
  call own a process-global resource. An embedder loses its own handling, concurrent runs of one
  tree fight over the registration, and a test can only interrupt by raising a real signal.
- **`signal.NotifyContext`.** It yields one cancellation and nothing for a second signal, so the
  forced exit has no trigger. It also cancels on a signal and leaves nothing in the Env to inject,
  which returns the registration to `ExecuteArgs`.
- **`os.Signal` on `Env`, relaxing the no-`os`-import rule of `cli/spec`.** `doc.go` states the
  rule and `TestPackageImportsNoCLIFramework` enforces it, so the model stays free of the process.
  The same applies to a seam only `cli/core` can see: a field `cli/spec` does not declare cannot be
  carried by the `*spec.Env` the whole library already passes. A plain `<-chan struct{}` needs no
  import and says all a run needs, that an interrupt happened.
- **An injectable exit function.** Calling it on the second interrupt would end the process from
  inside the library and leave the return value of `ExecuteArgs` unreachable for that path. A test
  has to fake the exit, and an embedder cannot be sure the library does not end its process.
  Returning 130 leaves `main` the only caller of `os.Exit`.
- **Always 130 after an interrupt, even for a clean return.** It reports a failure for a run that
  finished its work, and a script cannot tell a completed command from an aborted one.
- **The exit mapping unchanged, a cancelled run exiting 1.** An interrupt is indistinguishable from
  a failure for a caller or a shell. 130 is the conventional status of a process ended by SIGINT,
  and a run error caused by cancellation is the case it names.

## Consequences

- **An abandoned run outlives the return.** After the second interrupt the run's goroutine may
  still write to the Env's streams. It is harmless in a process that exits right after, and an
  embedder that sends two values accepts it.
- **A run that ignores its context** ends only on the second interrupt.
- **SIGTERM and SIGINT are one case.** The exit code is 130 for both.
- A change to `cli/core` selects its dependents in CI (ADR-0006).
