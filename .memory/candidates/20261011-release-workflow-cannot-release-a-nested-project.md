---
about: "a release of any-depth project (and /vN module paths) is validated against the Worker and the module proxy; both resolve through go.vipengele.org, so a project whose go.mod publish-vanity-modules has not yet published fails verify (after 300s) or announce"
saw:
  - .github/workflows/release.yml
  - .github/workflows/auto-release.yml
  - .github/scripts/release/release-project.sh
  - .github/scripts/release/lib.sh
  - docs/adr/0004-projects-nest-and-take-vanity-module-paths.md
  - docs/adr/0016-the-vanity-module-list-is-published-as-data-to-r2-from-main.md
---

Re-checked 2026-11-17. The original note described a tag-triggered `release.yml`; that workflow is now
a reusable one (ADR-0037) and the subject survives in different places.

- Any depth: `RELEASE_TAG_RE` (`lib.sh:32`) takes one or more `/`-separated project segments, and
  release.yml's `resolve` step repeats it (`release.yml:132`). Notes are
  `docs/release-notes/<project>/<version>.md` (`release-project.sh:188`).
- Project shape: `release-project.sh:160-181` fails when `source/<project>/go.mod` is missing, when a
  parent directory holds a `go.mod` or a subdirectory does.
- Module path: `release-project.sh:207-218` wants `go.vipengele.org/<project>`, plus `/vN` when the
  version is v2 or above (v0/v1 carry no suffix); the tag stays `source/<project>/vX.Y.Z`.
- The vanity dependency is checked twice. `validate` calls `worker_answers`
  (`release-project.sh:122-146`, called `:281`), which polls `<base>/<project>?go-get=1` for up to
  `RELEASE_WORKER_TIMEOUT` (default 300s, `:72`) for a four-field go-import naming
  `go.vipengele.org/<project>` and subdirectory `source/<project>`; it queries the unsuffixed
  path even for a /vN module (`:278-280`). Failure is in `verify`, before any tag exists, and the
  message says to check `publish-vanity-modules` and re-run (`:282`).
- `announce` runs `go list -m <module>@<version>` through proxy.golang.org with up to 6 attempts
  (`:379-388`), using the module path read from the tagged go.mod (`:366-374`); this failure comes
  after the tag is pushed, and `workflow_dispatch` of release.yml with the tag finishes it.
- The Worker learns a project from R2 `modules.json`, written by `publish-vanity-modules.yml` when a
  go.mod merges to main, with no Worker deploy (ADR-0016). A release PR for a new project can merge
  before that run completes, hence the polling.

Not re-run: the live Worker. Only the scripts and the workflow were read.
