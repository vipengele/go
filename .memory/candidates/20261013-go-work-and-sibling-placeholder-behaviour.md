---
about: "Verified in a scratch two-module workspace: workspace mode builds a dependent whose sibling require is unreleased or absent, but go mod tidy and GOWORK=off both fail, and only a go.mod replace to the sibling's directory satisfies both; consumers ignore a published replace"
saw:
  - .github/workflows/ci-build.yml
  - .github/actions/changed-projects/action.yml
  - go.work
  - docs/adr/0006-a-sibling-is-developed-by-a-directory-replace-and-released-dependency-first.md
---

Scratch module pair (go 1.27.2): `go.vipengele.org/commons` and `go.vipengele.org/app` (imports commons), both in a go.work. Real network, go.vipengele.org answered 404 for `commons` (Worker not deployed with it).

Observed (app/go.mod requires commons):
- Workspace on, `require commons v0.0.0` or `v0.99.0` (unreleased), no replace: `go build`, `go vet`, `go test ./...` rc=0. With NO require line at all: also rc=0. `go list -m all` then errors with the 404 (it tries to resolve the version).
- Workspace on, `go mod tidy`: FAILS ("unrecognized import path ... 404"). tidy ignores go.work. `go work sync` does not touch app/go.mod.
- `GOWORK=off go build`: "missing go.sum entry for module providing package"; with `GOFLAGS=-mod=mod`: 404 fetch error. `GOWORK=off go mod tidy`: 404.
- `-mod=mod` is rejected in workspace mode ("-mod may only be set to readonly or vendor when in workspace mode").
- Placeholder `v9.9.9` is a parse error ("should be v0 or v1, not v9") and poisons go.mod for every command, even `go mod edit`; use v0.x.
- `replace go.vipengele.org/commons => ../commons` in app/go.mod: `GOWORK=off go mod tidy` rc=0, `GOWORK=off go build` rc=0, workspace build rc=0, tidy leaves go.mod unchanged, no go.sum entry needed.
- A third module requiring app (itself via local replace) ignores app's replace: build fails with missing go.sum / 404. So a replace left in a published go.mod breaks consumers.
- `go test ./...` inside source/app tests only app; at the workspace root `./...` errors "directory prefix . does not contain modules listed in go.work".

This is why siblings under development are a `require` plus a version-less `replace` to the sibling's own directory (`../commons` from `source/update`, `../../commons` from `source/cli/spec`), with CI on `GOWORK=off` and the tidy gate unchanged (`ci-build.yml:36`), rather than a placeholder `require` that only a workspace satisfies. A leftover replace is not caught by building, so verify-sibling-deps and release.yml's `tag` job check for it.

changed-projects builds edges from go.mod `Require` only (`changed-projects/action.yml:113-122`), so a dependent must keep its `require` line while it uses the library, even though workspace mode tolerates dropping it; without the line the dependent loses its edge and is not selected when the library changes.
