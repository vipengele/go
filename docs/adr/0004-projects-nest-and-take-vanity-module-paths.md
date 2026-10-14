# Projects nest under `source/` and take vanity module paths

A project's module path is a vanity path on the host `go.vipengele.org`, built from its directory
under `source/`:

```
go.vipengele.org/<path>
```

`<path>` is the project's directory relative to `source/`. `source/cli/core` is the module
`go.vipengele.org/cli/core`, and its packages are imported as `go.vipengele.org/cli/core/<package>`.
`source/` organises the repository; it is not part of the namespace a consumer reads.

Projects may nest. A directory under `source/` is either a project, holding a `go.mod`, or a
grouping directory, holding projects and no `go.mod`. A project directory contains no other
`go.mod`, so a project never encloses another. The projects are:

| Directory | Module path | Holds |
|-----------|-------------|-------|
| `source/cli/core` | `go.vipengele.org/cli/core` | The app shell that ADR-0003 assigns to `cli`. |
| `source/cli/spec` | `go.vipengele.org/cli/spec` | The contracts a CLI tool and the libraries it assembles agree on, including `Identity`. |
| `source/update` | `go.vipengele.org/update` | Self-update, as ADR-0003 assigns it. |
| `source/daemon` | `go.vipengele.org/daemon` | The long-running runtime, as ADR-0003 assigns it. |
| `source/commons` | `go.vipengele.org/commons` | Generic building blocks any project may require. |

`source/cli` is a grouping directory. `update` and `daemon` stay one level under `source/`.

Every new or changed `go.mod` carries `go 1.27.2`.

## Tags stay the repository directory

A release is one tag per project, and the tag is the project's directory in the repository
followed by the version: `source/cli/core/vX.Y.Z`. The Go command computes the tag prefix of a
module from the repository subdirectory the module lives in, and from nothing else. No `go-import`
field, environment variable or `go.mod` directive changes it, so the tag carries `source/` even
though the module path does not.

A consumer never types a tag. It names the module path and the version:

```
go get go.vipengele.org/cli/core@v1.2.3
```

Each module path has exactly one tag series, `source/<path>/v*`.

## How `go.vipengele.org/...` resolves

`https://go.vipengele.org/<path>?go-get=1` answers with the four-field `go-import` meta tag that
Go 1.25 introduced, whose fourth field names the module's subdirectory in the repository:

```html
<meta name="go-import" content="go.vipengele.org/cli/core git https://github.com/vipengele/go source/cli/core">
```

The Go command clones `github.com/vipengele/go`, finds the module in `source/cli/core` and reads
its versions from the `source/cli/core/v*` tags. The module proxy and the checksum database do the
same on a consumer's behalf.

The host is a Cloudflare Worker written in TypeScript, in `tools/vanity-worker`. It is versioned in
this repository and deployed from this repository's CI. It owns the whole hostname
`go.vipengele.org`, attached as a Cloudflare custom domain, so no other site shares the host and no
route has to out-rank another. Its list of modules is generated from `source/**/go.mod`, so the
Worker answers for exactly the projects that exist; a path that is not a project gets no
`go-import` tag.

The `go.` subdomain is a Go-only arrangement. The platform's other ecosystems (npm, TypeScript,
React) name their packages by registry scope, not by hostname, so they keep their registry scopes
and no `ts.`, `react.` or similar hostname is reserved for imports.

`tools/` holds the repository's own tooling. `source/` holds the Go framework and nothing else.

## `commons`

`source/commons` holds generic building blocks: code with no knowledge of CLIs, updates or daemons
that more than one project needs. Reusable code is written in `commons` from the start, not grown in
one project and extracted later. `commons` requires no sibling, so any project can require it
without creating a cycle (ADR-0003).

## `Identity` lives in `cli/spec`

`Identity` — the tool's name, the release owner and repository, the environment-variable prefix and
the directory name — describes a command-line tool and how it is updated. It is a `cli`/`update`
concept, not a generic building block, so it belongs in `cli/spec` and not in `commons`.

This is the type ADR-0003 has the tool's `main` spell out twice, once for `cli`'s `App` and once for
`update.Config`. `App` and the rest of the shell remain in `cli/core`. Which projects require
`cli/spec`, and so whether `update` takes `Identity` from it, is a dependency-direction question
that belongs to the command abstraction below.

## Out of scope: the command abstraction

Confining cobra behind a framework-neutral `Command` declared in `cli/spec` is a separate decision,
ADR-0005 (`0005-commands-are-a-framework-neutral-model-in-cli-spec-and-cobra-is-an-internal-adapter.md`).
That ADR supersedes ADR-0003's rule on where cobra is allowed and its rule that `update`
requires no sibling. This ADR decides neither, and ADR-0003's dependency direction and cobra rule
stand as written for this decision.

## What this changes in earlier ADRs

- **ADR-0001.** A module path is not the repository path plus the directory; it is
  `go.vipengele.org/` plus the directory under `source/`. A project is not necessarily one level
  under `source/`; it may sit inside a grouping directory. The tag form, the release workflow and
  the rule that every project releases alone are unchanged. ADR-0001's reason for `source/` in the
  module path — Go requires a nested module's path to end with its directory — holds only for a
  module served from its repository path; a vanity path with a subdirectory field removes it.
- **ADR-0002.** A sibling is required under its vanity path:
  `require go.vipengele.org/cli/core v0.3.0`. How a sibling is required while two projects are
  developed together, and when it must be a released version, is ADR-0006, which supersedes
  ADR-0002.
- **ADR-0003.** The project inventory is `cli/core`, `cli/spec`, `update`, `daemon` and `commons`;
  ADR-0003's `cli` is `cli/core`. ADR-0003 rejected "a fourth project for the shared identity and
  paths" because it added a module to release for a handful of lines. That rejection stands for
  what it describes: no project exists only to hold identity. `Identity` goes into `cli/spec`, a
  project that exists for the CLI contracts as a whole, and `commons` exists for generic code and
  holds no identity. Neither is a module whose release is paid for by identity alone.
- **The rule `all-code-lives-under-source`.** The repository root also holds `tools/`: repository
  tooling that is not a Go project. It has no `go.mod`, is not released by tag and is deployed from
  CI. Go code that builds, tests or ships as a project lives under `source/`; `tools/` is the one
  other root directory, for tooling that is not a Go project.
- **The module-path formula in rules and the glossary.** The rules on module paths and on sibling
  dependencies, and the Project and Package entries of `CONTEXT.md`, give a module path as
  `github.com/vipengele/go/source/<project>`.
  Where they do, this ADR governs: the module path is `go.vipengele.org/<path>`.

## Considered options

- **Projects at the repository root (`cli/core/`, tags `cli/core/vX.Y.Z`).** Module paths and tags
  would then match without a vanity host, but code would live at the root, which
  `all-code-lives-under-source` rejects, and CI's change detection selects projects by their
  `source/` directory.
- **`source/` kept in the module path (`go.vipengele.org/source/cli/core`).** Needs only the
  three-field `go-import` tag and works on any Go release, but puts a repository-layout word into
  every import a consumer writes, which is the cost a vanity path exists to remove.
- **A path form on the apex: module paths under `/go/` on `vipengele.org`.** Couples Go module
  resolution to the apex host, which the marketing site also serves: the Worker needs a route for
  the `/go/` path that must out-rank the site's own, and the apex needs a placeholder origin behind
  it. Every module path is also longer by a `go` segment that says nothing a consumer needs. A
  subdomain lets the Worker own its host outright through a Cloudflare custom domain.
- **The `mod` vcs in `go-import`: `go.vipengele.org` as a self-hosted `GOPROXY` endpoint.** Makes
  tag names irrelevant, since the server would serve versions however it chooses, but it needs a
  server that builds and serves module zips rather than one static answer per path, and how the
  checksum database treats modules served that way is unconfirmed.
- **The Worker under `source/`.** `source/` holds Go projects that CI builds, tests and releases by
  tag; a TypeScript deployment is none of these, and every rule that selects projects from
  `source/` would have to learn to skip it.
- **`Identity` in `commons`.** Puts a CLI-and-update concept into the project meant to know nothing
  about either, and gives every consumer of a generic building block a type it has no use for.
- **A shared project holding only identity, as ADR-0003 weighed it.** Rejected there for the reason
  ADR-0003 gives, and it holds: a release cadence for two strings. This decision
  places identity in a project with its own reason to exist.

## Consequences

- **A Worker outage breaks resolution.** While `go.vipengele.org` does not answer, every `go get`
  of a version not already in a consumer's module cache or the public proxy fails, and every
  release's `announce` step, which asks the module proxy to fetch the new version, fails with it.
  Re-running the release finishes it once the Worker answers.
- **A pushed tag is permanent.** The module proxy and checksum database keep what they fetched; a
  wrong tag is answered by a new version, never by moving the tag.
- **A new project needs a Worker deploy before its first tag.** The Worker's module list is
  generated from `source/**/go.mod`, so until the deploy that includes the new `go.mod`, the
  project's path does not resolve and its release cannot be announced.
- **Consumers need Go 1.25 or later.** Earlier releases ignore the fourth `go-import` field, look
  for the module at the repository root and fail to resolve it.
- **pkg.go.dev needs its own first request.** For a module `go.vipengele.org/<dir>` in the
  repository directory `source/<dir>`, pkg.go.dev renders the package with source links under
  `.../blob/source/<dir>/<version>/source/<dir>/`. Its first request for a new module answers 404
  until the module has been requested once, which a visit to
  `https://pkg.go.dev/go.vipengele.org/<dir>@<version>` does.
- **A module path does not equal its directory.** The rule "a module path matches its directory"
  becomes "a module path matches the vanity map": the `module` line of `source/<path>/go.mod` is
  `go.vipengele.org/<path>`, which is what the Worker serves and what `verify-sibling-deps` checks.
- **A renamed or moved project starts a new version history.** Its module path, its tag prefix and
  the Worker's entry change together, and tags under the old directory stay where they are.
- **A new project** needs a `.lydite` component, a `.gt-repo.yaml` `gomod` entry and a
  `go.work` line, at its full nested directory (`./source/cli/spec`).
