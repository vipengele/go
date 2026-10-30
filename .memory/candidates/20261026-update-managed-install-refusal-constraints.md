---
about: "source/update has no replace/apply step yet; container detection already exists as cli/spec/paths.InContainer; update's seams, deps and testing conventions constrain a managed-install refusal"
saw:
  - source/update/config.go
  - source/update/AGENTS.md
  - source/update/.golangci.yml
  - source/update/download.go
  - source/cli/spec/paths/container.go
  - docs/adr/0008-update-takes-its-identity-from-cli-spec.md
  - docs/adr/0009-platform-paths-live-in-cli-spec-paths.md
  - docs/adr/0020-a-download-returns-a-verified-archive-the-caller-never-sees-unverified.md
---

Explored for issue #52 (refuse externally managed installs).

- `update` may require only `cli/spec` and `commons` (ADR-0008); never `cli/core`; cobra only in an `updatecmd` package (`source/update/.golangci.yml` depguard). `go.mod` also needs `golang.org/x/mod` already, so third-party deps are not forbidden but ADR-0009 rejected `adrg/xdg` as a graph cost.
- Every outside-world access must be a `Config` field with a documented zero default resolved in `resolve` (`source/update/AGENTS.md` Conventions; `config.go:87` `Executable func() (string, error)`, `Getenv`, `GOOS`). `Executable` is declared and defaulted but no non-test code calls it yet (grep `\.Executable` in `source/update/*.go`).
- The only flow step in the package is `Config.Download` (`download.go:64`): resolve, checksums, archive. No extraction, replace or `updatecmd` exists, so a refusal check has no "replace" site to precede yet; it belongs before `Download` resolves/requests anything (ADR-0020 order).
- Container detection precedent: `source/cli/spec/paths/container.go` `InContainer(Probes)` (/.dockerenv, /run/.containerenv, `container`/KUBERNETES_SERVICE_HOST env, /proc/1/cgroup markers; linux only; injected GOOS/LookupEnv/Exists/ReadFile). ADR-0009 line 49: "Container detection is a heuristic that reports and changes no path." No recorded reason for REFUSING in containers.
- Testability precedent: ADR-0009 chose runtime `Options.GOOS` + injected probes over build tags so all layouts are covered on Linux-only CI. Only `_unix_test.go` files use `//go:build !windows`.
- Coverage: lydite gates coverage per `.lydite/components.yml` component (`update` runs `go test -race ./...`); thresholds are not in the repo (gt reusable workflow, external).
- Nothing in docs/adr or CONTEXT.md mentions Homebrew, Nix, symlinks/EvalSymlinks, "managed install" or refusal.
