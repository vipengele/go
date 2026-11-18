---
about: "clocktest.Manual runs AfterFunc callbacks synchronously inside Advance, departing from the clock.Clock goroutine contract, so a callback that calls Advance deadlocks and the system clock's goroutine semantics are not what a test sees"
saw:
  - source/commons/clock/clocktest/manual.go
  - source/commons/clock/clock.go
  - docs/adr/0040-the-shared-clock-lives-in-commons.md
---

- `Manual.Advance` takes the next due wait under `mu`, releases `mu`, then runs `f` (or closes the sleeper's channel) on the caller's goroutine; a second mutex, `advancing`, serialises concurrent Advance calls. Releasing `mu` before the callback is what lets `f` read `Now`, register timers inside the advanced window (fired in the same call) and stop timers; calling `Advance` from `f` deadlocks on `advancing`.
- `clock.Clock.AfterFunc` documents "its own goroutine", which the system clock honours through `time.AfterFunc`; `Manual` does not (ADR-0040 Consequences). Code that relies on the callback running concurrently with its caller behaves differently under a manual clock.
- A negative `Advance` panics; `AfterFunc(d <= 0)` is due at the current time and fires on the next `Advance`, `Advance(0)` included, never inside `AfterFunc`.
- `clock` carries no package-level variable: `System()` returns the zero-size `systemClock{}`, which `IsSystem` recognises by type assertion; `gochecknoglobals` is enabled, so a compile-time interface check `var _ clock.Clock = (*Manual)(nil)` is not written.
