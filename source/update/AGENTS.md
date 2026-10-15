# update

`go.vipengele.org/update`: checks for and applies updates to a command-line tool. Read `doc.go` first,
then `docs/adr/0008-update-takes-its-identity-from-cli-spec.md` at the repo root.

## Commands (run from `source/update`)

```bash
go build ./...
go vet ./...
go test -race -shuffle=on ./...
golangci-lint run
```

## Structure

- `config.go` — `Config`: embeds `spec.Identity` (from `go.vipengele.org/cli/spec`) and carries every
  seam to the outside world (HTTP client, clock, GOOS/GOARCH, executable, env lookup, user home
  dir). `Validate` reports problems; `resolve` returns a copy with each zero field set to its
  production default and never mutates the receiver. The default state dir comes from
  `paths.Resolve(..., paths.State)` in `go.vipengele.org/cli/spec/paths`, resolved through the GOOS,
  env lookup and user home dir seams; only the state kind is resolved, so the home directory is
  needed only when the state directory itself falls back to it.
- `source.go` — `Source`, `Release` and `Asset`. The library ships no `Source` implementation; a nil
  `Source` is the one thing a zero `Config` cannot default (`ErrNoSource`).

## Conventions

- Reach the outside world only through a `Config` field, never through `http.DefaultClient`,
  `time.Now`, `runtime.GOOS`, `os.Getenv` and the like called directly. A new seam is a new
  `Config` field with a documented zero-value default, resolved in `resolve`.
- No package-level mutable state: `gochecknoglobals` is enabled (sentinel errors are allowed).
- `depguard` forbids importing `go.vipengele.org/cli/core/**` anywhere, and `github.com/spf13/cobra`
  outside an `updatecmd` package. The library depends on no command-line framework.
- `cli/spec` and `commons` are siblings reached by directory `replace`s in `go.mod`
  (`../cli/spec`, `../commons`); see the repo-root rule on sibling replaces.
