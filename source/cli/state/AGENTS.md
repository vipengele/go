# cli/state

`go.vipengele.org/cli/state`: atomic file replacement and typed JSON and YAML load and save helpers
for the small state files a tool keeps beside its configuration. Read `doc.go` first, then
`docs/adr/0018-the-state-store-is-its-own-project-cli-state.md` at the repo root. It requires no
sibling project; `cli/core` requires it.

## Commands (run from `source/cli/state`)

```bash
go build ./...
go vet ./...
go test -race -shuffle=on ./...
golangci-lint run
```

## Structure

- `WriteFile(path, data, mode)` and `Replace(tmp, final, mode)` (`write.go`) — temp file in the
  destination's directory, chmod, sync, rename, directory sync. `Replace` opens `tmp` by path when
  called, so it chmods and syncs whatever inode the path names then, not a handle the caller held.
  The temp file is removed on every failure the package reports; a killed process can leave
  `.<base>.tmp-*` behind and nothing sweeps it.
- `steps` (`write.go`) — one function field per filesystem operation, in write order. Tests swap a
  field to make that step fail and check the temp file and the destination afterwards
  (`export_test.go`, `write_test.go`); `write_linux_test.go` holds the Linux-only cases.
- `LoadJSON[T]`, `LoadYAML[T]` (`json.go`, `yaml.go`, shared `load` in `load.go`) — return the
  value, a `LoadStatus` (`Missing`, `Loaded`, `Corrupt`, `Unreadable`) and an error. Anything but
  `Loaded` returns the zero `T`. `Missing` has a nil error; `Corrupt` matches `ErrCorrupt` with
  `errors.Is`, an empty file included; `Unreadable` does not. YAML must be exactly one document.
- `SaveJSON[T]`, `SaveYAML[T]` — marshal, then `WriteFile` at 0600. An encode failure leaves the
  file untouched; `SaveYAML` turns the encoder's panic on unencodable types into an error.

## Gotchas

- The package takes paths and nothing more: it resolves no directory, creates none and takes no
  lock. Writing into a missing directory is an error.
- Rename is atomic only within one filesystem, so the temp file must stay beside the destination.
- The only dependency is the YAML library; keep it that way, since every dependent of this project
  inherits it.
