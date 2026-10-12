# vipengele/go

The Go libraries of the vipengele platform: reusable modules for building Go services and
command-line tools. The repo holds **projects**, each a self-contained Go module under
`source/<project>/`. A project is released alone, by its own tag `source/<project>/vX.Y.Z`
([ADR-0001](docs/adr/0001-each-project-is-a-go-module-released-by-its-own-prefixed-tag.md)).

| Project | Module path |
|---------|-------------|
| [`source/cli/core`](source/cli/core) | `github.com/vipengele/go/source/cli/core` |
| [`source/update`](source/update) | `github.com/vipengele/go/source/update` |
| [`source/daemon`](source/daemon) | `github.com/vipengele/go/source/daemon` |

A project's module path is `github.com/vipengele/go/source/<project>`:

```sh
go get github.com/vipengele/go/source/<project>@latest
```

## Develop

Work happens inside a project's directory:

```sh
cd source/<project>
go build ./...
go test -race ./...
golangci-lint run
```

To work on a project and a sibling it requires together, list both in the root `go.work`
([ADR-0002](docs/adr/0002-a-cross-project-dependency-is-a-released-version-linked-locally-by-go-work.md)).
CI builds each project with `GOWORK=off`.

## Add a project

1. `mkdir source/<project> && cd source/<project> && go mod init github.com/vipengele/go/source/<project>`
2. Add a component to [`.lydite/components.yml`](.lydite/components.yml), a `gomod` entry to
   [`.gt-repo.yaml`](.gt-repo.yaml), and a `use` line to `go.work`.
3. Run `gt repo sync`.

## Licence

[MIT](LICENSE)
