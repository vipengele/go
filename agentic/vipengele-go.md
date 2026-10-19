# Vipengele Go

The `vipengele/go` repo: the Go libraries of the vipengele platform, holding **projects** — each a
self-contained Go module in a directory at any depth under `source/` (ADR-0004), with the vanity
module path `go.vipengele.org/<path>`, where `<path>` is its directory relative to `source/`. A
project is released alone, by the tag `source/<path>/vX.Y.Z` (ADR-0001). See `CONTEXT.md` for the
glossary (Project, Grouping directory, Package, Release, Prefixed tag, Workspace, Sibling) before
naming things.

**No code at the repo root** — the Go framework, everything that builds, tests or ships, lives
under `source/`; the repository's own tooling that is not a Go project lives under `tools/`
(`agentic/rules/all-code-lives-under-source.md`).

## Layout

- `source/<path>/` — one Go module per project, with its own `go.mod`, `go.sum` and, where it
  differs from the repo default, `.golangci.yml`. A project may sit at any depth: a directory under
  `source/` is either a project, holding a `go.mod`, or a grouping directory (`source/cli`), holding
  projects and no `go.mod`; a project directory contains no other `go.mod` (ADR-0004).
- The module path is `go.vipengele.org/<path>`, not the repository path: `source/cli/core` is
  `go.vipengele.org/cli/core`, and its packages are `go.vipengele.org/cli/core/<package>`. The tag
  is the repository directory, `source/` included: `source/cli/core/vX.Y.Z`
  (`agentic/rules/module-path-matches-the-vanity-map.md`). The `go` directive names a full patch
  release (`agentic/rules/go-directive-names-a-patched-release.md`). Public packages are
  subdirectories; everything not meant for consumers lives under `internal/`.
- Consumers need Go 1.25 or later: `go.vipengele.org` answers with the four-field `go-import` tag,
  whose subdirectory field earlier releases ignore.
- `tools/vanity-worker` — the Cloudflare Worker that serves `go.vipengele.org`. It reads its module list
  from the R2 object `modules.json`, which `publish-vanity-modules.yml` writes from `source/**/go.mod`
  when a `go.mod` merges to `main`, so a new project resolves once that workflow has run, with no
  Worker deploy (ADR-0016). A `tools/vanity-worker/vX.Y.Z` tag deploys Worker code only. Repo
  tooling, not part of the framework: no `go.mod`, deployed from CI.
- `go.work` — lists every project for local development. CI never reads it (ADR-0006).
- A dependency on a sibling project is a `require` of `go.vipengele.org/<path>`. While the two are
  developed together it also carries a `replace` with the sibling's own directory, relative to the
  dependent (`../commons` from `source/update`, `../../commons` from `source/cli/spec`), and the
  required version is a `v0` placeholder or the last release. Otherwise it is a tagged release, never
  a pseudo-version (`agentic/rules/a-sibling-is-developed-by-a-directory-replace.md`, ADR-0006).
- `.github/actions/changed-projects` — the projects a change affects, dependents included; CI
  builds only those. A project's identifier is its path relative to `source/` (`cli/core`), a file
  belongs to the project with the longest matching directory, and a require of
  `go.vipengele.org/<path>` is an edge to the project owning `<path>`.
- `.github/actions/verify-sibling-deps` — checks every project's `go.mod`. A project is in strict
  mode while some `docs/release-notes/<path>/v<version>.md` has no tag `source/<path>/v<version>`:
  it then fails any `replace` of a sibling and any sibling version with no tag. Otherwise it accepts
  a version-less `replace` of a sibling by that sibling's own project directory, and fails a
  `replace` naming a version or another directory and a sibling with no `replace` at a version with
  no tag. In both modes it fails a sibling at a pseudo-version, a `replace` of a non-sibling, a
  project directory holding another `go.mod`, and a module path other than
  `go.vipengele.org/<path>` for its directory `source/<path>`.
- `docs/adr/` — architecture decision records. Read before revisiting a decision recorded there.
- `docs/release-notes/<path>/vX.Y.Z.md` — one file per release tag. The release workflow refuses
  to run without it.

## Commands (run from `source/<path>`)

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
  `GOWORK=off` resolves each sibling as the project's `go.mod` names it: through its directory
  `replace` while they are developed together, at its tag in strict mode.
- `ci-test.yml` runs `go test -race -shuffle=on ./...` per affected project with `GOWORK=off`. The
  `lydite` stage gates coverage separately, running each `.lydite/components.yml` component's suite
  itself.
- A change to a shared CI file (`ci-*.yml`, the actions, `.lydite/`) selects every project; a change
  that touches no project (docs, agentic instructions) selects none and the stages skip.
- A new project needs a component in `.lydite/components.yml`, a `gomod` entry in `.gt-repo.yaml`
  followed by `gt repo sync`, and a `use` line in `go.work`, each at its full nested directory
  (`./source/cli/core`). It resolves through `go.vipengele.org` once its `go.mod` has merged and
  `publish-vanity-modules.yml` has run; check it before its first tag.

## Release

A release is one project, one tag, pushed by a human (ADR-0001). The tag is
`source/<path>/vX.Y.Z`; a consumer never types it and runs `go get go.vipengele.org/<path>@vX.Y.Z`.
The release pull request carries the notes, drops the sibling `replace`s and requires each sibling
at its tag, and the tag goes on its merge commit; release order is dependency-first at release time
only. The `release` skill walks the whole sequence. `release.yml`: `tag` validates the tag form,
that the project exists, that the commit is on `main`, that
`docs/release-notes/<path>/<version>.md` exists, and that the tagged project's `go.mod` carries no
`replace` and requires every sibling at a tagged version — no other project's `go.mod` is read, so
a sibling under development elsewhere never blocks the release; `test` runs, with `GOWORK=off`,
`go mod tidy` failing on any change to `go.mod` or `go.sum`, `go build`, `go vet` and the module's
suite; `announce` asks the module proxy to fetch the version, which resolves through
`go.vipengele.org`, and creates the GitHub Release. Re-running finishes a partial release, including
one whose `announce` failed while the Worker did not answer.
