# Vipengele Go

The Go libraries of the vipengele platform: reusable modules for building Go services and
command-line tools, each versioned and released on its own.

## Language

**Project**:
A self-contained Go module in a directory at any depth under `source/`, with the module path
`github.com/vipengele/go/source/<project>`. Its identifier is its path relative to `source/`
(`update`, `cli/core`). A project is released alone, by the tag `source/<project>/vX.Y.Z`
(ADR-0001). A project's directory contains no other `go.mod`.
_Avoid_: module in prose about the repo's layout (the Go toolchain's word, used when talking about
`go.mod` itself), repo, package

**Grouping directory**:
A directory under `source/` that holds projects and has no `go.mod` of its own (`source/cli`). It is
not a project: it is never built, tested or released, and a change inside it but outside every
project selects nothing.
_Avoid_: parent project, namespace

**Package**:
One Go package inside a project, imported as `github.com/vipengele/go/source/<project>/<package>`.
Code a project does not export lives in `internal/`.
_Avoid_: library (ambiguous between package and project)

**Release**:
One project at one version, identified by its tag. The tag is the publication: the module proxy
serves the version from it, and the `release` workflow only validates, tests and announces it.
_Avoid_: publish (nothing is uploaded)

**Prefixed tag**:
The tag form `source/<project>/vX.Y.Z`. Go resolves a module in a subdirectory only from a tag
that starts with its directory path, so a nested project's tag is `source/cli/core/vX.Y.Z`.
_Avoid_: version tag (a bare `vX.Y.Z` is not a release here)

**Workspace**:
The root `go.work`, which lists every project so local development resolves siblings from disk
(ADR-0002). CI never uses it.
_Avoid_: monorepo module

**Sibling**:
Another project in this repo that a project requires. A sibling is always required at a released
version, never through `replace` (ADR-0002).
_Avoid_: local dependency

## Library vocabulary

These name what the `cli`, `update` and `daemon` projects build (ADR-0003). They describe the tools
a consumer writes with the library, not this repo; **Release** above is a version of a project here,
and a tool's own releases are reached through a **Release source**.

**App**:
The identity and shell of one tool, built in its `main` from the `cli` project: its binary name,
environment-variable prefix, version, root command, output mode and exit codes. The tool declares
the identity once and hands the same values to `update` and `daemon`.
_Avoid_: application, program, tool (the word for the consumer's whole product)

**Env**:
The surroundings a command reads, passed in explicitly: environment variables, the standard
streams, the working directory and the platform paths. Code takes an `Env`, never `os.Getenv` or
`os.Stdout`, so tests substitute it.
_Avoid_: environment (also means a deployment such as staging), context (`context.Context`), config
(values loaded from a file or flags)

**Release source**:
Where a tool's own published releases come from, behind the `update` project's `Source` interface:
it lists releases and serves the assets of one. It does not choose among them; version comparison
and asset selection do.
_Avoid_: update server, registry, feed, repository

**Managed binary**:
The installed executable of a tool, the file `update` replaces atomically with a verified staged
binary. For a daemon it is also the file the **Service** starts, which makes pending, confirm and
rollback states of the replacement visible to the **Supervisor**.
_Avoid_: executable, artifact, current binary

**Runner**:
One unit of long-running work in a daemon (the HTTP server, a scheduled task, the update check)
with a start that blocks until its context is cancelled and a stop that returns once it has
released everything. It knows nothing of its siblings or of restarts.
_Avoid_: worker, task, job, goroutine

**Supervisor**:
The `daemon` component that starts every **Runner**, stops them in reverse order on shutdown or
when one fails, and owns the process lifecycle and health. One per process.
_Avoid_: manager (a **Service** manager is the operating system's), orchestrator, scheduler

**Service**:
A daemon registered with the operating system's service manager (systemd, launchd, the Windows
service control manager), started, stopped and restarted by it. Installing, removing and querying
a Service are the `daemoncmd` commands.
_Avoid_: daemon for the registration (the daemon is the program, the Service is its registration),
unit, agent, microservice
