---
about: "ADR-0003 says update writes its own atomic writer and not cli's store, but ADR-0018 (later) names cli/state the home for update's state and leaves 'whether update requires it' to issue #54"
saw:
  - docs/adr/0003-the-library-is-three-projects-cli-update-and-daemon.md
  - docs/adr/0018-the-state-store-is-its-own-project-cli-state.md
  - docs/adr/0019-the-file-lock-is-its-own-project-filelock.md
  - docs/adr/0008-update-takes-its-identity-from-cli-spec.md
  - source/update/.golangci.yml
  - source/update/go.mod
  - source/cli/state/write.go
  - source/cli/state/load.go
  - source/cli/state/json.go
---

Findings for issue #54 (update state persistence), checked 2026-11-03.

- ADR-0003:69-70 (still unamended on this point): "`update` writes its state with its own atomic
  writer. It does not use `cli`'s store, so the two stores do not share a lock or a file format."
  "cli's store" there is the pre-split `cli` project; ADR-0018 later moved it to `cli/state`.
- ADR-0018:30-31: "Whether `update` does [require cli/state] is left to issue #54; this ADR does
  not change ADR-0005's limit of `update` to `cli/spec`." ADR-0018:44-45 rejects a store in
  cli/core because "update may not import it" and "update's state is the first consumer".
- ADR-0008:140 ("requires cli/spec and commons, and no other sibling") is the only hard limit, and it
  is a recorded decision that requiring cli/state would amend. depguard in
  source/update/.golangci.yml denies only `cli/core/**` and cobra outside updatecmd: not cli/state.
- cli/state (go.mod: only go.yaml.in/yaml/v3, no sibling): WriteFile(path, data, mode) =
  CreateTemp(".<base>.tmp-*" in dest dir) -> write -> close -> open by path -> chmod -> fsync ->
  rename -> fsync dir (EINVAL/ENOTSUP ignored) (write.go:65-157). LoadJSON[T] returns
  (T, LoadStatus, error); Missing nil err, Corrupt (incl. empty file) errors.Is ErrCorrupt,
  Unreadable otherwise (load.go:210-232, json.go:246). SaveJSON writes indent-2 + newline at 0600
  (json.go:255). No dir creation, no lock.
- Locking: cli/state takes none (ADR-0018:55-57). ADR-0019:67-70 and CONTEXT.md:287-290 say update
  uses filelock "to serialise its check and its replacement of the managed binary".
  filelock.New(path) Locker; Lock(ctx) (unlock func(), error) (filelock.go:19-24); requires only x/sys.
- No atomic helper in commons (only envvar, validate). The only in-tree atomic writers are
  cli/state, an unexported one in cli/spec/completioninstall/install.go:291-333 (mkdirAll 0755,
  no dir fsync), and update's own staging in extract.go/install.go.
- cli/spec/paths Ensure creates dirs at 0700 (ensure.go:12). Config.StateDir already resolves via
  paths.Resolve (update/config.go:75-80, 315-324).
- No recorded state file name or version field for update. Only JSON convention is the CLI output
  envelope `schema` int key (ADR-0014:1-13), which is output not state.
