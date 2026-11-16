# greet

`go.vipengele.org/examples/greet`: an example tool built on the released `cli/core`, `cli/spec`,
`config` and `update` projects, quoted by `docs/guide/consumer.md`. Read `doc.go` first. It is never
tagged and has no release notes.

## Commands (run from `source/examples/greet`)

```bash
go build -o /dev/null ./...   # a main package: a plain go build writes ./greet into the tree
go vet ./...
go test -race -shuffle=on ./...
go test -run TestReleaseGoreleaserConfig -update   # re-renders .goreleaser.yaml
golangci-lint run
```

## Conventions

- Siblings are required at their tags, never with a `replace`: the project builds against what a
  consumer's `go get` fetches.
- Regions between `snippet:start <name>` and `snippet:end` comments are excerpts the guide quotes.
  `TestGuide` fails when the guide and the source differ; names are unique within a file.
- A new release of `update` moves together: the `go.mod` requires, the commit SHA and release comment
  in `release.yml`, `.goreleaser.yaml`, and every `@vX.Y.Z` in `docs/guide/consumer.md`. The tests
  fail until all name the same release.
- No package-level mutable state; register flags in `init` and read them back by name.
