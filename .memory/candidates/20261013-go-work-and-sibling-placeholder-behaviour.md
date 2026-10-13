---
about: "Verified in a scratch two-module workspace: workspace mode builds a dependent whose sibling require is unreleased or absent, but go mod tidy and GOWORK=off both fail, and only a go.mod replace to the directory satisfies both (consumers ignore it)"
saw:
  - .github/workflows/ci-build.yml
  - .github/actions/changed-projects/action.yml
  - go.work
  - docs/adr/0002-a-cross-project-dependency-is-a-released-version-linked-locally-by-go-work.md
---

Scratch module pair (go 1.27.2): `go.vipengele.org/commons` and `go.vipengele.org/app` (imports commons), both in a go.work. Real network, go.vipengele.org currently answers 404 for `commons` (Worker not deployed with it).

Observed (app/go.mod requires commons):
- Workspace on, `require commons v0.0.0` or `v0.99.0` (unreleased), no replace: `go build`, `go vet`, `go test ./...` rc=0. With NO require line at all: also rc=0. `go list -m all` then errors with the 404 (it tries to resolve the version).
- Workspace on, `go mod tidy`: FAILS ("unrecognized import path ... 404"). tidy ignores go.work. `go work sync` does not touch app/go.mod.
- `GOWORK=off go build`: "missing go.sum entry for module providing package"; with `GOFLAGS=-mod=mod`: 404 fetch error. `GOWORK=off go mod tidy`: 404.
- `-mod=mod` is rejected in workspace mode ("-mod may only be set to readonly or vendor when in workspace mode").
- Placeholder `v9.9.9` is a parse error ("should be v0 or v1, not v9") and poisons go.mod for every command, even `go mod edit`; use v0.x.
- `replace go.vipengele.org/commons => ../commons` in app/go.mod: `GOWORK=off go mod tidy` rc=0, `GOWORK=off go build` rc=0, workspace build rc=0, tidy leaves go.mod unchanged, no go.sum entry needed.
- A third module requiring app (itself via local replace) ignores app's replace: build fails with missing go.sum / 404. So a replace left in a tagged go.mod breaks consumers (the reason ADR-0002 bans it).
- `go test ./...` inside source/app tests only app; at the workspace root `./...` errors "directory prefix . does not contain modules listed in go.work".

changed-projects builds edges from go.mod `Require` only (`changed-projects/action.yml:114-122`), so a dependent that drops the require line (which workspace mode tolerates) loses its edge and is not selected when the library changes.
