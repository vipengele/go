---
about: "the cli version command's --json shape is a contract owned by cli/core (ADR-0007, amending ADR-0003) and ADR-0005 (command abstraction) is referenced but does not exist"
saw:
  - docs/adr/0003-the-library-is-three-projects-cli-update-and-daemon.md
  - docs/adr/0004-projects-nest-and-take-vanity-module-paths.md
  - docs/adr/0007-the-cli-project-owns-the-version-json-contract.md
  - source/cli/core/buildinfo/doc.go
  - source/cli/core/.golangci.yml
---

ADR-0003's staged-binary preflight runs `<staged> version --json`. ADR-0007 makes cli/core define and
document that shape, in the package documentation of `source/cli/core/buildinfo/doc.go`, versioned by
a `schema` field (1; adding a key keeps it, renaming, removing or retyping a key increments it);
`update` only reads it. ADR-0003 carries the amendment in its Status line and an inline pointer.

ADR-0004 ("Out of scope: the command abstraction") defers a framework-neutral `Command` in cli/spec
to ADR-0005; `ls docs/adr` shows no 0005, so the number is reserved, cobra-in-cli (ADR-0003) stands
and the abstraction is undecided. The next free ADR number is therefore 0007 and above.

`source/cli/core/.golangci.yml` enables `gochecknoglobals` and a `depguard` rule that denies
`github.com/spf13/cobra` outside `versioncmd`. That is narrower than ADR-0003 allows (root, wiring,
completion); widening it belongs with the root command builder.
