---
about: "cobra may be imported only in cli, update/updatecmd and daemon/daemoncmd; engine packages must stay cobra-free, enforced by depguard"
saw:
  - docs/adr/0003-the-library-is-three-projects-cli-update-and-daemon.md
---

Kind: invariant.

ADR-0003: `github.com/spf13/cobra` v1 is allowed in exactly one place per project: `cli` (root command, wiring, completion, version command), `update/updatecmd`, `daemon/daemoncmd`. The engine packages (`update`, daemon runtime/supervisor/health/service managers) must not import it.

Why: a daemon's scheduled runner runs the update engine with no command in the process, and consumers embedding the engine in their own framework must not inherit a command tree. Each project's `.golangci.yml` carries a `depguard` rule denying cobra outside its command package, so a stray import fails `golangci-lint` in CI. Copying one project's `.golangci.yml` to another without adjusting the allowed package silently breaks or loosens the rule.
Evidence: ADR-0003 "The CLI framework dependency". No `.golangci.yml` exists yet in source/cli, so the rule is not yet implemented.
