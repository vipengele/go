---
about: "daemon's .golangci.yml depguard allow-lists name sibling imports one by one and omit commons, so a daemon.Clock replaced by a commons clock fails lint until every list is edited"
saw:
  - source/daemon/.golangci.yml
  - source/daemon/clock.go
  - source/daemon/go.mod
  - docs/adr/0028-lifecycle-time-and-readiness-are-injectable-seams.md
---

`source/daemon/.golangci.yml` rules (root-imports-only..., selfupdate-imports..., selfupdate-tests-also-import...,
daemoncmd-imports...) are `list-mode: lax` with an `allow:` of exact `go.vipengele.org/...$` paths and a
`deny: pkg: go.vipengele.org/` catch-all. `go.vipengele.org/commons` is in none of them (root daemon allows only
cli/spec, cli/spec/paths, filelock + its own subpackages). daemon's go.mod lists commons only as `// indirect`.
So importing a commons clock from daemon, selfupdate, daemoncmd or their tests needs the allow lists edited and
the require made direct, and a commons release containing the clock before daemon (ADR-0006 dependency-first).

Clock is part of daemon's public API: `daemon.Clock` (clock.go), `WithClock`/`WithHealthClock` (option.go, health.go),
`selfupdate.WithClock`/`WithRunnerClock`, `daemoncmd.UpdateConfig.Clock`, duplicate `systemClock` types in
selfupdate/options.go and daemoncmd/update.go, fake `daemontest.Clock` (var _ daemon.Clock). No consumer outside
source/daemon (grep of `daemon.Clock|daemontest` outside source/daemon -> 0 hits). ADR-0028 requires Now+AfterFunc
with time.Timer.Stop semantics, Deadline() = clock.Now()+d (earlier of parent's), withDeadline keeps
context.WithTimeout for the real clock (it type-asserts `realClock`, clock.go:~40, so a commons real clock needs an
equivalent identity check). daemon v0.1.0 notes exist (docs/release-notes/daemon/v0.1.0.md): breaking change.
