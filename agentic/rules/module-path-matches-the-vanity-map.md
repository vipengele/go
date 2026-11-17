---
description: A project's module path is go.vipengele.org/<path>, its directory relative to source/, and its tag is source/<path>/vX.Y.Z.
---

# A module path matches the vanity map

The `module` line of `source/<path>/go.mod` is `go.vipengele.org/<path>`, where `<path>` is the
project's directory relative to `source/` (ADR-0004). That is the path the Worker in
`tools/vanity-worker` serves: its `go-import` answer names `source/<path>` as the module's
subdirectory in `github.com/vipengele/go`, and the Go command then reads the module from there and
its versions from the tags `source/<path>/vX.Y.Z`. The tag is the repository directory, `source/`
included, even though the module path is not; any other module line makes every `go get` of the
module fail, and only after the tag is created. `verify-sibling-deps` fails a module path that is not
`go.vipengele.org/<path>`.

The Worker reads its module list from an R2 object that `publish-vanity-modules.yml` writes from
`source/**/go.mod` when a `go.mod` merges to `main`, so a new project resolves once that workflow
has run, with no Worker deploy. The release validation retries
`https://go.vipengele.org/<path>?go-get=1` for up to 5 minutes before it tags, so a project added
together with its notes waits for that workflow; when the Worker still does not answer, the release
fails and a later push to `main` retries it.

## Applies to

- `go mod init` in a new project: pass the full vanity path, `go mod init go.vipengele.org/<path>`.
- A rename or move of a project directory: the module line, every import of the module, the tag
  prefix and the Worker's entry all change together. A tag already created cannot be moved, so a
  renamed or moved project starts a new version history, and tags under the old directory stay
  where they are.
- A major version `v2` or above: the module path gains a `/v2` suffix after the path
  (`go.vipengele.org/<path>/v2`) and the tag stays `source/<path>/v2.0.0`.

## Example

`source/cli/core/go.mod`, released by the tag `source/cli/core/v1.2.3`:

```
module go.vipengele.org/cli/core
```
