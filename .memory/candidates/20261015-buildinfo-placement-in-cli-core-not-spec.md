---
about: "buildinfo sits in cli/core by ADR-0007's contract-ownership choice; no ADR discusses moving it to cli/spec, and the move is not rejected explicitly"
saw:
  - docs/adr/0007-the-cli-project-owns-the-version-json-contract.md
  - docs/adr/0004-projects-nest-and-take-vanity-module-paths.md
  - docs/adr/0009-platform-paths-live-in-cli-spec-paths.md
  - source/cli/core/buildinfo/doc.go
  - source/cli/spec/go.mod
  - source/update/.golangci.yml
---

Findings (new, no target note):

- ADR-0007:5-8 puts the contract in `cli/core`'s `buildinfo` docs because `cli/core` holds the `version` command. The
  considered option at :54 rejects "a shared Go type in cli/spec" only for the JSON contract crossing a process
  boundary and adding a `cli/spec` require to update; it never discusses placing the package that *produces* the
  JSON (buildinfo) in cli/spec. Placement of buildinfo itself is not recorded as decided or rejected anywhere
  (grep buildinfo docs/adr: only 0007 and 0009-the-app-owns-version).
- cli/spec scope: ADR-0004 table "contracts a CLI tool and the libraries it assembles agree on"; ADR-0009:17-30 root
  package performs no I/O, paths subpackage does; paths is "no dependency beyond stdlib and cli/spec". cli/spec/go.mod
  requires only commons (replace ../../commons). buildinfo imports stdlib only (buildinfo.go:3-8, doc.go "depends on
  the standard library only"), so it would fit the spec dependency rule but does read runtime/debug.
- Importers of buildinfo: only cli/core (app.go:4,63,71; versioncmd/versioncmd.go:8,33; tests). update does not
  import it; update's depguard `core-stays-out` (source/update/.golangci.yml) denies `go.vipengele.org/cli/core/**`.
- Move cost: the `-X go.vipengele.org/cli/core/buildinfo.version=` ldflags path (buildinfo/doc.go) changes for every
  tool's build; testdata/ldflagsmain and ldflags_test.go pin it; cli/core go.mod already requires cli/spec.
- No release exists: `git tag` is empty, docs/release-notes holds only commons/v0.1.0.md, so moving is not a
  published-API break yet.
- A third-party dep (e.g. x/mod) in cli/spec would reach cli/core, update and daemon; no ADR permits third-party
  deps in cli/spec (ADR-0009 says "no dependency beyond stdlib and cli/spec" for paths only). No x/mod anywhere in
  the repo (grep).
