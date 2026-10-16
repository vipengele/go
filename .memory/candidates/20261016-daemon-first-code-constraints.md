---
about: "What binds daemon's first code: Runner/Supervisor glossary, cobra only in cli/core/internal/cobra, per-project .golangci.yml with gochecknoglobals, no signal handlers (cli/core owns them), ADR-0003/0005 dependency text disagree"
saw:
  - CONTEXT.md
  - docs/adr/0003-the-library-is-three-projects-cli-update-and-daemon.md
  - docs/adr/0005-commands-are-a-framework-neutral-model-in-cli-spec-and-cobra-is-an-internal-adapter.md
  - docs/adr/0012-signals-are-registered-by-execute-and-injected-through-env.md
  - source/daemon/doc.go
  - source/daemon/.golangci.yml
  - docs/adr/0014-the-supervisor-stops-runners-in-reverse-order-within-one-grace-period.md
  - source/commons/.golangci.yml
  - source/update/.golangci.yml
---

- CONTEXT.md:231-240 defines **Runner** ("start that blocks until its context is cancelled and a stop that returns once it has released everything. It knows nothing of its siblings or of restarts"; avoid worker/task/job/goroutine) and **Supervisor** ("starts every Runner, stops them in reverse order on shutdown or when one fails, and owns the process lifecycle and health. One per process"; avoid manager/orchestrator/scheduler). **Service** (:242) is the OS service-manager registration, not the daemon. "Lifecycle" is not a glossary term.
- ADR-0003 table assigns daemon "runners and supervisor, lifecycle, health, HTTP server kit, service managers, single-instance guard". Names `daemon/daemoncmd` for service commands (cobra home) and says engine packages ("daemon's runtime, supervisor, health and service managers") do not import cobra. ADR-0005 supersedes where cobra lives: only `cli/core/internal/cobra`, not in daemon at all; its depguard rule is "per project".
- ADR-0003 and ADR-0005 disagree on dependency direction (0003: daemon -> cli, update; 0005: daemon -> cli/core, cli/spec, update). The issue's deferral of the cli require is consistent with both (ADR-0006: no require needed until used).
- Runner and Supervisor live in the root package `daemon`, as other projects keep public API in their root package (update: config.go, source.go) and put sub-concerns in subdirs (commons/envvar). docs/adr/0014-the-supervisor-stops-runners-in-reverse-order-within-one-grace-period.md records the placement and rejects a subpackage.
- `source/daemon/.golangci.yml` copies commons' (`default: standard` + `gochecknoglobals`) and adds a `depguard` rule denying cobra and `go.vipengele.org/cli/core/**` everywhere; commons/config use `default: standard` + `gochecknoglobals`; update/cli-core add `depguard` (update denies `cli/core/**` and cobra outside updatecmd; cli-core denies cobra outside internal/cobra). Sentinel errors (`var ErrX = errors.New("pkg: ...")`, update/config.go:34) are the allowed globals. Errors are prefixed with the package name and wrapped with `%w` (update/config.go:154).
- Signals: ADR-0012 / cli/core/execute.go: `Execute` registers SIGINT/SIGTERM once, the first interrupt cancels the run context, a second returns 130 and abandons the run. `ExecuteArgs` registers nothing because `signal.Notify` is process-global. A supervisor under cli/core receives a cancelled ctx; it should not call signal.Notify. execute.go:220-228 is the repo's one panic idiom: recover in the goroutine, send `outcome{panicked, value}` on a buffered chan, re-panic on the caller.
- No ADR or note mentions errgroup, supervision, graceful shutdown ordering or restart policy; `grep errgroup|recover()` finds only execute.go.
