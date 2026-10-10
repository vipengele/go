# Projects nest under `source/` and take vanity module paths

A project's module path is a vanity path on `vipengele.org`, built from its directory under
`source/`:

```
vipengele.org/go/<path>
```

`<path>` is the project's directory relative to `source/`. `source/cli/core` is the module
`vipengele.org/go/cli/core`, and its packages are imported as `vipengele.org/go/cli/core/<package>`.
`source/` organises the repository; it is not part of the namespace a consumer reads.

Projects may nest. A directory under `source/` is either a project, holding a `go.mod`, or a
grouping directory, holding projects and no `go.mod`. A project directory contains no other
`go.mod`, so a project never encloses another. The projects are:

| Directory | Module path | Holds |
|-----------|-------------|-------|
| `source/cli/core` | `vipengele.org/go/cli/core` | The app shell that ADR-0003 assigns to `cli`. |
| `source/cli/spec` | `vipengele.org/go/cli/spec` | The contracts a CLI tool and the libraries it assembles agree on, including `Identity`. |
| `source/update` | `vipengele.org/go/update` | Self-update, as ADR-0003 assigns it. |
| `source/daemon` | `vipengele.org/go/daemon` | The long-running runtime, as ADR-0003 assigns it. |
| `source/commons` | `vipengele.org/go/commons` | Generic building blocks any project may require. |

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
go get vipengele.org/go/cli/core@v1.2.3
```

Each module path has exactly one tag series, `source/<path>/v*`.

## How `vipengele.org/go/...` resolves

`https://vipengele.org/go/<path>?go-get=1` answers with the four-field `go-import` meta tag that
Go 1.25 introduced, whose fourth field names the module's subdirectory in the repository:

```html
<meta name="go-import" content="vipengele.org/go/cli/core git https://github.com/vipengele/go source/cli/core">
```

The Go command clones `github.com/vipengele/go`, finds the module in `source/cli/core` and reads
its versions from the `source/cli/core/v*` tags. The module proxy and the checksum database do the
same on a consumer's behalf.

The host is a Cloudflare Worker written in TypeScript, in `tools/vanity-worker`. It is versioned in
this repository and deployed from this repository's CI. Its list of modules is generated from
`source/**/go.mod`, so the Worker answers for exactly the projects that exist; a path that is not a
project gets no `go-import` tag.

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
ADR-0005. That ADR supersedes ADR-0003's rule on where cobra is allowed and its rule that `update`
requires no sibling. This ADR decides neither, and ADR-0003's dependency direction and cobra rule
stand as written for this decision.

## What this changes in earlier ADRs

- **ADR-0001.** A module path is not the repository path plus the directory; it is
  `vipengele.org/go/` plus the directory under `source/`. A project is not necessarily one level
  under `source/`; it may sit inside a grouping directory. The tag form, the release workflow and
  the rule that every project releases alone are unchanged. ADR-0001's reason for `source/` in the
  module path — Go requires a nested module's path to end with its directory — holds only for a
  module served from its repository path; a vanity path with a subdirectory field removes it.
- **ADR-0002.** Unchanged in substance: a sibling is required at a released version, now under its
  vanity path (`require vipengele.org/go/cli/core v0.3.0`).
- **ADR-0003.** The project inventory is `cli/core`, `cli/spec`, `update`, `daemon` and `commons`;
  ADR-0003's `cli` is `cli/core`. ADR-0003 rejected "a fourth project for the shared identity and
  paths" because it added a module to release for a handful of lines. That rejection stands for
  what it describes: no project exists only to hold identity. `Identity` goes into `cli/spec`, a
  project that exists for the CLI contracts as a whole, and `commons` exists for generic code and
  holds no identity. Neither is a module whose release is paid for by identity alone.

## Considered options

- **Projects at the repository root (`cli/core/`, tags `cli/core/vX.Y.Z`).** Module paths and tags
  would then match without a vanity host, but code would live at the root, which
  `all-code-lives-under-source` rejects, and CI's change detection selects projects by their
  `source/` directory.
- **`source/` kept in the module path (`vipengele.org/go/source/cli/core`).** Needs only the
  three-field `go-import` tag and works on any Go release, but puts a repository-layout word into
  every import a consumer writes, which is the cost a vanity path exists to remove.
- **The `mod` vcs in `go-import`: `vipengele.org` as a self-hosted `GOPROXY` endpoint.** Makes tag
  names irrelevant, since the server would serve versions however it chooses, but it needs a server
  that builds and serves module zips rather than one static answer per path, and how the checksum
  database treats modules served that way is unconfirmed.
- **The Worker under `source/`.** `source/` holds Go projects that CI builds, tests and releases by
  tag; a TypeScript deployment is none of these, and every rule that selects projects from
  `source/` would have to learn to skip it.
- **`Identity` in `commons`.** Puts a CLI-and-update concept into the project meant to know nothing
  about either, and gives every consumer of a generic building block a type it has no use for.
- **A shared project holding only identity, as ADR-0003 weighed it.** Rejected there for the reason
  ADR-0003 gives, and it holds: a release cadence for two strings. This decision
  places identity in a project with its own reason to exist.

## Consequences

- **A Worker outage breaks resolution.** While `vipengele.org` does not answer, every `go get` of a
  version not already in a consumer's module cache or the public proxy fails, and every release's
  `announce` step, which asks the module proxy to fetch the new version, fails with it. Re-running
  the release finishes it once the Worker answers.
- **A pushed tag is permanent.** The module proxy and checksum database keep what they fetched; a
  wrong tag is answered by a new version, never by moving the tag.
- **A new project needs a Worker deploy before its first tag.** The Worker's module list is
  generated from `source/**/go.mod`, so until the deploy that includes the new `go.mod`, the
  project's path does not resolve and its release cannot be announced.
- **Consumers need Go 1.25 or later.** Earlier releases ignore the fourth `go-import` field, look
  for the module at the repository root and fail to resolve it.
- **A module path does not equal its directory.** The rule "a module path matches its directory"
  becomes "a module path matches the vanity map": the `module` line of `source/<path>/go.mod` is
  `vipengele.org/go/<path>`, which is what the Worker serves and what `verify-sibling-deps` checks.
- **A renamed or moved project starts a new version history.** Its module path, its tag prefix and
  the Worker's entry change together, and tags under the old directory stay where they are.
- **A new project** needs a `.lydite` component, a `.gt-repo.yaml` `gomod` entry and a
  `go.work` line, at its full nested directory (`./source/cli/spec`).
