---
about: "every error cli/core writes to stderr goes through internal/errrender.Render at three sites in execute.go; the layer split is a ': '+child suffix strip with *cobra.UsageError special-cased, and renderers match per node by type assertion, not errors.As"
saw:
  - source/cli/core/execute.go
  - source/cli/core/internal/errrender/render.go
  - source/cli/core/errrender.go
  - source/cli/core/export_test.go
  - source/cli/core/execute_test.go
  - source/cli/core/render_golden_test.go
  - docs/adr/0011-errors-render-as-a-layer-tree-with-typed-renderers.md
---

- Print sites: `executeArgsCtx` renders the run error (skipped when `errors.As` finds a
  `*spec.ReportedError`) and the invalid-tree error, built as `fmt.Errorf("invalid command tree: %w",
  err)` so `Command.Validate`'s `errors.Join` becomes `•` rows and is never suppressed;
  `executeProcess` renders a `Getwd` failure with nil renderers. Nothing else writes an error; cobra is
  silenced (`SilenceErrors`, `SilenceUsage` in `internal/cobra/cobra.go`).
- `errrender.chain` splits a node with one `Unwrap() error` child by `strings.TrimSuffix(msg,
  ": "+child)`; a message that does not end that way, a nil child, or a multi-child node whose text is not
  the newline-join of its children prints whole and is not descended. `*cobra.UsageError` is checked
  first and printed whole: its `Error()` is `Path + ": " + Err` and it unwraps to `Err`, so the generic
  rule would turn every usage error into a tree.
- Renderers (`errrender.go` `typedRenderer`) match a node by `err.(E)`, the first registered match
  replacing the node's subtree; the exit-code registry (`exitcodes.go`) uses `errors.As` over the chain.
  Both are per-App lists because `gochecknoglobals` forbids a package-level one.
- `executeArgsCtx` takes the renderers as a trailing variadic parameter because `export_test.go` calls
  it with six arguments and is not changed by a renderer story.
- Exact stderr is asserted in `execute_test.go`, `app_test.go` and `flags_test.go` (the invalid-tree
  prefix is `"tool: invalid command tree\n"`), and pinned by `testdata/render/*.golden`. The `-update`
  flag is registered by `flag.Bool` in `TestMain` (`execute_test.go`) and read with `flag.Lookup` in
  `render_golden_test.go`, since a package-level `var` fails `gochecknoglobals` in test files too.
