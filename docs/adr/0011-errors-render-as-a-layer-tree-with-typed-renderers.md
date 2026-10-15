# Errors render as a layer tree, with typed renderers registered on the App

Status: amends ADR-0010.

A tool's error is usually a chain: `load config: open app.yml: file does not exist`, or a joined
error whose children are chains of their own. Printed as one line, the layers run together and a
`errors.Join` collapses into newlines with no structure. `cli/core` prints a top-level error as a
tree of its layers, and a tool can render an error type of its own in place of the default.

## The tree

```
tool: load config
  → open app.yml
  → file does not exist
```

- The summary line is `<tool>: <the outermost layer's own text>`.
- Each inner `%w` layer is a `→` row. Each child of an `errors.Join` is a `•` row. Every depth
  indents by two spaces.
- The output is always UTF-8 with no colour and no ASCII fallback. An error message is for a person
  reading a terminal and for a log, and neither is helped by a second spelling of the same arrow.

A top-level `errors.Join` with no wrapper has the summary `N errors`. A join of one child renders as
that child.

## What a layer is

A node with exactly one `Unwrap() error` child has an own text of
`strings.TrimSuffix(node.Error(), ": "+child.Error())`: what the wrap added in front of the error
it wraps. Standard `fmt.Errorf("...: %w", err)` produces exactly that shape, so the heuristic works
on the errors tools already return.

- **Transparent.** The node's text equals its child's: `spec.CodedError`, `spec.ReportedError` and
  any other plain wrapper. The node is skipped and only its child is shown.
- **Opaque.** The child's text is not a `": "` suffix of the node's: a `%w` in the middle of the
  string, or a custom `Error()`. The node's full message is one row and the walk does not descend,
  because the child's text is already inside it.
- **Empty.** A layer whose own text is empty is dropped.
- **Multi-child.** A node with several children whose text is the newline-join of the children's
  texts is a join and renders its children as `•` rows. Any other multi-child node, such as a
  `fmt.Errorf` with several `%w`, is opaque.
- `*cobra.UsageError` is an opaque flat leaf, so a usage error stays on one line:
  `tool: greet: unknown flag: --bogus`.

## Typed renderers

`WithErrorRenderer[E error](fn func(E) string)` is an option of the `App`. A renderer is tried on
every node of the tree by a direct type assertion to `E`, not `errors.As`. The first registered
renderer that matches takes the node and its whole subtree. Its first line is the node's text and
any further lines are detail rows under it. An empty string means no rendering and falls back to
the default for that node.

```
tool: load config
  → invalid port
    must be 1-65535
    got 70000
```

The renderers are a per-App ordered list that nothing changes after `New`, the same shape as the
exit-code registry of ADR-0010, because `gochecknoglobals` forbids a package-level one. The
package-level `Execute` and `ExecuteArgs` take no options and have none.

## Where it applies

Three places write an error to standard error and all three go through the renderer: the main
`<name>: <error>` line, the `invalid command tree` line (rendered from
`fmt.Errorf("invalid command tree: %w", err)`, so each defect is a row), and a failure to read the
working directory, which happens before any App renderer is in play and uses the default alone.

Nothing else changes. `spec.Reported` still suppresses the line, an interruption still exits 130
silently, and the exit code is chosen by the precedence of ADR-0010.

## Considered options

- **A generic `command failed` header with `— and —` separators**, as the agentic-toolkit's
  `renderTopLevelError` prints. The header tells the reader nothing the exit code did not, and the
  summary line is the best place for the outermost layer's text.
- **Keep descending when a layer is opaque.** The child's text is already inside the node's, so
  descending prints the same words twice.
- **An opt-in `Layer()` interface.** Every error type, in every project and in the standard library,
  would have to adopt it. The heuristic works on the errors tools already return.
- **`errors.As` for renderers over the whole output.** It finds one error and hides the rest of a
  join and every other layer. A per-node match renders what it recognises and leaves the rest of the
  tree as it is.
- **An `io.Writer` renderer.** The renderer would own indentation and could break the tree. Returning
  a string leaves layout to `cli/core`.
