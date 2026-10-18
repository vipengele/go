---
about: "A tools/vanity-worker/vX.Y.Z tag deploys Worker code only; new projects resolve after publish-vanity-modules runs. The post-deploy go get probe skips ROLLBACK_TAG (v0.1.2, still compiled-in list), so redeploying v0.1.0 or v0.1.1 fails the probe"
saw:
  - .github/workflows/deploy-vanity-worker.yml
  - .github/workflows/publish-vanity-modules.yml
  - tools/vanity-worker/package.json
---

Verified in `.github/workflows/deploy-vanity-worker.yml`:

- Header (lines 3-6, 11-13): the Worker reads its list from R2 at request time, so a tag deploys
  Worker code only; adding, moving or removing a project is served once `publish-vanity-modules`
  has published, not by a new Worker tag.
- `env.ROLLBACK_TAG: tools/vanity-worker/v0.1.2` (line 43). At that tag `src/index.ts` imports
  `./modules.generated` and `package.json` has the `generate`/`pretest`/`pretypecheck`/`prebuild`
  hooks (`git show tools/vanity-worker/v0.1.2:tools/vanity-worker/package.json`), so it serves a
  list compiled in at build and reads no R2. The workflow comment (lines 172-175) says that list
  holds `daemon` and none of commons, cli/spec, cli/core; that part was taken from the comment,
  not re-derived from the tag's tree.
- Post-deploy step "The deployed Worker answers go get" (lines 179-212) probes
  `commons cli/spec cli/core daemon` with `?go-get=1`, retrying 6 times at 10s, and fails with a
  message naming the rollback. Its condition (line 180) is
  `needs.tag.outputs.tag != env.ROLLBACK_TAG`, so deploying v0.1.2 skips it.
- Gotcha: `workflow_dispatch` with tag v0.1.0 or v0.1.1 (both exist as tags) deploys an
  R2-less Worker that cannot serve cli/spec, cli/core or commons, so the probe fails the job. Only
  ROLLBACK_TAG is a safe rollback target. Moving ROLLBACK_TAG, or adding a project the probe list
  must cover, means editing the workflow.
