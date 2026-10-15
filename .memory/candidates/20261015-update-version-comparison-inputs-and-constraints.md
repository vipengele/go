---
about: "the release rule and 'newer' are defined once in cli/spec/version (ADR-0010); update has no caller of it yet and does not require golang.org/x/mod until it imports the package"
saw:
  - docs/adr/0010-version-comparison-lives-in-cli-spec-version.md
  - docs/adr/0007-the-cli-project-owns-the-version-json-contract.md
  - source/cli/spec/version/version.go
  - source/cli/spec/version/doc.go
  - source/update/go.mod
  - source/update/source.go
---

- `version.IsRelease(v)` is `semver.IsValid(v) && semver.Canonical(v) == v && !module.IsPseudoVersion(v)`
  (version.go); `Newer(current, latest)` is false for a non-release `latest`, true for a non-release
  `current` and a release `latest`, else `semver.Compare(latest, current) > 0`. Dev builds are older
  than every release by that explicit rule.
- Pure ordering: `Newer("v1.2.3", "v1.3.0-rc.1")` is true, so offering a prerelease to a stable user
  is the caller's decision. A Source tag without the leading `v` is not a release, so `Newer` is
  false for it and indistinguishable from "up to date" (doc.go).
- `source/update` has no import of `cli/spec/version` and its go.mod lists no `golang.org/x/mod`;
  `Release.Tag` (source.go) is "as the source publishes it, such as v1.2.3". The check story that
  calls `Newer` is not written.
- `source/update/config.go` has no Version field; ADR-0003's Config sketch (`Version string // "dev"`)
  is not the code.
