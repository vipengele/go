---
about: "issue #56's 'cobra constructor' conflicts with ADR-0005: update's command should be a []*spec.Command in updatecmd; update/.golangci.yml and AGENTS.md:265 still carry ADR-0003's cobra-in-updatecmd allowance"
saw:
  - docs/adr/0005-commands-are-a-framework-neutral-model-in-cli-spec-and-cobra-is-an-internal-adapter.md
  - docs/adr/0003-the-library-is-three-projects-cli-update-and-daemon.md
  - source/update/.golangci.yml
  - source/update/example_test.go
  - source/cli/spec/prompt.go
  - source/cli/spec/exit.go
---

Found while planning #56 (2026-11-05).

- ADR-0005:47-52,64-68: cobra appears in cli/core's go.mod only (cli/core/go.mod:6 is the sole cobra require under source/); update returns `[]*spec.Command` (`updatecmd.Commands(cfg)`), never imports cli/core. ADR-0003:79-90 (cobra allowed in update/updatecmd) is superseded (ADR-0005:90).
- Drift: source/update/.golangci.yml:16-21 and source/update/AGENTS.md:265 still permit cobra in `updatecmd`; ADR-0008:20 repeats it. updatecmd does not exist yet.
- No orchestrator exists. The documented composition is source/update/example_test.go:~118-175: CheckWritable -> Download -> Target -> Extract -> Install -> RunPostUpdate; on hook error the example calls Rollback, but hook.go:8-10 and ADR-0021 say hook failure is non-fatal (update applied, report it). Contradiction to settle in #56.
- Exit codes: spec.Coded(code, err) (cli/spec/exit.go:13), ADR-0010: 3..125 are tool's own; no documented "check" code in repo (agentic-toolkit's 10 is not mentioned anywhere).
- Prompting: (*spec.Invocation).Confirm/Prompt (cli/spec/prompt.go:81,109) ask only when Env stdin is a terminal; update.Config has no stdin/stdout seam (only IsTerminal for stderr).
