# Vipengele Go

The Go libraries of the vipengele platform: reusable modules for building Go services and
command-line tools, each versioned and released on its own.

## Language

**Project**:
A self-contained Go module in a directory at any depth under `source/`, with the **Vanity path**
`go.vipengele.org/<path>` as its module path. Its identifier is its `<path>`, its directory
relative to `source/` (`update`, `cli/core`). A project is released alone, by the tag
`source/<path>/vX.Y.Z` (ADR-0001, ADR-0004). A project's directory contains no other `go.mod`.
_Avoid_: module in prose about the repo's layout (the Go toolchain's word, used when talking about
`go.mod` itself), repo, package

**Grouping directory**:
A directory under `source/` that holds projects and has no `go.mod` of its own (`source/cli`). It is
not a project: it is never built, tested or released, and a change inside it but outside every
project selects nothing.
_Avoid_: parent project, namespace

**Package**:
One Go package inside a project, imported as `go.vipengele.org/<project>/<package>`.
Code a project does not export lives in `internal/`.
_Avoid_: library (ambiguous between package and project)

**Vanity path**:
The module path `go.vipengele.org/<path>` of a project, where `<path>` is its directory relative
to `source/` (`go.vipengele.org/cli/core` for `source/cli/core`). It differs from the repository
path, `github.com/vipengele/go/source/<path>`, which is where the code lives and which no
consumer writes. The Worker in the **Tools directory** serves it (ADR-0004).
_Avoid_: import path (also names a package's path inside the module), repository path, short path

**Module list**:
The JSON array of project paths relative to `source/`, one for every `source/**/go.mod` on `main`,
published to R2 as the object `modules.json` by the `publish-vanity-modules` workflow
(ADR-0016). It is data, separate from the Worker's code: a project added, moved or removed changes
it without a Worker tag.
_Avoid_: project index, manifest, registry

**Release**:
One project at one version, identified by its tag. The tag is the publication: the module proxy
serves the version from it, and the `release` workflow only validates, tests and announces it.
_Avoid_: publish (nothing is uploaded)

**Prefixed tag**:
The tag form `source/<path>/vX.Y.Z`. Go resolves a module in a subdirectory only from a tag
that starts with its directory path, so the tag names the repository directory, `source/`
included, and not the **Vanity path**: the project `go.vipengele.org/cli/core` is tagged
`source/cli/core/vX.Y.Z`. A consumer names the module path and a version, never the tag.
_Avoid_: version tag (a bare `vX.Y.Z` is not a release here)

**Workspace**:
The root `go.work`, which lists every project for local development (ADR-0006). CI never uses it,
and a project never depends on it: a sibling developed alongside is reached through its directory
`replace`.
_Avoid_: monorepo module

**Sibling**:
Another project in this repo that a project requires. While the two are developed together, the
project's `go.mod` also replaces the sibling with the sibling's own directory; from its release
notes to its tag, the project requires every sibling at a tagged release and replaces none
(ADR-0006).
_Avoid_: local dependency

**Tools directory**:
`tools/` at the repo root: the repository's own tooling that is not part of the framework, such as
the Worker in `tools/vanity-worker` that serves the **Vanity path**s. It has no `go.mod`, is not a
project and is not released by tag; CI deploys it (ADR-0004).
_Avoid_: scripts, utilities

## Library vocabulary

These name what the `cli/core`, `cli/spec`, `update` and `daemon` projects build (ADR-0003). They describe the tools
a consumer writes with the library, not this repo; **Release** above is a version of a project here,
and a tool's own releases are reached through a **Release source**.

**App**:
The identity and shell of one tool, built in its `main` from the `cli/core` project: its binary name,
environment-variable prefix, version, root command, output mode and exit codes. The tool declares
the identity once and hands the same values to `update` and `daemon`.
_Avoid_: application, program, tool (the word for the consumer's whole product)

**Identity**:
The fixed values that name one tool: its binary name, the owner and repository its releases are
published from, its environment-variable prefix and its directory name. The tool declares them once
in its `main` and hands the same values to each library it assembles, so they cannot disagree.
_Avoid_: config (values a user supplies), name (one of its values), metadata

**Build info**:
What a tool's binary reports about its own build: its version, commit and build date, and the Go
toolchain, operating system and architecture it was built for. The `version` command prints it, and
its JSON form, versioned by a `schema` number, is a contract the `cli/core` project owns and
`update` reads (ADR-0007).
_Avoid_: version info (the version is one field of it), metadata, build metadata (a `+suffix` on a
version)

**Dev build**:
A binary whose version is not a release: anything but `vMAJOR.MINOR.PATCH`, optionally with a
prerelease, that is not a Go pseudo-version. An unknown version, a pseudo-version and a version
carrying build metadata are all dev builds (ADR-0007). `cli/spec/version` implements the rule (ADR-0010).
_Avoid_: debug build, snapshot, local build (a tagged release built locally is not a dev build)

**Env**:
The surroundings a command reads, passed in explicitly: environment variables, the standard
streams and the working directory. Code takes an `Env`, never `os.Getenv` or `os.Stdout`, so tests
substitute it. An `Env` also answers, per standard stream, whether that stream is a terminal;
anything that is not a terminal file, such as a buffer in a test, is not one. An `Env` also
delivers the interrupt signals the process receives, as a channel (`Interrupts`) that `cli/core`
fills from SIGINT and SIGTERM (ADR-0012). An `Env` does not
carry the platform directories: `cli/spec/paths` resolves them (ADR-0009).
_Avoid_: environment (also means a deployment such as staging), context (`context.Context`), config
(values loaded from a file or flags)

**Directory kind**:
One of the five directories a tool keeps its files in: config, state, cache, data and log.
`cli/spec/paths` resolves one directory for each kind from the **Identity**, the operating system's
layout and the environment (ADR-0009).
_Avoid_: directory type, location, path (a resolved directory is a path, the kind is what it is
for)

**Home**:
The single root under which every **Directory kind** is a subdirectory (`config`, `state`, `cache`,
`data`, `log`). It is set by the `Home` option or by the environment variable `<PREFIX>_HOME`, and
is used as given, without the directory name appended (ADR-0009).
_Avoid_: home directory (the user's own, `~`), root, base directory

**Configuration**:
The values a user supplies to a tool, read into one struct whose fields are its **Key**s and whose
initial values are the defaults. Each **Provider** is a layer above the defaults, and the value of a
**Key** is the one from the highest layer that sets it (ADR-0012).
_Avoid_: settings, options, **Identity** (values the tool fixes in its `main`)

**Provider**:
One layer of **Configuration**: a source of values for **Key**s, such as a YAML file, environment
variables or the **Flag**s a command line set. Providers are applied in order and a later one
overrides an earlier one (ADR-0012).
_Avoid_: source (also the **Release source**), loader, backend

**Key**:
The name of one value of a **Configuration**: a dotted path with one segment per level of nesting
(`server.max_retries`), in snake_case. Matching ignores case, `-` and `_` within a segment, so every
**Provider** may spell it in its own convention (ADR-0012).
_Avoid_: name, path (also a directory), property

**Origin**:
Where the value of a **Key** was read: a file and line, an environment variable, a **Flag**, or
`default` when no **Provider** set it. Error messages and the report of a loaded **Configuration**
name it (ADR-0012). The report lists, for each **Key**, its effective value and its Origin; an entry
of the report is a Source, a **Key**'s value together with its Origin, never a **Provider**.
_Avoid_: location, provenance

**Default file**:
A commented file holding every **Key** of a **Configuration** at its default value, written for the
user to edit. A tool creates it on first run or not at all, and an existing file is never replaced
(ADR-0012).
_Avoid_: template, sample config, example file

**Removed key**:
A **Key** a **Configuration** has dropped, paired with a hint telling the user what to write
instead. A **Provider** that finds one reports the hint in place of the generic unknown-key error
(ADR-0012).
_Avoid_: deprecated key, renamed key, legacy key

**Command**:
One named action of a tool (`version`, `update`), described as data in `cli/spec`: its name,
descriptions, **Flag**s, subcommands and what it does when run. Any project contributes Commands
and the **App** runs them; a Command names no CLI framework (ADR-0005).
_Avoid_: subcommand (a Command nested under another), handler, action

**Flag**:
One named option of a **Command**, with a type, a default and a usage string. A Command declares
its Flags and reads their values from the **Invocation** (ADR-0005).
_Avoid_: option, switch, argument (a positional value, not a Flag)

**Invocation**:
One execution of a **Command**: the Flag values parsed, the positional arguments and the **Env** it
was run with. A Command is reusable and an Invocation is not (ADR-0005).
_Avoid_: call, run, context (`context.Context`)

**Output mode**:
How one **Invocation** presents its output: more or less detail than the default, JSON or text, and
whether colour is allowed, which is decided for the standard output and the standard error
separately. It is decided for the Invocation from the standard flags the **App** takes and the
**Env**, and a **Command** reads it rather than parsing those flags itself.
_Avoid_: format (JSON or text is one part of it), verbosity, log level

**Message**:
A line a **Command** writes about the run itself, such as progress, a warning or an error it shows,
as opposed to what it produces. It goes to the standard error, styled by the **Output mode**, and
quiet silences it except for errors (ADR-0013).
_Avoid_: log, print

**Data**:
What a **Command** exists to produce: the text or JSON a user or a script reads from the standard
output. It is written as given, unstyled, and no **Output mode** suppresses it (ADR-0013).
_Avoid_: result, output (the **Output mode** covers both streams)

**Envelope**:
The JSON object a **Command** writes as **Data** in a JSON **Output mode**: a flat object whose first
key is the **Schema** and whose remaining keys are the Command's own (ADR-0014).
_Avoid_: wrapper, payload

**Schema**:
The integer first key of an **Envelope**, the version of the shape of its keys. Adding a key keeps
it; renaming, removing or retyping a key increments it (ADR-0007, ADR-0014).
_Avoid_: version (a key of its own in some payloads)

**Hook**:
A function the **App** runs before or after every **Command** it runs, given the **Invocation** and
the Command's path and **Annotation**s. It belongs to the tool, not to the Command, so a Command
shared by several tools carries none; an update check is one (ADR-0009).
_Avoid_: middleware, callback, interceptor

**Annotation**:
A key-value note on one **Command**, read by the **App** and its **Hook**s, never by the Command's
own run: one key exempts a Command from the update check. It applies to the Command that carries it,
not to its subcommands.
_Avoid_: tag (a **Prefixed tag** is a release), label, metadata

**Exit code**:
The number a tool's process ends with. 0 is success, 1 a general error, 2 a command line the tool
does not accept, 130 an interruption; a tool gives 3 through 125 to its own errors (ADR-0010). A run
function reaches 2 only by returning a `spec.UsageError`, never by a code it chooses (ADR-0015).
_Avoid_: status, return code, error code (a code inside an error's own text)

**Reported error**:
An error whose message a **Command** has already shown to the user, so the **App** ends the process
with its **Exit code** without printing it again (ADR-0010).
_Avoid_: silent error, handled error

**Error layer**:
One wrap's own text in an error chain: what a wrapping error adds in front of the error it wraps.
The **App** prints a top-level error as its layers, outermost first (ADR-0011).
_Avoid_: level, frame, cause (the error a layer wraps)

**Release source**:
Where a tool's own published releases come from, behind the `update` project's `Source` interface:
it lists releases and serves the assets of one. It does not choose among them; version comparison
(`cli/spec/version`) and asset selection do.
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

**Grace period**:
The single time budget a shutdown shares: the **Runners** are stopped one after another within it,
and a **Runner** that has not stopped when it ends is reported as timed out.
_Avoid_: timeout, deadline, shutdown timeout

**Service**:
A daemon registered with the operating system's service manager (systemd, launchd, the Windows
service control manager), started, stopped and restarted by it. Installing, removing and querying
a Service are the `daemoncmd` commands.
_Avoid_: daemon for the registration (the daemon is the program, the Service is its registration),
unit, agent, microservice
