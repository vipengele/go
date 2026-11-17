---
about: "command aliases and flag groups: spec.Validate owns the static rules, the cobra adapter dispatches aliases and enforces groups itself (not cobra MarkFlags*), and cobra does not offer aliases as completion candidates"
saw:
  - source/cli/spec/command.go
  - source/cli/spec/flaggroup.go
  - source/cli/core/internal/cobra/cobra.go
  - source/cli/core/execute.go
  - source/cli/core/execute_test.go
---
- Aliases: `spec.Command.Aliases`; `validateAliases` (command.go) reports a root alias, a non-slug,
  an alias equal to Name and a repeat under the command's own path; sibling collisions (name vs
  alias, alias vs alias) are reported by the parent through its `claimed` map. `help` is not
  reserved. cli/core's `completionAliasError` (execute.go, joined with `root.Validate()` in
  `executeArgsEnded`) rejects a root-child alias "completion" only when `cfg.Completion` is on; a
  child *named* completion still replaces cobra's command.
- The adapter sets `Aliases` in `build()` (cobra.go). Cobra dispatches by alias and prints an
  "Aliases:" help line, but `__complete` offers only command *names* as candidates: an alias is
  resolved when descending into it, never suggested (pinned in execute_test.go).
- Flag groups: `spec.FlagGroup{Kind, Flags}`; the zero `FlagGroupKind` is invalid on purpose.
  `validateFlagGroups` (flaggroup.go) checks names against the existing flagScope (own + inherited;
  cli/core's standard flags are inherited flags of the synthetic root, so groups may name them) and
  rejects groups on a command with no Run, since only a Run-bearing command's RunE enforces them.
- Enforcement is `checkGroups(cmd.Flags(), groups)` in the RunE of `build()`, right after `read`
  and before `cfg.Output` and the pre-run hooks; "set" is pflag `Changed`, which covers inherited
  flags given before or after the subcommand. A violation is `*UsageError` -> exit 2. Cobra's
  `MarkFlagsMutuallyExclusive`/`RequiredTogether` were rejected for wording, exit code and
  ordering before hooks.
