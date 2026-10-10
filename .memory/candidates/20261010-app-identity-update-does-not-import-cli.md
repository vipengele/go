---
about: "ADR-0003 forbids update importing cli, so the cli App identity type is never shared with update; update.Config duplicates EnvPrefix/Name and the tool wires both in main"
saw:
  - docs/adr/0003-the-library-is-three-projects-cli-update-and-daemon.md
  - CONTEXT.md
  - source/cli/doc.go
---

Read from ADR-0003 (lines 13-60, 89-93): `cli` and `update` require no sibling; `daemon` requires both.
`update` is configured by its own `update.Config{Name, EnvPrefix, Version, Source, StateDir, Token}`
and "`cli` has no type that `update` names". The tool builds `App` and `update.Config` from the same
values in its `main` and passes app's resolved state dir as `StateDir`. "A fourth project for the
shared identity" was rejected ("the identity is two strings").

Consequences for issue #21 (app.App): it need not be importable by update. The `<PREFIX>_NO_UPDATE_CHECK`
name is derived in update, `<PREFIX>_NO_COLOR` in cli, from separate EnvPrefix strings.

Not specified anywhere (grep of docs/, CONTEXT.md for XDG, EnvPrefix, App, state dir): the package path
of `App` (ADR table only says cli owns "`App`"; the call `app.AddCommand(...)` shows a value named app),
OS directory rules (only "platform paths" and "platform default"), and any glossary term for App/Tool/Prefix.
source/cli currently holds only doc.go and go.mod (no requires).
