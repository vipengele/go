# The `cli` project owns the `version --json` contract

Status: amends ADR-0003.

Every tool built on the library ships the `version` command from `cli/core`, the project that holds
the app shell ADR-0003 assigns to `cli` (ADR-0004). What that command prints is a contract between
processes: `update`'s staged-binary preflight runs `<staged> version --json` and reads the result.
`cli/core` defines and documents that contract, in the documentation of its `buildinfo` package, and
`update` reads the shape `cli/core` defines.

## The JSON shape

`version --json` writes one object on one line, every key always present (wrapped here):

```json
{"schema":1,"version":"v1.0.0","commit":"0123abcdef0123abcdef0123abcdef0123abcdef",
 "date":"2026-01-02T03:04:05Z","go":"go1.27.2","os":"linux","arch":"amd64","dev":false}
```

`commit` is the full SHA, `date` is passed through as it was supplied, and every string value is
`""` when nothing supplies it. The shape is versioned by `schema`, which is `1`. Adding a key is a
compatible change and leaves `schema` as it is; renaming, removing or retyping a key increments
`schema`. A reader that ignores unknown keys keeps working across every compatible change, and one
that checks `schema` knows when it must not trust the keys it expects.

## Dev builds

`dev` is false only for a release version: `vMAJOR.MINOR.PATCH`, optionally with a prerelease
(`v1.2.3`, `v1.2.3-rc.1`), that is not a Go pseudo-version. Every other version is a dev build: an
empty version, `(devel)`, a version carrying build metadata such as `v1.2.3+dirty`, and every
pseudo-version. A pseudo-version is what `go install` of an untagged commit produces, and build
metadata such as `+dirty` marks a build that differs from the version it names, so neither is a
release a self-update can compare against.

The rule is implemented by `go.vipengele.org/cli/spec/version`, which `buildinfo` calls and which
applies it to the latest version a release source reports as well (ADR-0010).

## The text form

Without `--json` the command prints one line, and that line is the whole text contract:

```
v1.0.0 build 0123abcdef01
```

The version, then ` build ` and the commit shortened to 12 characters. With no commit the clause is
omitted (`v1.0.0`); with no version the line reads `dev`. A pseudo-version is printed as it is. The
text form is for people; a program reads the JSON form.

## Considered options

- **`update` defines and documents the contract, as ADR-0003 states it.** `update` only reads the
  output; `cli/core` builds the command and the binary every tool ships. A contract owned by the
  project that does not emit it is documented away from the code that has to honour it, so a change
  to the command is reviewed without the definition beside it, and the project that owns the
  contract has no control over the output that has to match it.
- **A shared Go type in `cli/spec` that both `cli/core` and `update` import.** The contract
  crosses a process boundary as JSON: the preflight reads the output of a different binary, built
  from whatever version of `cli/core` that binary was built with. A shared type couples `update` to
  a Go type that says nothing about the bytes it actually receives, and it puts a `require` of
  `cli/spec` into `update` for a structure the documented JSON already describes. The documented
  shape and its `schema` field are the interface.

## What this changes in earlier ADRs

- **ADR-0003.** The JSON shape the preflight reads is defined and documented by `cli/core`, not by
  `update`. The rest of ADR-0003's consequence holds: the two projects agree through a documented
  contract, not through a shared type, and `update` requires no sibling for it.

## Consequences

- **A key change in `buildinfo` is a contract change.** Renaming, removing or retyping a key is
  released with `schema` incremented, and `update`'s preflight has to handle both values for as long
  as binaries emitting the lower `schema` are in the field.
- **`update` keeps no copy of the shape's definition.** It decodes the keys it needs and checks
  `schema`; the documentation it follows is `cli/core`'s.
