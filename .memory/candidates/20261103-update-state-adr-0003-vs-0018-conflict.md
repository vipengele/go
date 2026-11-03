---
about: "source/update persists its State through the sibling cli/state (ADR-0023, amending ADR-0003/ADR-0008); the file is indented JSON because cli/state.SaveJSON always indents"
saw:
  - docs/adr/0023-update-keeps-its-check-state-through-cli-state.md
  - source/update/state.go
  - source/update/go.mod
  - source/cli/state/json.go
  - source/cli/state/load.go
---
- `update` requires `go.vipengele.org/cli/state` with a directory replace `../cli/state`
  (go.mod); ADR-0023 records why it is not a private writer.
- `fileStore` keeps `<StateDir>/update-state.json` (state.go `stateFileName`). Save creates the
  dir 0700 itself (`os.MkdirAll`), since `cli/state.SaveJSON` requires the directory to exist;
  Load creates nothing. Missing and Corrupt (empty file included) load as the zero State with a
  nil error; Unreadable is an error (state.go `Load`, cli/state load.go `load`).
- `cli/state.SaveJSON` always writes `json.MarshalIndent(v, "", "  ")` plus a trailing newline
  at 0600 (cli/state json.go), so the state file is indented and a zero State is `{}\n`; tests
  asserting exact bytes must expect that.
- No lock is taken around Save; concurrent saves replace the file whole and the last rename wins.
