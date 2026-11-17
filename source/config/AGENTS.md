# config

`go.vipengele.org/config`: a layered, strict, provider-based configuration loader. Read `doc.go`
first.

## Commands (run from `source/config`)

```bash
go build ./...
go vet ./...
go test -race -shuffle=on ./...
golangci-lint run
```

## Supported types

A key holds a bool, an integer, a float, a string, a `time.Duration`, or a slice of one of these. It
may also hold a collection: a slice of structs, or a `map[string]T` where T is any of those, a
struct, or a nested map. A pointer, an embedded field, a non-string map key, a slice of maps and any
other type is `ErrSchema`. `doc.go` and ADR-0037 describe the behaviour.

## Collection invariants

- A map key is never passed through `matchKey` or `byKey`, which lowercase and strip `-` and `_`;
  a map key is verbatim.
- A provider emits one `Entry` per collection field per layer, its value in `Entry.Node`. That is
  the layer boundary merging needs, so never split a collection into one entry per leaf.
- `merge` tags: none replaces the collection whole; `merge:"<key>"` on a slice of structs matches
  elements on a string field; `merge:"keys"` on a map merges one level deep. A tag is allowed only
  on a field outside collection elements.
- Collection fields, and every field of their elements, have no Env or flag name (`""`); the
  collision check skips `""`.
- Paths display as `profiles[2].env["FOO"]` and are formatted and parsed only by the helper in
  `key.go`.
- A file is strict inside struct values and accepts any key beneath a map.

## Conventions

- No package-level mutable state: `gochecknoglobals` is enabled (sentinel errors are allowed).
