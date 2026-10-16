---
about: "What a project gaining its first code meets in lydite: component already exists, thresholds are not in this repo, mutation exclude marker syntax, test style, lydite-baseline failure"
saw:
  - .lydite/components.yml
  - .lydite/config.yml
  - .github/workflows/ci-orchestration.yml
  - source/config/key.go
  - source/commons/envvar/envvar_test.go
  - source/daemon/go.mod
---

- `.lydite/components.yml:28-31`: daemon component exists (`go-test`, args `-race ./...`), `go.work:5` and `.gt-repo.yaml:27` have it; `source/daemon/go.mod` is `go 1.27.2` with no require, which satisfies the patch-release rule. `source/daemon/.lydite-reports` is a local artefact git does not ignore; stage files by explicit path.
- Coverage/CRAP/mutation thresholds are NOT in this repo: the gate is the external `pedromvgomes/gt/.github/workflows/reusable-lydite.yml@v2` (ci-orchestration.yml:160). `.lydite/config.yml` holds only a licence allow-list and `go.enabled: true`. Any number (80%, CRAP 30) is unverified.
- Mutation exclusion marker is a trailing line comment: `// [lydite:exclude_from_mutation][reason]`, reasons state why the mutant is unobservable (source/config/key.go:69, file.go:142, update/config.go:149). Comments must still describe the code, not history.
- `lydite-baseline` (ci-orchestration.yml:176-180) is push-only and deliberately not in ci-gate needs (note lydite-job-has-no-skip-guard), so its failure on main is red job not red gate. The "daemon has no entry" cause (no coverable statements) was reported by the caller, not reproduced here; adding the first tested code should fix it.
- Test style seen: stdlib `testing` only, table tests with a `name` field (commons/envvar/envvar_test.go:13), external `_test` packages, `t.Parallel()` only in config/file_test.go; no testify anywhere. Commands: `go test -race -shuffle=on ./...` so tests must not depend on order.
- Not found anywhere: `--gate-flaky` or any flaky policy; no timing-test conventions. Deterministic fakes (channels) are the repo idiom: ADR-0012 injects a `chan struct{}` instead of raising signals, and execute_test counts goroutines (note signals-reach-the-run-only-through-env-interrupts candidate).
