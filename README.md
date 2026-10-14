# vipengele/go

The Go libraries of the vipengele platform: reusable modules for building Go services and
command-line tools. The repo holds **projects**, each a self-contained Go module under
`source/<project>/`, at any depth. A project is released alone, by its own tag
`source/<project>/vX.Y.Z`
([ADR-0001](docs/adr/0001-each-project-is-a-go-module-released-by-its-own-prefixed-tag.md)).

| Project | Module path |
|---------|-------------|
| [`source/cli/core`](source/cli/core) | `go.vipengele.org/cli/core` |
| [`source/cli/spec`](source/cli/spec) | `go.vipengele.org/cli/spec` |
| [`source/update`](source/update) | `go.vipengele.org/update` |
| [`source/daemon`](source/daemon) | `go.vipengele.org/daemon` |
| [`source/commons`](source/commons) | `go.vipengele.org/commons` |

A project's module path is `go.vipengele.org/<project>`, which needs Go 1.25 or later:

```sh
go get go.vipengele.org/<project>@latest
```

## Develop

Work happens inside a project's directory:

```sh
cd source/<project>
go build ./...
go test -race ./...
golangci-lint run
```

To work on a project and a sibling it requires together, require the sibling and replace it with
the sibling's own directory (`replace go.vipengele.org/commons => ../commons`), in the same pull
request as the sibling's change. The project's release drops the `replace` and requires the
sibling at its tag
([ADR-0006](docs/adr/0006-a-sibling-is-developed-by-a-directory-replace-and-released-dependency-first.md)).
CI builds each project with `GOWORK=off`; the root `go.work` lists every project for local
development.

## Add a project

1. `mkdir source/<project> && cd source/<project> && go mod init go.vipengele.org/<project>`
2. Add a component to [`.lydite/components.yml`](.lydite/components.yml), a `gomod` entry to
   [`.gt-repo.yaml`](.gt-repo.yaml), and a `use` line to `go.work`.
3. Run `gt repo sync`.
4. Deploy the `go.vipengele.org` Worker with the new `go.mod` before the project's first tag
   ([ADR-0004](docs/adr/0004-projects-nest-and-take-vanity-module-paths.md)).

## Licence

[MIT](LICENSE)
