# The vanity module list is published as data to R2 from `main`

Status: accepted. It amends `docs/adr/0004-projects-nest-and-take-vanity-module-paths.md`, which
generates the Worker's module list from `source/**/go.mod` at the commit a Worker tag points at.

The set of projects is a fact about `main`: one entry for every `source/**/go.mod`. This decision
publishes that set as a file, separately from the Worker's code, so that a change to the set is a
change to data and not a Worker release. The Worker's own list is unchanged by this decision, and
the Worker does not read the published object.

## What is published

- **The list.** A JSON array of project paths relative to `source/`, sorted, one per `go.mod`
  (`["cli/core","cli/spec","commons","daemon","update"]`). It is generated and validated by
  `tools/vanity-worker/scripts/generate-modules.ts` against the schema in
  `tools/vanity-worker/src/module-list.ts`, and nothing is sent that fails validation.
- **Where.** The object `modules.json` in a Cloudflare R2 bucket, written by
  `tools/vanity-worker/scripts/publish-modules.ts` through R2's S3-compatible API as one
  `PutObject` of the whole list. A failed upload leaves the previous object in place.
- **When.** The `publish-vanity-modules` workflow runs on a push to `main` that changes a `go.mod`,
  the generator, the schema module, the publisher or the workflow, and on `workflow_dispatch`, which
  republishes from `main`'s head. A step in the workflow fails a run on any ref but `main`, as a
  guard against a mistaken dispatch; the control that keeps the credentials to `main` is the
  environment's deployment-branch policy, below.
- **Under what credential.** The job runs in its own GitHub Environment, `vanity-modules`, which
  holds the secrets `R2_ACCESS_KEY_ID` and `R2_SECRET_ACCESS_KEY` and the variables
  `CLOUDFLARE_ACCOUNT_ID` and `R2_BUCKET`. The key is an R2 API token scoped to that one bucket, so
  a run that reaches it can write that bucket and nothing else. The environment allows deployments
  from the `main` branch only, with administrator bypass off, as `cloudflare-production` allows only
  the Worker tag pattern. That policy is configured on the environment, outside the repository's
  files, and it is the control for the credential: GitHub runs a workflow file from the ref that
  triggers it, so a branch could remove the in-workflow ref check or add another workflow naming the
  environment. Without all four values the job logs a notice and succeeds without uploading.
- **Checked on every pull request.** `ci-tools.yml` runs `publish-modules --dry-run` on a change
  touching `tools/**`, which generates and validates the list and sends nothing.

## The Worker tag

A `tools/vanity-worker/v*` tag deploys the Worker's code (`deploy-vanity-worker.yml`). It is
reserved for a change to that code. Adding, moving or removing a project is not one: it changes
`source/**/go.mod` and is published by `publish-vanity-modules`.

## Considered options

- **A `main` branch policy on the `cloudflare-production` environment.** The deploy would run on
  every push to `main`, so any merged change would reach the environment holding the code-deploy
  token, and the deploy by named tag, which is how a Worker version is redeployed or rolled back,
  would be lost with the tag gate.
- **CI creating a Worker tag and dispatching the deploy.** A tag pushed or a workflow dispatched
  with `GITHUB_TOKEN` does not trigger other workflows, and the repository has no GitHub App or
  personal access token to act with.
- **AWS S3.** A second cloud account, with its own credentials and billing, for a file Cloudflare
  already hosts next to the Worker.
- **Workers KV.** Eventually consistent: a reader of a key just written can still be answered with
  the old value, so a check made straight after publication could not trust the answer. R2 is
  strongly consistent, so a read after a successful write sees that write.
- **Staying manual with only a pre-flight check.** A new project stays unresolvable until someone
  remembers to tag the Worker, and a check that reports the gap does not close it.

## Consequences

- **The environments hold separate credentials.** `cloudflare-production` holds the token that
  deploys code and runs only from a Worker tag. `vanity-modules` holds a token that writes one
  bucket and runs from `main`. A secret is available only to jobs that name the environment holding
  it, and each environment's deployment policy decides which refs may: the Worker tag pattern for
  `cloudflare-production`, `main` for `vanity-modules`. The workflow's own ref check is a guard
  against mistakes, not the control.
- **The object is the list at the last successful publication.** An unchanged list overwrites the
  object with identical content. Runs are serialised and never cancelled in flight, so the last
  run started is the list left published.
- **A repository without the four values publishes nothing** and shows a green run.
