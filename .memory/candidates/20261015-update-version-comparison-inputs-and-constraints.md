---
about: "update's version comparison: no existing implementation; dev-build definition is fixed by ADR-0007; ADR-0003's Config sketch (Version field, 'dev') is stale"
saw:
  - docs/adr/0003-the-library-is-three-projects-cli-update-and-daemon.md
  - docs/adr/0007-the-cli-project-owns-the-version-json-contract.md
  - source/cli/core/buildinfo/buildinfo.go
  - source/update/config.go
  - source/update/source.go
  - source/update/go.mod
  - source/update/.golangci.yml
  - CONTEXT.md
---

Findings from planning issue #47 (version comparison in source/update):

- No version comparison exists in the repo: `grep -rniE "semver|x/mod|CompareVersion|IsNewer"` hits only
  buildinfo (dev detection by regex, not ordering), ADRs 0003/0007 and CONTEXT.md. No go.mod in
  source/ requires golang.org/x/*. Lydite's update.go (the issue's prior art) is not in this repo.
- ADR-0003 assigns "version comparison" to `update` and says `Release source ... does not choose among
  them; version comparison and asset selection do` (CONTEXT.md, Release source).
- Dev build is defined in ADR-0007 and CONTEXT.md "Dev build", implemented in
  source/cli/core/buildinfo/buildinfo.go:93-97 (releaseVersion regex + pseudoVersionTail regex at :32):
  empty, `(devel)`, build metadata (`v1.2.3+dirty`), any pseudo-version, anything not vMAJOR.MINOR.PATCH[-pre].
  update must not import cli/core (depguard, source/update/.golangci.yml), so it cannot reuse that
  function; ADR-0007 says update "keeps no copy of the shape's definition".
- source/update/config.go has NO Version field (fields: BinaryName, TempPrefix, AssetTemplate,
  ChecksumsAsset, StateDir, Source, HTTPClient, Now, GOOS, GOARCH, Executable, Getenv, UserHomeDir);
  ADR-0003's Config sketch with `Version string // "dev"` predates ADR-0008 and is not the current code.
  Release.Tag (source/update/source.go) is "as the source publishes it, such as v1.2.3".
- Third-party deps: no rule forbids them in update. Only constraints: ADR-0003 (cobra only in updatecmd),
  ADR-0008 (siblings: cli/spec and commons only), ADR-0009 rejects adrg/xdg as a dependency for a few
  path rules (precedent of reluctance, not a rule). cli/core already requires cobra/pflag.
- source/update is in development mode: docs/release-notes holds only commons/v0.1.0.md, none for update.
