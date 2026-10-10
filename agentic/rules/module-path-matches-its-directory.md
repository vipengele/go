---
description: A project's module path is github.com/vipengele/go/source/<project>, exactly its directory under the repo.
---

# A module path matches its directory

The `module` line of `source/<project>/go.mod` is `github.com/vipengele/go/source/<project>`. The
Go module proxy finds a nested module by the tag `source/<project>/vX.Y.Z` and then expects the
module path to end with that same directory; any other path makes every `go get` of the module
fail, and only after the tag is pushed (ADR-0001).

## Applies to

- `go mod init` in a new project: pass the full path.
- A rename or move of a project directory: the module line, every import of the module and the tag
  prefix all change together. A tag already pushed cannot be moved, so a renamed project starts a
  new version history.
- A major version `v2` or above: the module path gains a `/v2` suffix after the directory
  (`github.com/vipengele/go/source/<project>/v2`) and the tag stays `source/<project>/v2.0.0`.

## Example

```
module github.com/vipengele/go/source/cli
```
