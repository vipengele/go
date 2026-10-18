---
about: "The candidate 20261011-vanity-worker-module-list-generated-from-directories.md describes removed build-time generation (modules.generated.ts, pretest/pretypecheck/prebuild) and should be rejected"
saw:
  - tools/vanity-worker/package.json
  - tools/vanity-worker/src/index.ts
  - tools/vanity-worker/scripts/generate-modules.ts
---

`package.json` has no `generate`, `pretest`, `pretypecheck` or `prebuild` script, and `src/index.ts`
no longer imports `./modules.generated`; the Worker reads `modules.json` from R2. See
`20261018-vanity-worker-reads-module-list-from-r2-and-fails-closed.md` for the replacement.
Only the sentence that `go.mod` directories (not module lines) define the list still holds, and
it now applies to the publisher (`scripts/generate-modules.ts` `findGoModDirs`), not the build.
