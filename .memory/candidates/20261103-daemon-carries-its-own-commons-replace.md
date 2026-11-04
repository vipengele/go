---
about: "A project that requires cli/spec needs its own indirect require and directory replace of commons, because cli/spec's replace is ignored by the dependent's build"
saw:
  - source/daemon/go.mod
  - source/cli/spec/go.mod
  - source/cli/core/go.mod
  - docs/adr/0023-a-daemons-single-instance-is-a-filelock-try-lock-with-a-holder-record.md
---

- `source/cli/spec/go.mod` reaches `go.vipengele.org/commons` through its own `replace => ../../commons`.
  Go honours `replace` directives only in the main module, so when `source/daemon` builds with
  `GOWORK=off` the cli/spec replace has no effect and commons would be looked up at its required
  version, which has no tag.
- So `source/daemon/go.mod` carries `go.vipengele.org/commons v0.0.0 // indirect` and
  `replace go.vipengele.org/commons => ../commons` beside its `filelock` and `cli/spec` replaces,
  the same shape as `source/cli/core/go.mod` (ADR-0023, ADR-0006).
- commons is in strict mode (it has `docs/release-notes/commons/v0.1.0.md` and no tag), but strict
  mode constrains only commons' own go.mod, so a dependent's replace of commons is accepted.
