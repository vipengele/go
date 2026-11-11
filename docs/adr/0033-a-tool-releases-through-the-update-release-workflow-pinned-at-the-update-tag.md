# A tool releases through the update release workflow, pinned at the update tag

Status: accepted. It follows ADR-0032, whose rendered GoReleaser config the workflow builds from,
and ADR-0024, whose minisign signature it produces, with the unsigned opt-out of ADR-0026. It
follows ADR-0027 in marking a tag with a semver pre-release suffix as a GitHub pre-release. It
leaves ADR-0001's release workflow for this repository's projects unchanged.

ADR-0032 renders the GoReleaser config and leaves the workflow that runs it outside the package.
A tool written on `go.vipengele.org/update` releases through
`.github/workflows/release-tool.yml`, a reusable workflow of this repository, which its own
`release.yml` calls as
`vipengele/go/.github/workflows/release-tool.yml@source/update/vX.Y.Z` on a push of a `vX.Y.Z` tag.
It builds with GoReleaser from the tool's committed config, writes `checksums.txt`, signs it with
minisign and publishes a GitHub Release whose notes are `docs/release-notes/<tag>.md`.

## The rules

- **The workflow is pinned at the `update` tag.** A consumer pins `@source/update/vX.Y.Z`, the
  version of its `go.vipengele.org/update` require. The workflow lives in the `update` project (a
  change to `.github/workflows/release-tool*.yml` selects it) and is released with it, so the
  workflow and `goreleaser.Render` that it builds the output of cannot drift apart.
- **The config is committed.** The tool commits the rendered `.goreleaser.yaml` and keeps it fresh
  with a golden test over `goreleaser.Render`. The input `goreleaser-config` names it, default
  `.goreleaser.yaml`, and a missing file fails the run.
- **The notes are a file.** `release-notes-dir`, default `docs/release-notes`, holds
  `<tag>.md`. The run fails before it builds when the file is missing.
- **The key is a secret of a protected environment.** The workflow declares an optional
  `workflow_call` secret `MINISIGN_KEY` and an `environment` input, and its `build` job declares
  `environment:` from that input. The tool stores the key as a secret of a protected `release`
  environment that only tag pushes reach. An environment secret is resolved inside the called job,
  because a caller job that `uses:` a reusable workflow cannot declare `environment:`.
- **Two jobs.** `build` runs with `contents: read` and holds the key. It runs
  `goreleaser release --clean --skip=publish` on the real tag, not `--snapshot`, so GoReleaser's
  dirty-tree check applies, signs the checksums and uploads `dist/` as an artifact. `publish` runs
  with `contents: write`, runs no consumer code and does not check out. It runs
  `gh release create <tag> --verify-tag --notes-file <notes>` over the assets of the artifact, with
  `--prerelease` when the tag has a semver pre-release suffix (ADR-0027), behind a `gh release view`
  guard so a re-run finishes a partial release. Publishing uses only `GITHUB_TOKEN`.
- **The pre-release rule is the workflow's.** The config holds no `release:` block, so
  `goreleaser.yaml.in`, the section list of the `goreleaser` package and ADR-0032 stay silent on it.
- **The key stays out of the work tree.** It is written to `signing-key-path`, default
  `minisign.key`, which is `goreleaser.DefaultSigningKeyPath`. That path is appended to
  `.git/info/exclude` first: an untracked key makes GoReleaser refuse every real release as a dirty
  tree, which `--snapshot` would hide. An `always()` step deletes the key.
- **Signing is the default.** `sign` is `true`, and then a missing secret fails with a message that
  says so. A tool whose `Config` sets `AllowUnsigned` passes `sign: false`.
- **Paths are relative to `working-directory`.** Only `working-directory` defaults to `.`. Each
  path input has its own default (`goreleaser-config` is `.goreleaser.yaml`, `release-notes-dir` is
  `docs/release-notes`, `signing-key-path` is `minisign.key`, `go-version-file` is `go.mod`), and
  every one is resolved relative to `working-directory`. The workflow sets `GOWORK=off`, its actions are pinned by
  SHA, and GoReleaser is v2.6 or later (ADR-0032).

## The dry run

The `dry-run` input differs from a real release in exactly three things:

- It tags `HEAD` locally as `v0.0.0-dryrun`, never pushed, and sets `GORELEASER_CURRENT_TAG` inside
  the workflow.
- It generates an ephemeral key with `minisign -G -W` instead of reading the secret, and exposes the
  public key with the artifact.
- It skips `publish`.

The notes check, the dirty-tree check, the key handling, GoReleaser and the pre-release computation
run in both modes. The self-test, `.github/workflows/release-tool-selftest.yml`, calls the workflow
with `dry-run`, against the fixture tool in `source/update/testdata/releasefixture`, and
`source/update/testdata/releaseverify` (over `internal/releasetest`) checks that the assets verify
with the tool's own `update.Config`. It cannot set `GORELEASER_CURRENT_TAG` or `GOWORK` itself,
because the `env` of a caller does not reach a called workflow; the workflow owns both.

## Considered options

- **Generating the config in the workflow.** The workflow would run `goreleaser.Render` and need no
  committed file. Rejected because it runs the consumer's code, to build the `Config`, in the job
  that holds the signing key.
- **One job.** Build, sign and publish in one, with the dry run only skipping the publish step.
  Rejected because a called workflow cannot narrow its permissions per mode: the self-test on a pull
  request would run build config the pull request controls with a write token next to the key.
- **Keyless signing.** ADR-0024 rejected Sigstore, and its scheme has no keyless mode, so the
  workflow cannot use the OIDC identity of the run in place of a key.

## Consequences

- **A workflow fix reaches a tool only through an `update` release**, and the tool then raises both
  its require and its pin. Nothing can be pinned until `update`'s first release.
- **The signing key is the one long-lived credential**, because the minisign scheme requires one
  (ADR-0024). Its exposure is limited to the `build` job, which runs under `contents: read`.
- **The self-test is not a merge gate.** Only `ci-gate` is required (`ci-orchestration.yml`); the
  self-test is path-filtered like `ci-tools.yml`, so a pull request that does not touch its paths
  does not run it.
- **`publish` first runs on the first real release.** The self-test never reaches it; its idempotence
  guard makes a re-run safe.
