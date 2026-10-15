---
about: "the only place cli prints an error is one Fprintf in executeArgsCtx (execute.go), the ADRs record no rule for splitting a %w chain into per-layer text, and ADR-0010's exit-code registry is first-match in option order"
saw:
  - source/cli/core/execute.go
  - source/cli/core/exitcodes.go
  - source/cli/core/internal/cobra/cobra.go
  - docs/adr/0010-errors-map-to-exit-codes-through-a-registry-with-coded-and-reported-in-cli-spec.md
  - source/cli/core/.golangci.yml
---

Checked while planning issue #26 (top-level error rendering).

- Print sites: `execute.go` `executeArgsCtx` writes `"%s%v\n"` (prefix(root), err) unless
  `errors.As(err, *spec.ReportedError)`; `executeProcess` writes the same shape for a Getwd failure; the
  invalid-tree line is a third Fprintf. cobra is silenced (`SilenceErrors`, `SilenceUsage`,
  `cobra.go:123-124`), so cobra prints nothing.
- ADR-0010 only says `Reported` suppresses the `<name>: <message>` line, and that the invalid-tree line is
  never suppressed. No ADR, note or `git log` entry (`grep -rn render docs/adr CONTEXT.md`) decides how to
  derive per-%w-layer text. The only layered messages in the repo are hand-written `fmt.Errorf("x: %w", e)`.
- Registry shape (exitcodes.go): ordered `[]exitMatcher`, first registered match wins regardless of position
  inside an errors.Join; `typedMatcher` uses `errors.As`, `sentinelMatcher` `errors.Is`; a bad code panics in
  `New`. Registry is per-App because `gochecknoglobals` forbids a package one (ADR-0010 considered options),
  so a renderer registry in the same style would also be an App option; package-level Execute/ExecuteArgs
  have no options.
- Existing tests asserting the exact line: `execute_test.go:311-321`, `app_test.go:195`; the `result.stderr`
  helper is used ~25 times in `source/cli/core/*_test.go`.
- Deps: depguard in `source/cli/core/.golangci.yml` denies cobra outside internal/cobra and x/term outside
  cli/spec/terminal; a colour lib would need a rule/ADR. Types update/daemon need must live in cli/spec (ADR-0005).
- CI: `ci-test.yml:63` runs `go test -race -shuffle=on ./...`; lydite component `cli-core` runs
  `-race ./...` (`.lydite/components.yml`). No golden/testdata convention exists in cli yet
  (`ls source/cli/core/testdata` -> missing).
