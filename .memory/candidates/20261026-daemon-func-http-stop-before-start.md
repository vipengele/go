---
about: "Why Func and the HTTP runner return an error wrapping context.Canceled when Stop reaches them before Start, instead of an already-started error"
saw:
  - source/daemon/func.go
  - source/daemon/http.go
  - source/daemon/supervisor.go
  - source/daemon/func_test.go
---

- `Supervisor.start` calls each `Start` in a goroutine and, when the start context ends (timeout or parent cancel), stops the runner without waiting for that `Start` to return. `Stop` can therefore take the runner's mutex before `Start` does.
- A `Start` that finds `stopped` set launches nothing and returns `stoppedBeforeStart(name)` (func.go), an error wrapping `context.Canceled`; `launched.finish` drops context errors (`isContextErr`), so a clean cancellation during the start phase still returns nil from `Run`. Returning a plain "already started" error there is reported as a start failure.
- A real second `Start` still returns `errFuncStarted` / `errHTTPStarted`. `Stop` sets only `stopped`; only the first `Start` sets `claimed`.
- Covered by `TestFuncStopBeforeAPendingStartIsACleanCancellation` and the HTTP counterpart, which force the order with the `lateStart` wrapper in func_test.go.
