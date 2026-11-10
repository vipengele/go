---
about: "the background update check runs CheckManaged on its own goroutine bounded by the check's context, because CheckManaged takes no context and would otherwise let Finish hang"
saw:
  - source/update/background.go
  - source/update/background_test.go
---

- After `runCheck`, the goroutine started by `Config.Start` runs the managed-install check only when a
  release would be announced (`k.announces(higher(k.saved, k.result))`, background.go:93) and stores its
  `Instead` for `Notice` (background.go:94).
- `CheckManaged` takes no context (it calls Executable, EvalSymlinks, the container probe and caller
  Detectors), so `managedInsteadWithin` (background.go:184) runs it on a goroutine of its own that
  delivers to a one-slot channel and selects against the check's context. A context already done skips
  it; a check still running when the context ends is abandoned and the notice keeps the default
  "Run '<name> update'" text. Without this bound `Finish` waits on `done` with no limit.
- Consequence: when the network check is cut off at CheckWait, a managed install (e.g. in a container)
  can still get the default notice text for a release already in the saved state.
- `TestFinishDoesNotWaitForABlockedManagedCheck` pins the bound with an Executable that blocks on a gate.
