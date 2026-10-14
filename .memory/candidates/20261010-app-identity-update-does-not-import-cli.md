---
about: "ADR-0003 forbids update importing cli, so update.Config duplicates EnvPrefix/Name and the tool wires both in main; ADR-0004 puts Identity in cli/spec and ADR-0005 lets update require cli/spec"
saw:
  - docs/adr/0003-the-library-is-three-projects-cli-update-and-daemon.md
  - docs/adr/0004-projects-nest-and-take-vanity-module-paths.md
  - CONTEXT.md
  - source/cli/core/doc.go
---

Read from ADR-0003 (lines 13-60, 89-93): `cli` and `update` require no sibling; `daemon` requires both.
`update` is configured by its own `update.Config{Name, EnvPrefix, Version, Source, StateDir, Token}`
and "`cli` has no type that `update` names". The tool builds `App` and `update.Config` from the same
values in its `main` and passes app's resolved state dir as `StateDir`. "A fourth project for the
shared identity" was rejected ("the identity is two strings"). The rule that `update` requires no sibling is superseded by ADR-0005; the duplication of EnvPrefix/Name in `update.Config` is not.

ADR-0004 (sections "`Identity` lives in `cli/spec`" and "Out of scope: the command abstraction")
places `Identity` (name, owner, repo, env prefix, directory name) in `cli/spec`, a project that
exists for the CLI contracts as a whole, and keeps generic code in `commons`. ADR-0005 then allows `update` to require `cli/spec` (to contribute commands); whether it also takes
`Identity` from it is still undecided. `App` stays in `cli/core`.

Consequences for issue #21: the identity type is `cli/spec`'s, not `cli/core`'s. The
`<PREFIX>_NO_UPDATE_CHECK` name is derived in update, `<PREFIX>_NO_COLOR` in cli, from separate
EnvPrefix strings until `update` takes `Identity` from `cli/spec`.

Not specified anywhere (grep of docs/, CONTEXT.md for XDG, EnvPrefix, App, state dir): OS directory
rules (only "platform paths" and "platform default"), and any glossary term for App/Tool/Prefix.
`source/cli` is a directory with no go.mod.
