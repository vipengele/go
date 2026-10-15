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

## Conventions

- No package-level mutable state: `gochecknoglobals` is enabled (sentinel errors are allowed).
