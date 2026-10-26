---
about: "installer for completion (#44) is unspecified by ADR-0020; no install-script template, no post-update hook, no FS seam beyond paths.Options/Env.LookupEnv exist"
saw:
  - docs/adr/0020-completion-is-an-opt-in-app-option-over-a-neutral-spec-model.md
  - docs/adr/0003-the-library-is-three-projects-cli-update-and-daemon.md
  - docs/adr/0009-the-app-owns-version-and-takes-hooks-as-options.md
  - docs/adr/0009-platform-paths-live-in-cli-spec-paths.md
  - source/cli/spec/identity.go
  - source/cli/spec/paths/paths.go
  - source/update/config.go
  - source/cli/core/go.mod
---
Verified 2026-10-26 by reading ADRs and code.
- ADR-0020 "Consequences": "No installation ... An installer that places the script, and an env var that turns the command off, are not covered here."
- Opt-out env var convention: spec.Identity.EnvVar(suffix) (source/cli/spec/identity.go:54); update uses suffix NO_UPDATE_CHECK (source/update/config.go:21). Pattern <PREFIX>_NO_<THING>.
- Injection pattern: plain Options struct with GOOS, LookupEnv, UserHomeDir funcs (source/cli/spec/paths/paths.go:52-68); spec.Env.LookupEnv (source/cli/spec/env.go). No afero.
- No install.sh template exists in source/; "install script and release templates" is only listed in ADR-0003 table for update. update has only asset-name templates (source/update/asset).
- update has no post-update hook (grep hook in source/update: none). cli/core must not import update (ADR-0009 "The update check is wired by the tool"); update must not import cli/core (ADR-0009-paths); both share cli/spec.
- cli/core requires spec, state, config, commons (source/cli/core/go.mod); not update.
