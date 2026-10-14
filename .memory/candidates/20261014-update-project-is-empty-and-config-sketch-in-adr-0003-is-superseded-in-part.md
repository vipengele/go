---
about: "source/update is an empty scaffold (doc.go + go.mod, no .golangci.yml, no tests); ADR-0003's update.Config sketch is amended by ADR-0004/0005-pending, and no repo code uses t.Parallel or injectable seams yet"
saw:
  - source/update/doc.go
  - source/update/go.mod
  - source/commons/.golangci.yml
  - source/cli/core/.golangci.yml
  - docs/adr/0003-the-library-is-three-projects-cli-update-and-daemon.md
  - docs/adr/0004-projects-nest-and-take-vanity-module-paths.md
  - .lydite/components.yml
  - .gt-repo.yaml
  - go.work
---

Found while planning issue #45 (update Config and seams).

- `source/update` holds only `doc.go` and `go.mod` (module `go.vipengele.org/update`, `go 1.27.2`, no requires). No `.golangci.yml`, so golangci-lint defaults apply, and `gochecknoglobals` (enabled in `source/commons/.golangci.yml`) is NOT active there unless the file is added. Only `cli/core` adds depguard (cobra confined to `versioncmd`); ADR-0003 says every project's `.golangci.yml` carries a cobra depguard rule outside its command package.
- `update` is already in `go.work`, `.lydite/components.yml:24-27` (go-test, `-race ./...`, no explicit coverage threshold in repo; the lydite stage gates coverage by baseline comparison), and `.gt-repo.yaml:24-25`. Nothing to register.
- `grep -rn Parallel source --include=*_test.go` -> 0 hits: no t.Parallel convention exists. `grep -rn 'Clock|http.Client|Option' source --include=*.go` -> 0 hits: no injectable seam or options struct exists in any project yet.
- ADR-0003 (Status line, "How update takes the app identity...") sketches `Config{Name, EnvPrefix, Version, Source, StateDir, Token}`; it says update requires no sibling and never imports cli. ADR-0004 lines 72-95 say commons "requires no sibling, so any project can require it", and that a later ADR-0005 would supersede ADR-0003's "update requires no sibling" rule (cobra / Command in cli/spec). `docs/adr/` has no 0005 file (see candidate 20261014-version-command-contract-and-missing-adr-0005).
- Whether update may require commons is not stated explicitly: ADR-0003 says update requires no sibling; ADR-0004 says commons is requirable by any project without a cycle. Treat commons as allowed only if confirmed; cli/spec requires commons today (`source/cli/spec/go.mod`, directory replace `../../commons`).
