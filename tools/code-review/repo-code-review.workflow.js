export const meta = {
  name: 'repo-code-review',
  description: 'Resumable full review of vipengele/go: every step checkpointed to a branch, findings filed as issues',
  phases: [
    { title: 'State', detail: 'read the checkpoint store', model: 'sonnet' },
    { title: 'Review', detail: 'one reviewer per unit, findings saved to the branch', model: 'fable' },
    { title: 'Verify', detail: 'one adversarial verifier per finding, verdict saved to the branch', model: 'fable' },
    { title: 'File', detail: 'file issues, record each, comment, tick the unit', model: 'sonnet' },
    { title: 'Completeness', detail: 'find what the units missed', model: 'fable' },
    { title: 'Report', detail: 'final summary on the control issue', model: 'sonnet' },
  ],
}

const OWNER = 'vipengele', REPO = 'go'
const A = args || {}
const CONTROL = A.control || 203, COMMIT = 'e83a35a'
const ROOT = A.root || '/home/user/go'
const DIR = `${ROOT}/tools/code-review`
const CR = `python3 ${DIR}/cr.py`
const fname = id => id.replace(':', '--')

const AREAS = {
  'core-app': { project: 'cli', paths: 'source/cli/core (root package), source/cli/core/internal/cobra, source/cli/core/internal/errrender, source/cli/core/command/config, source/cli/core/command/version' },
  'core-logging': { project: 'cli', paths: 'source/cli/core/logging (and logtest), source/cli/core/buildinfo' },
  'spec-model': { project: 'cli', paths: 'source/cli/spec (root package), source/cli/spec/speccheck, table, terminal, version' },
  'spec-paths': { project: 'cli', paths: 'source/cli/spec/completioninstall, source/cli/spec/paths' },
  'small': { project: 'cli/commons/filelock', paths: 'source/cli/state, source/commons, source/filelock' },
  'config': { project: 'config', paths: 'source/config' },
  'daemon-core': { project: 'daemon', paths: 'source/daemon (root package only), source/daemon/daemoncmd, source/daemon/daemontest, source/daemon/systemd' },
  'daemon-service': { project: 'daemon', paths: 'source/daemon/service, source/daemon/httpserver' },
  'update-core': { project: 'update', paths: 'source/update (root package files only, not sub-directories)' },
  'update-sub': { project: 'update', paths: 'source/update/asset, goreleaser, installscript, internal/minisign, internal/minisigntest, internal/releasetest, updatecmd, updatetest, testdata' },
}

const LENS = {
  bugs: { model: 'fable', text: 'Correctness bugs in the non-test Go code: logic errors, wrong error handling, races and goroutine leaks, context and cancellation misuse, resource leaks, platform differences (Windows vs Unix build-tagged files), boundary and nil cases, behaviour that contradicts the doc comment on the same symbol, API contracts a caller can break without noticing. Read every non-test file in scope completely; read tests only to learn intended behaviour.' },
  secperf: { model: 'fable', text: 'Security and performance issues in the non-test Go code. Security: path traversal and archive extraction, symlink and TOCTOU races, file permissions, signature/checksum verification gaps and downgrade paths, token or secret leakage (logs, redirects, cross-host), command or script injection (including generated shell/PowerShell), HTTP server hardening (timeouts, limits, auth ordering), unsafe defaults, trust in environment variables. Performance: unbounded reads or memory, needless allocation or repeated work on hot paths, missing timeouts, blocking under locks, quadratic behaviour, busy loops. Read every non-test file in scope completely.' },
  tests: { model: 'fable', text: 'Test-suite quality for the code in scope: exported behaviour or error paths with no test, tests that cannot fail or assert nothing meaningful, tests coupled to implementation detail, flakiness (sleeps, real time, real network, ports, ordering, shared global state, missing t.Parallel safety under -race -shuffle=on), platform-gated code with no test on that platform, test helpers with bugs. Report a missing test only when you can name the untested behaviour and why it matters.' },
}

const UNITS = []
for (const lens of ['bugs', 'secperf']) for (const a of Object.keys(AREAS))
  UNITS.push({ id: `${a}:${lens}`, model: LENS[lens].model, project: AREAS[a].project, paths: AREAS[a].paths, focus: LENS[lens].text })
UNITS.push(
  { id: 'ci-build:ci', model: 'fable', project: 'repo', paths: '.github/workflows/ci-*.yml, gt-*.yml, dependabot-auto-merge.yml, .github/actions/changed-projects, .github/actions/verify-sibling-deps, .lydite/, .gt-repo.yaml, .gt.yaml, go.work, every source/**/go.mod and .golangci.yml',
    focus: 'Correctness and security of CI: logic bugs in the shell/scripts (change detection, dependents, sibling verification edge cases, nested project paths), cases where a required check is skipped or passes vacuously, script injection through untrusted event fields, over-broad permissions, unpinned third-party actions, pull_request_target or workflow_run misuse, secret exposure, auto-merge trust. Also every difference between what these files do and what CLAUDE.md "CI" and "Layout" sections and the agentic rules say they do, and go directives not naming a current patched release.' },
  { id: 'ci-release:ci', model: 'fable', project: 'repo', paths: '.github/workflows/release.yml, release-tool.yml, release-tool-selftest.yml, deploy-vanity-worker.yml, publish-vanity-modules.yml, agentic/skills/release',
    focus: 'Correctness and security of the release and deploy workflows: tag validation gaps, partial-release and re-run behaviour, races, injection through tag names or inputs, permissions and secret handling, supply-chain exposure of the reusable release-tool workflow consumed by other repositories, signing key handling. Also drift between these workflows and the "Release" section of CLAUDE.md, the release skill and ADRs 0001, 0006, 0016, 0032, 0033.' },
  { id: 'worker:worker', model: 'fable', project: 'repo', paths: 'tools/vanity-worker (src/, scripts/, wrangler.toml, package.json)',
    focus: 'Bugs, security and performance of the Cloudflare Worker serving go.vipengele.org and of its module-list scripts: path matching for nested modules and /vN suffixes, HTML/meta injection, caching and stale or missing modules.json, error responses the go command mishandles, R2 failure behaviour, input validation of the published list. Also drift from ADR-0004 and ADR-0016.' },
  { id: 'adr-a:docs', model: 'fable', project: 'repo', paths: 'docs/adr files numbered 0001 to 0016 (several numbers have more than one file), checked against the code under source/, .github/ and tools/',
    focus: 'Documentation drift: for each ADR in scope, extract every concrete claim about how the code behaves (names, packages, signatures, defaults, orderings, exit codes, file formats) and check it against the code. Report each claim the code contradicts or does not implement. Also report duplicate ADR numbers and ADRs superseded in practice without saying so, once each.' },
  { id: 'adr-b:docs', model: 'fable', project: 'repo', paths: 'docs/adr files numbered 0017 to 0033 (several numbers have more than one file), checked against the code under source/, .github/ and tools/',
    focus: 'Documentation drift: for each ADR in scope, extract every concrete claim about how the code behaves (names, packages, signatures, defaults, orderings, exit codes, file formats) and check it against the code. Report each claim the code contradicts or does not implement.' },
  { id: 'guides:docs', model: 'fable', project: 'repo', paths: 'README.md, CONTEXT.md, agentic/vipengele-go.md, agentic/rules, agentic/skills, docs/release-notes, every AGENTS.md under source/, every doc.go, package comment and Example test under source/',
    focus: 'Documentation drift: every statement in these documents that the code, the workflows or the repository layout contradicts; glossary terms in CONTEXT.md used differently in code; examples that would not compile or behave as described; release notes describing behaviour the tagged code lacks; projects or packages that exist but are documented nowhere. The root CLAUDE.md and AGENTS.md are rendered from agentic/, so cite the agentic/ source file, never the rendered one.' },
  { id: 'promises:gaps', model: 'fable', project: 'repo', paths: 'README.md, CONTEXT.md, docs/adr, docs/release-notes, package docs — against all code under source/',
    focus: 'Feature gaps against the repository\'s own promises: capabilities the documents describe, imply or list as part of a project that the code does not deliver or delivers only partly (stubbed, one platform only, option accepted but ignored, TODO). First inventory the full feature set of each project from its exported API, then compare.' },
  { id: 'cli:gaps', model: 'fable', project: 'cli', paths: 'source/cli/core, source/cli/spec, source/cli/state, source/config',
    focus: 'Feature gaps against comparable frameworks: inventory what these projects offer a CLI author, then compare with what cobra, urfave/cli, kong, viper and koanf users rely on (for example flag kinds and validation, env binding, help and usage customisation, command grouping and aliases, suggestions, man page or docs generation, shell completion coverage, config formats, layering, watch/reload, secrets). Report only gaps a real consumer of this framework would hit; say which peer has it.' },
  { id: 'update:gaps', model: 'fable', project: 'update', paths: 'source/update',
    focus: 'Feature gaps against comparable self-update libraries and release tooling (creativeprojects/go-selfupdate, rhysd/go-github-selfupdate, minio/selfupdate, equinox, Sparkle/TUF concepts): inventory what this project offers, then report missing capabilities a consumer would expect (for example other release sources, proxies and mirrors, private releases, delta or resumable downloads, rollback, staged rollout, key rotation, platform or archive coverage, package-manager-installed detection, progress reporting). Say which peer has it.' },
  { id: 'daemon-config:gaps', model: 'fable', project: 'daemon', paths: 'source/daemon, source/filelock',
    focus: 'Feature gaps against comparable daemon and service libraries (kardianos/service, takama/daemon, coreos/go-systemd, suture, oklog/run, common HTTP server kits): inventory what this project offers, then report missing capabilities a consumer would expect (for example launchd or Windows parity, restart policies and backoff, log integration, privilege drop, graceful restart, metrics and pprof endpoints, TLS, rate limits). Say which peer has it.' },
)
for (const a of Object.keys(AREAS))
  UNITS.push({ id: `${a}:tests`, model: LENS.tests.model, project: AREAS[a].project, paths: AREAS[a].paths, focus: LENS.tests.text })

const RULES = `Repository checkout: ${ROOT} (GitHub ${OWNER}/${REPO}), on the checkpoint branch; the code under review is commit ${COMMIT}. Do not edit, create or delete any file in the checkout except the one result file named below, run no git command yourself, and write nothing to GitHub. You may run go build / go vet / go test (with GOWORK=off, from the project directory) and npm test to check a suspicion; put any experiment file in a fresh directory under /tmp, never in the checkout. Ignore tools/code-review except as told here. Do not delegate to other agents.`

const SEV = `Severity: critical = exploitable security hole or data loss in normal use; high = wrong behaviour or security weakness a consumer is likely to hit; medium = wrong behaviour in an uncommon case, misleading documentation, notable missing capability; low = minor, cosmetic or defensive.`

const SAVED = { type: 'object', required: ['saved'], properties: { saved: { type: 'boolean', description: 'true only if cr.py save printed "saved"' }, count: { type: 'number' }, verdict: { type: 'string' } } }

const reviewPrompt = u => `${RULES}

You are one reviewer in a larger review; other reviewers cover other files and other lenses, so stay inside yours and go deep.

Unit: ${u.id}
Scope: ${u.paths}
Lens: ${u.focus}

Be exhaustive within scope, and report only what you can support: every finding needs repo-relative path:line locations and the concrete evidence (the code or text, and the input, sequence or comparison that shows the problem). Check each suspicion against callers, tests and the relevant ADR in docs/adr before reporting it; a behaviour an ADR chose deliberately is not a bug, though it can still be a feature gap. No style nits, no speculative "could be" items. One finding per distinct problem. ${SEV}

RESULT. Write your findings to ${DIR}/findings/${fname(u.id)}.json as JSON of this shape, then run \`${CR} save ${DIR}/findings/${fname(u.id)}.json\`. If it reports an error, fix the file and run it again.
{"unit": "${u.id}", "commit": "${COMMIT}", "status": "reviewed-unverified", "findings": [{"title": "...", "category": "bug|security|performance|docs-drift|feature-gap|test-quality|ci", "severity": "critical|high|medium|low", "locations": ["path:line"], "description": "...", "evidence": "the code or text that shows it, and the input or sequence that triggers it", "suggestion": "..."}]}
An empty findings list is a valid result. Return saved (whether the save command printed "saved") and count (the number of findings).`

const verifyPrompt = (u, n) => `${RULES}

The reviewer of unit "${u.id}" reported findings in ${DIR}/findings/${fname(u.id)}.json. Take finding number ${n} (1-based position in the "findings" list) and no other. Your job is to try to REFUTE it by reading the code yourself: follow callers, guards, build tags, tests and the ADRs in docs/adr. Where a quick command or a throwaway test outside the checkout settles it, run it.
- "confirmed": you checked it and it holds (correct any wrong detail in what you write).
- "refuted": it is wrong, already handled, or a deliberate documented decision (for a feature gap: the feature exists).
- "uncertain": you could not establish it either way.
Default to refuted or uncertain when the evidence is not solid.

RESULT. Write ${DIR}/verdicts/${fname(u.id)}/${n}.json, then run \`${CR} save ${DIR}/verdicts/${fname(u.id)}/${n}.json\`. If it reports an error, fix the file and run it again. The file is JSON with "verdict" and "reason" (two or three sentences on what you checked). When confirmed it also has:
- "severity" (${SEV})
- "category": bug, security, performance, docs-drift, feature-gap, test-quality or ci
- "project": cli (anything under source/cli), update, daemon, config, commons, filelock, or repo (CI, tools, documents spanning projects)
- "windows": true only if specific to Windows
- "issue_type": Bug for defects including security and performance; Feature for a missing capability; Task for documentation, test and CI upkeep
- "title": a plain sentence stating the problem, under 90 characters, no prefix
- "body": GitHub markdown with exactly these sections: "## Summary", "## Where" (bullets linking https://github.com/${OWNER}/${REPO}/blob/${COMMIT}/<path>#L<line>), "## Evidence" (short code quotes and the triggering input or sequence; for a feature gap, what exists today and which comparable project offers it), "## Impact", "## Suggested direction". Describe the code as it is; no mention of the review, of agents or of who found it.
Return saved (whether the save command printed "saved") and verdict.`

const filePrompt = (u, tick) => `You file the verified findings of code-review unit "${u.id}" as issues in ${OWNER}/${REPO}. Load the GitHub MCP tools with ToolSearch ("select:mcp__github__list_issues,mcp__github__issue_write,mcp__github__issue_read,mcp__github__add_issue_comment"). Edit no file and run no git command; the only shell commands you run are the \`${CR}\` ones below. Do not delegate.

1. Run \`${CR} drafts ${u.id}\`. It prints the issues still to file, as JSON (n, title, type, labels, body). An empty list means go to step 4.
2. List all OPEN issues (fields number, title, labels; follow pagination). Those labelled "code-review" were filed by this review; the rest were open before it.
3. For each draft, in order, do one of these and run its command BEFORE moving to the next draft:
   - an open "code-review" issue already describes the same problem at the same place: \`${CR} filed ${u.id} <n> skipped <that number> duplicate\`
   - an open issue without that label already tracks the same work: \`${CR} filed ${u.id} <n> skipped <that number> overlaps existing\`
   - otherwise create the issue with issue_write method "create", using the draft's title, type, labels and body exactly as given (if it fails because of the type, retry once without type), then \`${CR} filed ${u.id} <n> created <new issue number>\`. Count an issue you created earlier in this step as an open "code-review" issue.
4. Run \`${CR} comment ${u.id}\` and post its output, unchanged, as one comment on issue #${CONTROL}.
${tick ? `5. Read issue #${CONTROL} (issue_read, method get) and update its body with issue_write method "update", passing only the body: change the single line "- [ ] \`${u.id}\`" to "- [x] \`${u.id}\`". Every other character of the body stays exactly as it is.` : `5. (No checklist line exists for this unit; skip this step.)`}
6. Run \`${CR} done ${u.id}\`. Return done = true only if it printed "done ${u.id}"; also return how many issues you created and skipped.`

async function runUnit(u, st, tick) {
  let n = st ? st.n : -1, have = new Set(st ? st.verified : [])
  if (n < 0) {
    const r = await agent(reviewPrompt(u), { label: `review ${u.id}`, phase: 'Review', schema: SAVED, model: 'fable' })
    if (!r || !r.saved || typeof r.count !== 'number') { log(`${u.id}: review not saved`); return false }
    n = r.count
  }
  const missing = Array.from({ length: n }, (_, i) => i + 1).filter(i => !have.has(i))
  const vs = await parallel(missing.map(i => () =>
    agent(verifyPrompt(u, i), { label: `verify ${u.id} #${i}`, phase: 'Verify', schema: SAVED, model: 'fable' })))
  if (vs.some(v => !v || !v.saved)) { log(`${u.id}: ${vs.filter(v => !v || !v.saved).length} of ${missing.length} verdicts not saved`); return false }
  const f = await agent(filePrompt(u, tick), { label: `file ${u.id}`, phase: 'File', model: 'sonnet',
    schema: { type: 'object', required: ['done'], properties: { done: { type: 'boolean' }, created: { type: 'number' }, skipped: { type: 'number' } } } })
  if (!f || !f.done) { log(`${u.id}: filing not finished`); return false }
  log(`${u.id}: done — ${n} reported, ${f.created || 0} filed, ${f.skipped || 0} skipped`)
  return true
}

// Units run one after another, so that each is filed and marked done before the next starts.
// Two failed units in a row mean the usage limit or the network is gone: stop instead of burning through the list.
async function runAll(units, state, tick) {
  const left = []
  let streak = 0
  for (let i = 0; i < units.length; i++) {
    const u = units[i]
    if (await runUnit(u, state.get(u.id), tick)) { streak = 0; continue }
    left.push(u.id)
    if (++streak >= 2) { left.push(...units.slice(i + 1).map(x => x.id)); log(`Two units in a row failed; stopping with ${left.length} units left.`); break }
  }
  return left
}

const STATE = { type: 'object', required: ['ready', 'units', 'completeness_done'], properties: {
  ready: { type: 'boolean' }, problem: { type: 'string' },
  units: { type: 'array', items: { type: 'object', required: ['id', 'n', 'verified', 'done'], properties: { id: { type: 'string' }, n: { type: 'number' }, verified: { type: 'array', items: { type: 'number' } }, done: { type: 'boolean' } } } },
  extras: { type: 'array', items: { type: 'object', required: ['id', 'paths', 'focus', 'project'], properties: { id: { type: 'string' }, paths: { type: 'string' }, focus: { type: 'string' }, project: { type: 'string' } } } },
  has_extras_file: { type: 'boolean' },
  completeness_done: { type: 'boolean' },
} }

phase('State')
const s = await agent(`Prepare and read the checkpoint store of a code review. Run these in ${ROOT}, in order, and change nothing else:
1. \`git -C ${ROOT} rev-parse --abbrev-ref HEAD\` must print ccr-af36d1cd-8vhwcf.
2. If the environment variable AGTK_CLOUD is 1, run \`agtk cloud init\` in ${ROOT}.
3. \`git -C ${ROOT} var GIT_COMMITTER_IDENT\` must not contain noreply@anthropic.com.
4. \`${CR} status\` prints one JSON object.
If step 1 or 3 does not hold or a command fails, return ready = false with the problem, empty units and completeness_done = false. Otherwise return ready = true and copy the JSON of step 4 exactly: units, completeness_done, and extras with has_extras_file = true when its "extras" is a list (even an empty one), or has_extras_file = false and no extras when it is null.`,
  { label: 'read checkpoints', schema: STATE, model: 'sonnet' })
if (!s || !s.ready) throw new Error(`checkpoint store not usable: ${s ? s.problem : 'state agent did not finish'}`)
const state = new Map(s.units.map(x => [x.id, x]))
const isDone = id => state.has(id) && state.get(id).done
// Units already reviewed go first: their verification and filing is the cheapest work to finish.
const todo = UNITS.filter(u => !isDone(u.id)).sort((a, b) => (state.has(b.id) ? 1 : 0) - (state.has(a.id) ? 1 : 0))
log(`${UNITS.length - todo.length} of ${UNITS.length} units done; ${todo.filter(u => state.has(u.id)).length} reviewed and waiting for verification; ${todo.filter(u => !state.has(u.id)).length} not reviewed`)

const left = await runAll(todo, state, true)
if (left.length) return { complete: false, units_left: left }
if (s.completeness_done) return { complete: true }

phase('Completeness')
let extras = s.has_extras_file ? (s.extras || []) : null
if (!extras) {
  const crit = await agent(`${RULES}

A review of this repository ran these units (id — scope):
${UNITS.map(u => `- ${u.id} — ${u.paths}`).join('\n')}

What each unit reported and how it was judged is in ${DIR}/findings and ${DIR}/verdicts; read them. Then look at the repository itself and decide what the review has NOT examined or examined too thinly: files in no unit's scope, cross-project interactions (how cli/core, cli/spec, update, daemon, config, cli/state and filelock use each other), a class of problem confirmed in one project that likely recurs in another, a lens not applied to an area. Propose as many further review units as the gaps justify, each narrow and concrete, the most valuable first; none if coverage is adequate.

RESULT. Write ${DIR}/extras.json as {"units": [{"id": "extra-<short-kebab-name>", "paths": "...", "focus": "what to look for, written as an instruction to the reviewer", "project": "..."}]} (ids have no colon), then run \`${CR} save ${DIR}/extras.json\`. Return saved and the same units.`,
    { label: 'completeness critic', model: 'fable', schema: { type: 'object', required: ['saved', 'units'], properties: { saved: { type: 'boolean' }, units: STATE.properties.extras } } })
  if (!crit || !crit.saved) return { complete: false, units_left: ['completeness'] }
  extras = crit.units
}
const exLeft = await runAll(extras.filter(x => !isDone(x.id)), state, false)
if (exLeft.length) return { complete: false, units_left: exLeft }

phase('Report')
const rep = await agent(`Load the GitHub MCP tools with ToolSearch ("select:mcp__github__issue_write,mcp__github__issue_read,mcp__github__add_issue_comment"). Edit no file and run no git command.
1. Run \`${CR} report\` and post its output, unchanged, as one comment on issue #${CONTROL} of ${OWNER}/${REPO}.
2. Read issue #${CONTROL} and update its body (issue_write method "update", body only): change "- [ ] \`completeness\`" to "- [x] \`completeness\`" and the line starting "**Status:**" to "**Status:** complete.". Every other character stays as it is.
3. Run \`${CR} done completeness\`. Return done = true only if it printed "done completeness".`,
  { label: 'final summary', model: 'sonnet', schema: { type: 'object', required: ['done'], properties: { done: { type: 'boolean' } } } })
return { complete: !!(rep && rep.done) }
