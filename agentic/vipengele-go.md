# Vipengele Go

The `vipengele/go` repo: the Go libraries of the vipengele platform, holding **projects** — each a
self-contained Go module under `source/<project>/`. A project is released alone, by the tag
`source/<project>/vX.Y.Z` (ADR-0001). See `CONTEXT.md` for the glossary (Project, Package, Release,
Prefixed tag, Workspace, Sibling) before naming things.

**No code at the repo root** — everything that builds, tests or ships lives under `source/`
(`agentic/rules/all-code-lives-under-source.md`).

## Layout

- `source/<project>/` — one Go module per project, with its own `go.mod`, `go.sum` and, where it
  differs from the repo default, `.golangci.yml`. The module path is
  `github.com/vipengele/go/source/<project>` and must match the directory
  (`agentic/rules/module-path-matches-its-directory.md`). The `go` directive names a full patch release
  (`agentic/rules/go-directive-names-a-patched-release.md`). Public packages are subdirectories;
  everything not meant for consumers lives under `internal/`.
- `go.work` — lists every project for local development. CI never reads it (ADR-0002).
- A dependency on a sibling project is a `require` at a released tag. Never a `replace`, never a
  pseudo-version (`agentic/rules/a-sibling-is-required-at-a-released-version.md`, ADR-0002).
- `.github/actions/changed-projects` — the projects a change affects, dependents included; CI
  builds only those. A project may sit at any depth under `source/` (ADR-0004): its identifier is
  its path relative to `source/` (`cli/core`), a file belongs to the project with the longest
  matching directory, and a require of either `github.com/vipengele/go/source/<path>` or
  `go.vipengele.org/<path>` is an edge to the project owning `<path>`.
- `.github/actions/verify-sibling-deps` — fails a `replace` of a sibling, a sibling at a
  pseudo-version, a sibling version with no tag, and a module path that differs from its directory.
- `docs/adr/` — architecture decision records. Read before revisiting a decision recorded there.
- `docs/release-notes/<project>/vX.Y.Z.md` — one file per release tag. The release workflow refuses
  to run without it.

## Commands (run from `source/<project>`)

```bash
go build ./...
go vet ./...
go test -race -shuffle=on ./...
gofmt -l .            # prints files gofmt would change; CI fails on any output
golangci-lint run     # v2, the version pinned in .github/workflows/ci-build.yml
go mod tidy           # CI fails when this changes go.mod or go.sum
```

## CI

- `ci-build.yml` runs, per affected project with `GOWORK=off`: `go mod tidy` cleanliness, `gofmt`,
  `golangci-lint`, `go vet`, `go build`. It first runs `verify-sibling-deps` across the repo.
- `ci-test.yml` runs `go test -race -shuffle=on ./...` per affected project with `GOWORK=off`. The
  `lydite` stage gates coverage separately, running each `.lydite/components.yml` component's suite
  itself.
- A change to a shared CI file (`ci-*.yml`, the actions, `.lydite/`) selects every project; a change
  that touches no project (docs, agentic instructions) selects none and the stages skip.
- A new project needs a component in `.lydite/components.yml`, a `gomod` entry in `.gt-repo.yaml`
  followed by `gt repo sync`, and a `use` line in `go.work`.

## Release

A release is one project, one tag, pushed by a human (ADR-0001). The `release` skill walks the whole
sequence. `release.yml`: `tag` validates the tag form, that the project exists, that the commit is
on `main`, that `docs/release-notes/<project>/<version>.md` exists and that sibling requirements are
released versions; `test` runs the module's suite with `GOWORK=off`; `announce` asks the module
proxy to fetch the version and creates the GitHub Release. Re-running finishes a partial release.
