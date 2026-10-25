---
about: "update's default GitHub source returns nil Assets on the redirect path, so Config.Select locates assets through the optional AssetLocator instead of listing them, and a missing asset surfaces at download"
saw:
  - source/update/github.go
  - source/update/source.go
  - source/update/select.go
  - source/update/config.go
  - docs/adr/0016-the-default-update-source-reads-the-github-latest-redirect-then-the-api.md
  - docs/adr/0019-a-missing-asset-on-the-redirect-path-surfaces-at-download.md
  - source/update/AGENTS.md
---

- The redirect path returns tag + page URL with nil Assets ("the assets are not listed"), `GitHubSource.latestStable` in github.go and ADR-0016's rule "Release.Assets == nil means the assets are not listed". The API path and the IncludePrereleases path always return a non-nil Assets, empty when the release has none.
- `Config.Select` (select.go) renders `AssetTemplate` and `ChecksumsAsset` and matches a non-nil Assets by exact name; an empty non-nil slice is a miss, not "unlisted". A nil Assets goes through the resolved Source's optional `AssetLocator` (`GitHubSource.AssetURL`), giving assets of Size 0 ("unknown"); a Source without `AssetURL` gives `ErrAssetsNotListed`. Why no REST listing or HEAD probe: ADR-0019.
- Because nothing checks the asset on that path, the download step must map a 404 on a located URL to `*AssetNotFoundError` (errors.go); Select cannot. The contract is in source/update/AGENTS.md "Download contract".
- update is untagged and has no docs/release-notes/update/, so it is not in strict mode; no other project requires go.vipengele.org/update. No goreleaser tooling lives in the repo (release.yml tags and announces modules only), so `asset.GoReleaserArchive` serves consuming tools.
- Conventions: every outside-world reach is a Config field resolved in `resolve` (AGENTS.md); no mutable package globals (gochecknoglobals; AGENTS.md), so the asset placeholder tables are switch functions; depguard bans cli/core and cobra outside updatecmd (AGENTS.md).
