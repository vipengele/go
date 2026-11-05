---
about: "update's post-run hook must return nil, and its hooks are spec-typed funcs assignable to core's hook types without importing cli/core"
saw:
  - source/update/checkhooks.go
  - source/cli/core/hooks.go
  - source/cli/core/internal/cobra/cobra.go
---

Verified in code, 2026-11-04.

- cli/core runs every PostRun even when the run fails (internal/cobra/cobra.go, `runHooked`). When
  runErr is non-nil it is what gets reported, whatever the hooks return; when the run succeeded, a
  non-nil PostRun error becomes a `*HookError` and the command exits 1. So a hook that must never
  fail the command, like `Config.UpdateCheckHooks`'s post hook (checkhooks.go), returns nil always.
- depguard forbids update importing cli/core. The hooks are declared as unnamed func types built
  from `spec` types only; Go assigns an unnamed func type to core's named `PreRunHook` /
  `PostRunHook`, so a tool passes them to `core.WithPreRun` / `core.WithPostRun` directly.
- The `*Check` travels from pre-run to post-run in the context the pre-run hook returns, under an
  unexported key type (no package-level state; gochecknoglobals is on).
- The post hook calls `Finish` before `Notice`: otherwise the CheckWait wait never improves the
  notice and every release is announced one interval late. A done ctx (an interrupt) cancels the
  check at once and prints nothing.
