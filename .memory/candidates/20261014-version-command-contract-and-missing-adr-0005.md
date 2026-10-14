---
about: "the cli version command's --json shape is a contract defined by update (ADR-0003), ADR-0005 (command abstraction) is referenced but does not exist, and cli/core has no .golangci.yml yet"
saw:
  - docs/adr/0003-the-library-is-three-projects-cli-update-and-daemon.md
  - docs/adr/0004-projects-nest-and-take-vanity-module-paths.md
  - source/cli/core
  - source/cli/spec/.golangci.yml
  - source/commons/.golangci.yml
---

ADR-0003 line 62-64: the staged-binary preflight runs `<staged> version --json`; the JSON shape is
"defined and documented by `update`", `cli`'s version command is "written to emit it"; projects agree
through the documented contract, not a shared type. ADR-0003 line 44 says Version is "dev" for a local build.
So the version command (cli/core, ADR-0003 line 13, 71) cannot freely choose its JSON shape.

ADR-0004 ("Out of scope: the command abstraction") defers a framework-neutral `Command` in cli/spec
to ADR-0005; `ls docs/adr` shows no 0005, so cobra-in-cli (ADR-0003 line 66-76) stands and the
abstraction is undecided. ADR-0003 says each project's .golangci.yml carries a depguard rule denying
cobra outside its command package; `ls -a source/cli/core` shows only doc.go and go.mod, so neither
that rule nor gochecknoglobals (enabled in cli/spec and commons .golangci.yml) exists for cli/core yet.
grep of docs, CONTEXT.md, .github, .lydite, agentic for ldflags/buildinfo/ReadBuildInfo/IsDev: 0 hits.
