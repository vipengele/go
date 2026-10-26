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
- `source.go` — `Source`, `Release` and `Asset`. A nil `Config.Source` resolves to the
  `GitHubSource` (`github.go`) for the Identity's Owner and Repo, using the resolved HTTP client.
- `github.go` — `GitHubSource`: reads the `releases/latest` redirect without following it, falls back
  to the REST API, and with `IncludePrereleases` reads the first page of the release list instead.
  Tags must be plain release versions; `WebURL`, `APIURL` and `Timeout` are overridable for tests.
- `select.go` — `Config.Select` names the archive and checksums assets of a `Release`: the
  `AssetTemplate` and `ChecksumsAsset` rendered with `AssetExt`. A non-nil `Assets` is matched by
  exact name; nil `Assets` (the redirect path, ADR-0019) is located through the `Source`'s optional
  `AssetLocator`, which `GitHubSource.AssetURL` implements.
- `asset/` — the template language (`{name}`, `{version}`, `{tag}`, `{os}`, `{arch}`, `{ext}`) with
  `Validate`, `ValidateChecksums` and `Render`, and `GoReleaserArchive` / `GoReleaserChecksum`, which
  translate a template into a GoReleaser `name_template`. GoReleaser's archive `format` must equal
  `Config.AssetExt`, or the published name and the one `Select` looks for differ.
- `errors.go` — `StatusError`, `TimeoutError` (a `net.Error`), `TagError`, `AssetNotFoundError`,
  `ChecksumMismatchError`, `ChecksumNotFoundError`, `ChecksumConflictError`, `SizeLimitError`,
  `ErrPrerelease` and `ErrAssetsNotListed`, the typed errors a source, `Select` or `Download`
  returns; `classifyError` maps deadline and timeout failures to `TimeoutError` and passes
  cancellation through unchanged.
- `download.go` — `Config.Download`, `DownloadOption` and `WithProgress`: the checksums request,
  the archive request, and the bounded, hashed copy to the temp file.
- `archive.go` — `Archive`, the verified temp file `Download` returns, and its lifecycle.
- `checksums.go` — `parseChecksum`, which finds the archive's digest in a checksums file.

## Download contract

- An asset from `Select` on an unlisted release has `Size` 0, which means unknown, not empty.
- The download step maps a 404 on a URL `Select` located to a `*AssetNotFoundError`, not a
  `*StatusError`: a missing asset on the redirect path surfaces there (ADR-0019). `Selection.Located`
  marks such a selection and `Selection.Tag` names the release; any other non-200 is a `*StatusError`.
- `Download` order (ADR-0020): `resolve` the `Config`; reject an empty URL on either asset (an
  unexported sentinel wrapping "update: asset has no URL", not matchable with `errors.Is`); fetch and
  parse the checksums file; stream the archive through a SHA-256 hash into the temp file; compare
  the decoded digests; rewind and return. A missing or conflicting entry fails before the archive
  is requested. The caller never holds the file before it is verified.
- `Archive` exposes `Read`, `ReadAt`, `Seek`, `Size`, `Name` and `Close` only. `Close` closes the
  file before removing it (Windows cannot remove an open file), joins both errors and is idempotent:
  a second call returns nil. Every failure path removes the temp file; a failed removal is joined
  onto the returned error.
- The temp file is `os.CreateTemp(TempDir, TempPrefix+"*")`, mode 0600. A missing `TempDir` is an
  error; nothing creates it.
- Caps: the archive `MaxDownloadBytes` (default 256 MiB), the checksums file 1 MiB, fixed. Both
  give a `*SizeLimitError`, up front from `Content-Length` or a non-zero `Asset.Size`, else while
  streaming. `DownloadTimeout` (default 5 min) is one deadline per request, headers and body; a
  shorter deadline on the context wins and surfaces as a `*TimeoutError`. Cancellation passes
  through unchanged.
- `Size` and `Content-Length` are hints: a body of another length is not an error. The digest
  decides, and a difference is a `*ChecksumMismatchError`.
- Checksums lines are `<64 hex><spaces or '*'><name>`: either hex case, LF or CRLF, a leading `./`
  dropped, spaces kept inside a name; other lines are skipped. The name is matched exactly and
  digests compared as bytes. No entry is a `*ChecksumNotFoundError`; two different digests are a
  `*ChecksumConflictError`.
- `WithProgress` runs synchronously on the caller's goroutine, archive only. `total` is
  `Content-Length`, else a non-zero `Asset.Size`, else -1. The last call, after verification, is
  `(written, written)`; none follows a failure.
- HTTPS is not enforced. Requests use the resolved `HTTPClient` and follow redirects (release
  assets redirect to a CDN), not the redirect-refusing client `GitHubSource` builds for itself.

## Conventions

- Reach the outside world only through a `Config` field, never through `http.DefaultClient`,
  `time.Now`, `runtime.GOOS`, `os.Getenv` and the like called directly. A new seam is a new
  `Config` field with a documented zero-value default, resolved in `resolve`.
- No package-level mutable state: `gochecknoglobals` is enabled (sentinel errors are allowed).
- `depguard` forbids importing `go.vipengele.org/cli/core/**` anywhere, and `github.com/spf13/cobra`
  outside an `updatecmd` package. The library depends on no command-line framework.
- `cli/spec` and `commons` are siblings reached by directory `replace`s in `go.mod`
  (`../cli/spec`, `../commons`); see the repo-root rule on sibling replaces.
