# vipengele/go

The Go libraries of the vipengele platform: reusable modules for building Go services and
command-line tools. The repo holds **projects**, each a self-contained Go module under
`source/<project>/`, at any depth. A project is released alone, by its own tag
`source/<project>/vX.Y.Z`
([ADR-0001](docs/adr/0001-each-project-is-a-go-module-released-by-its-own-prefixed-tag.md)),
which the workflow creates when the pull request adding the project's release notes merges
([ADR-0037](docs/adr/0037-a-release-is-the-merge-of-its-notes.md)).

| Project | Module path |
|---------|-------------|
| [`source/cli/core`](source/cli/core) | `go.vipengele.org/cli/core` |
| [`source/cli/spec`](source/cli/spec) | `go.vipengele.org/cli/spec` |
| [`source/update`](source/update) | `go.vipengele.org/update` |
| [`source/daemon`](source/daemon) | `go.vipengele.org/daemon` |
| [`source/commons`](source/commons) | `go.vipengele.org/commons` |
| [`source/config`](source/config) | `go.vipengele.org/config` |
| [`source/cli/state`](source/cli/state) | `go.vipengele.org/cli/state` |
| [`source/filelock`](source/filelock) | `go.vipengele.org/filelock` |
| [`source/examples/greet`](source/examples/greet) | `go.vipengele.org/examples/greet`, an example tool the [consumer guide](docs/guide/consumer.md) quotes; never tagged, not for `go get` |

A project's module path is `go.vipengele.org/<project>`, which needs Go 1.27 or later:

```sh
go get go.vipengele.org/<project>@latest
```

To build a command-line tool on `cli/core`, `cli/spec`, `config` and `update`, from its first
command to its signed releases, follow the [consumer guide](docs/guide/consumer.md).

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
request as the sibling's change. A pull request that adds the notes of both releases the chain: the
dependent requires the sibling at its pending version and keeps the `replace`, and its tag points to
a release commit that drops it
([ADR-0006](docs/adr/0006-a-sibling-is-developed-by-a-directory-replace-and-released-dependency-first.md),
[ADR-0038](docs/adr/0038-a-dependent-is-released-from-a-release-commit.md)).
CI builds each project with `GOWORK=off`; the root `go.work` lists every project for local
development.

## Add a project

1. `mkdir source/<project> && cd source/<project> && go mod init go.vipengele.org/<project>`
2. Add a component to [`.lydite/components.yml`](.lydite/components.yml), a `gomod` entry to
   [`.gt-repo.yaml`](.gt-repo.yaml), and a `use` line to `go.work`.
3. Run `gt repo sync`.
4. Merge the new `go.mod`: `publish-vanity-modules.yml` adds the project to the `go.vipengele.org`
   Worker's module list, and a release waits for the Worker to answer
   ([ADR-0004](docs/adr/0004-projects-nest-and-take-vanity-module-paths.md)).

## Licence

[MIT](LICENSE)
