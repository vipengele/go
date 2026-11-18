# Vipengele Go

The `vipengele/go` repo: the Go libraries of the vipengele platform, holding **projects** — each a
self-contained Go module in a directory at any depth under `source/` (ADR-0004), with the vanity
module path `go.vipengele.org/<path>`, where `<path>` is its directory relative to `source/`. A
project is released alone, by the tag `source/<path>/vX.Y.Z` (ADR-0001), which `auto-release`
creates when the notes merge (ADR-0037). See `CONTEXT.md` for the
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
- `source/examples` — a grouping directory holding examples: projects that demonstrate released
  projects. An example is never tagged and has no release notes; it requires its siblings at their
  tags and carries no `replace`. `docs/guide/consumer.md` is checked by the tests of
  `source/examples/greet` (`TestGuide`, `TestRelease`) and by `.github/workflows/guide-snippets.yml`,
  which is path-filtered and not a `ci-*` file.
- The module path is `go.vipengele.org/<path>`, not the repository path: `source/cli/core` is
  `go.vipengele.org/cli/core`, and its packages are `go.vipengele.org/cli/core/<package>`. The tag
  is the repository directory, `source/` included: `source/cli/core/vX.Y.Z`
  (`agentic/rules/module-path-matches-the-vanity-map.md`). The `go` directive names a full patch
  release (`agentic/rules/go-directive-names-a-patched-release.md`). Public packages are
  subdirectories; everything not meant for consumers lives under `internal/`.
- Consumers need Go 1.27 or later, the release every project's `go` directive names.
  `go.vipengele.org` answers with the four-field `go-import` tag, whose subdirectory field releases
  before 1.25 ignore, so 1.25 is the technical minimum for the vanity path.
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
  `go.vipengele.org/<path>` is an edge to the project owning `<path>`. A change to
  `.github/workflows/release-tool*.yml` selects `update`, which owns those workflows.
- `.github/workflows/release-tool.yml` — the reusable workflow through which a tool built on
  `go.vipengele.org/update` releases: GoReleaser over its committed config, a minisign signature over
  `checksums.txt` and a GitHub Release. A tool pins it by the commit SHA of the tag
  `source/update/vX.Y.Z`, the version of its `update` require, written `@<sha> # source/update/vX.Y.Z`,
  so a change to it reaches tools only through an `update` release.
  `release-tool-selftest.yml` is its dry run against `source/update/testdata/releasefixture`; it is
  not a merge gate (ADR-0033). This repository's own releases stay with `auto-release.yml` and `release.yml`.
- `.github/actions/verify-sibling-deps` — checks every project's `go.mod`. A project is in strict
  mode while some `docs/release-notes/<path>/v<version>.md` has no tag `source/<path>/v<version>`:
  it then accepts a version-less `replace` of a sibling by that sibling's own project directory when
  the sibling is required at its pending version (notes exist, tag does not) or at an existing tag,
  and fails a sibling with no `replace` at a version with no tag. Otherwise it accepts the same
  `replace` at any version, and fails a sibling with no `replace` at a version with no tag. In both
  modes it fails a `replace` naming a version or another directory, a sibling at a pseudo-version, a
  `replace` of a non-sibling, a project directory holding another `go.mod`, and a module path other
  than `go.vipengele.org/<path>` for its directory `source/<path>`.
- `docs/adr/` — architecture decision records. Read before revisiting a decision recorded there.
- `docs/release-notes/<path>/vX.Y.Z.md` — one file per release tag. Merging it to `main` releases
  the project, and the release workflow refuses to run without it.

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
  `golangci-lint`, `go vet`, `go build`, and `GOOS=windows go vet ./...` for a project with any
  `*_windows.go` or `*_windows_test.go` file (ADR-0019). It first runs `verify-sibling-deps` across
  the repo.
  `GOWORK=off` resolves each sibling as the project's `go.mod` names it: through its directory
  `replace` while they are developed together, and a project whose release is written may keep it
  (ADR-0038).
- `ci-test.yml` runs `go test -race -shuffle=on ./...` per affected project with `GOWORK=off`. The
  `lydite` stage gates coverage separately, running each `.lydite/components.yml` component's suite
  itself.
  Its `test-windows` job also runs that command on `windows-latest`, for each affected project
  holding a `*_windows.go` or `*_windows_test.go` file, and is skipped when none does. It gates
  merges like the Linux job (ADR-0030). Coverage stays Linux-only.
- `ci-test.yml`'s `test-supervisor` job runs when `changed-projects` selects `daemon`:
  `SERVICETEST=1 go test -v -run '^TestSupervisor$' ./servicetest/` on `ubuntu-latest` with rootless
  podman. It fails unless the log shows `--- PASS: TestSupervisor`, since a skipped test exits 0, and
  an `always()` step removes the containers labelled `servicetest`. It gates merges through the test
  stage.
- `release-selftest.yml` is the offline self-test of the release scripts and workflows, including
  `verify-sibling-deps/verify.sh`, and actionlint over `release.yml`, `auto-release.yml`, itself and `ci-test.yml`. `ci-test.yml`'s
  `release-selftest` job calls it on every run of the test stage, so it gates `ci-gate` through that
  stage. `ci-orchestration.yml` is rendered by `gt` from `.gt-repo.yaml`, which accepts only the
  `preflight`, `build`, `test` and `end2end` stages, so the self-test is a job of the repo-owned
  test stage.
- A change to a shared CI file (`ci-*.yml`, the actions, `.lydite/`, the release scripts under
  `.github/scripts/release/`, `release.yml`, `auto-release.yml`, `release-selftest.yml`) selects
  every project; a change that touches no project (docs, agentic instructions) selects none and the
  stages skip.
- A new project needs a component in `.lydite/components.yml`, a `gomod` entry in `.gt-repo.yaml`
  followed by `gt repo sync`, and a `use` line in `go.work`, each at its full nested directory
  (`./source/cli/core`). It resolves through `go.vipengele.org` once its `go.mod` has merged and
  `publish-vanity-modules.yml` has run; release validation waits for it.

## Release

A release is one project, one tag (ADR-0001), made by `auto-release.yml` when the pull request that
adds `docs/release-notes/<path>/vX.Y.Z.md` merges to `main` (ADR-0037). The tag is
`source/<path>/vX.Y.Z`, an unsigned annotated tag created as `github-actions[bot]`; a consumer never
types it and runs `go get go.vipengele.org/<path>@vX.Y.Z`. The release pull request carries the
notes, and for a dependent requires each sibling at its tag or pending version and keeps its
`replace`. One pull request releases a chain: `auto-release.yml` releases it tier by tier, and a
dependent is tagged on a **release commit** (ADR-0038), the merge commit with only that project's
`go.mod` and `go.sum` changed, its `replace`s dropped and tidied against the siblings' tags. The
commit is on no branch and `main` keeps the `replace`. The `release` skill walks the whole sequence.

`auto-release.yml` runs on every push to `main`: it plans the pending projects (notes exist, tag
does not) as dependency tiers and calls `release.yml` for each, tier by tier. `release.yml` has no
tag-push trigger; a tag pushed by hand runs nothing. Its jobs: `verify` (read-only) validates the
version, the project, the notes and the commit, that the project's `go.mod` declares
`go.vipengele.org/<path>` (with the suffix `/vN` for a `vN.x.y` tag, N of 2 or more) and, once the
release commit has dropped the sibling `replace`s and tidied against the siblings' tags, carries no
`replace` and requires every sibling at a tagged version — no other project's `go.mod` is read, so a
sibling under development elsewhere never blocks the release — that the commit is on `main` or a
release commit whose single parent is, and that `go.vipengele.org/<path>?go-get=1`
answers, retrying the Worker for up to 5 minutes, then runs, with `GOWORK=off`, `go mod tidy`
failing on any change to `go.mod` or `go.sum`, `go build`, `GOOS=windows go vet` when the project
holds a `*_windows.go` or `*_windows_test.go` file, `go vet` and the module's suite; `tag` (write)
rebuilds the identical release commit, requires the id `verify` tested, tags it in release mode and
pushes only the tag; `announce` (write) asks the module proxy to fetch the version,
which resolves through `go.vipengele.org`, and creates the GitHub Release. A partial release is
finished by dispatching `release.yml` with the tag as its `tag` input; each run also announces any
tag that has no GitHub Release.
