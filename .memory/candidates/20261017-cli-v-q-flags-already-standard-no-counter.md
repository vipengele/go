---
about: "cli/core already owns -v/-q as standard flags (bool only, no counter, no log level); a logging feature must layer on them, not redeclare them"
saw:
  - source/cli/core/flags.go
  - source/cli/spec/flag.go
  - source/cli/core/hooks.go
  - docs/adr/0016-pre-run-hooks-return-the-context.md
  - docs/adr/0013-stdout-carries-data-stderr-carries-messages.md
  - docs/adr/0015-a-run-function-claims-exit-2-only-through-spec-usageerror.md
---

- `cli.WithVerbose()`/`WithQuiet()` (core/flags.go) declare root-inherited bool flags -v/--verbose, -q/--quiet; both together is a usage error (exit 2) before any hook runs (hooks.go WithPreRun doc). A command or logging flag declaring -v/-q makes the tree invalid.
- spec flag kinds are only BoolKind, StringKind, IntKind (spec/flag.go:15-19); no counter, no repeatable flag, so -vv does not exist.
- Hook point: cli.WithPreRun, runs after parse, receives inv and returns a context (ADR-0016). Hooks do not run for --help or rejected command lines.
- Exit 2 for run-function usage errors only via *spec.UsageError (ADR-0015); Coded(2) ignored.
- CONTEXT.md "Output mode" says Avoid "log level"; ADR-0013 Messages (stderr, silenced by quiet) are explicitly not logs. ADR-0003 lists "logging" under cli but no ADR designs it.
- All golangci configs enable gochecknoglobals and depguard; no forbidigo/sloglint. No git tags exist; only docs/release-notes/commons/v0.1.0.md exists.
