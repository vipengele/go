---
about: "before #23 (Env/Execute/ExecuteArgs): no ADR or note decides Env, ExecuteArgs's return type or the root builder; #21/#22 left cli/core with no App type, no cli/spec require, and explicit-param wiring"
saw:
  - docs/adr/0003-the-library-is-three-projects-cli-update-and-daemon.md
  - docs/adr/0004-projects-nest-and-take-vanity-module-paths.md
  - docs/adr/0007-the-cli-project-owns-the-version-json-contract.md
  - CONTEXT.md
  - source/cli/core/versioncmd/versioncmd.go
  - source/cli/core/buildinfo/buildinfo.go
  - source/cli/core/.golangci.yml
  - source/cli/core/go.mod
  - source/cli/spec/identity.go
  - .lydite/components.yml
---

Explored 2026-10-14 (git log: c346f84 #112 build info + version command, 971ffc1 #111 Identity).

- Identity (#21) is `spec.Identity` in `source/cli/spec/identity.go` (Name, Owner, Repo, EnvPrefix,
  DirName; Validate, EnvVar, Slug, Subdir; "never reads the environment"). There is NO `app.App`
  type anywhere (`grep -rn 'App\b' source/cli --include=*.go` -> 0). ADR-0004 "Identity lives in
  cli/spec" says App stays in cli/core, undefined yet. `source/cli/core/go.mod` requires only cobra
  v1.10.2 (no cli/spec, no commons, no replace).
- #22: `versioncmd.New(info buildinfo.Info) *cobra.Command` (versioncmd.go:13): explicit param, no
  global; writes via `cmd.OutOrStdout()`, so a root builder/Execute must call SetOut/SetErr from Env
  for it to be testable. `--json` is a local flag. buildinfo.Read() (buildinfo.go:58) is the only
  global-reading point (three -X vars, nolint:gochecknoglobals); `Info` is a value passed in.
- cli/core/.golangci.yml: gochecknoglobals + depguard `cobra-stays-in-versioncmd`
  (`files: "!**/versioncmd/**"`) -> any cobra import in a new root/execute package is lint-denied
  until the rule is widened; ADR-0003 allows cobra for "root command, command wiring, completion,
  version command". Noted in 20261014-version-command-contract-and-missing-adr-0005.md too.
- CONTEXT.md defines **Env**: "environment variables, the standard streams, the working directory
  and the platform paths. Code takes an Env, never os.Getenv or os.Stdout". Nothing else defines
  ExecuteArgs, exit-code return, or the Env/root-builder split (grep Execute|exit|signal in docs,
  CONTEXT.md, source/cli -> nothing). ADR-0005 (command abstraction) does not exist
  (docs/adr has 0001-0004, 0006, 0007); it would supersede ADR-0003's cobra placement.
- Release state: no tags in repo (`git tag` -> none), docs/release-notes holds only commons/v0.1.0.md
  -> cli/core and cli/spec have no notes, so not in strict mode (development mode). .lydite/components.yml
  has `cli-core` and `cli-spec` components (race, ./...); no coverage threshold is visible there.
- Nothing tried-and-abandoned in this area found in docs or store.
