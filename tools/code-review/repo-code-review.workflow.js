export const meta = {
  name: 'repo-code-review',
  description: 'Resumable full review of vipengele/go: findings filed as issues, progress kept in a control issue',
  phases: [
    { title: 'State', detail: 'read ticked units from the control issue', model: 'haiku' },
    { title: 'Review', detail: 'one reviewer per unit' },
    { title: 'Verify', detail: 'one adversarial verifier per finding', model: 'fable' },
    { title: 'File', detail: 'file issues, comment, tick the unit (serialised)', model: 'sonnet' },
    { title: 'Completeness', detail: 'find what the units missed', model: 'fable' },
    { title: 'Report', detail: 'final summary on the control issue', model: 'sonnet' },
  ],
}

const OWNER = 'vipengele', REPO = 'go'
const CONTROL = args.control, COMMIT = args.commit
const ROOT = '/home/user/go'

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

const RULES = `Repository: ${ROOT} (GitHub ${OWNER}/${REPO}), at commit ${COMMIT}. This is a READ-ONLY review: do not edit, create or delete any file inside the repository, run no git command that writes, and write nothing to GitHub. You may run go build / go vet / go test (with GOWORK=off, from the project directory) and npm test to check a suspicion; put any experiment file in a fresh directory under $TMPDIR or /tmp, never in the repository. Do not delegate to other agents.`

const SEV = `Severity: critical = exploitable security hole or data loss in normal use; high = wrong behaviour or security weakness a consumer is likely to hit; medium = wrong behaviour in an uncommon case, misleading documentation, notable missing capability; low = minor, cosmetic or defensive.`

const FINDINGS = { type: 'object', required: ['findings'], properties: { findings: { type: 'array', items: { type: 'object',
  required: ['title', 'category', 'severity', 'locations', 'description', 'evidence'],
  properties: {
    title: { type: 'string' },
    category: { type: 'string', enum: ['bug', 'security', 'performance', 'docs-drift', 'feature-gap', 'test-quality', 'ci'] },
    severity: { type: 'string', enum: ['critical', 'high', 'medium', 'low'] },
    locations: { type: 'array', items: { type: 'string' }, description: 'repo-relative path:line' },
    description: { type: 'string' },
    evidence: { type: 'string', description: 'the code or text that shows it, and the input or sequence that triggers it' },
    suggestion: { type: 'string' },
  } } } } }

const VERDICT = { type: 'object', required: ['verdict', 'reason'], properties: {
  verdict: { type: 'string', enum: ['confirmed', 'refuted', 'uncertain'] },
  reason: { type: 'string' },
  severity: { type: 'string', enum: ['critical', 'high', 'medium', 'low'] },
  category: { type: 'string', enum: ['bug', 'security', 'performance', 'docs-drift', 'feature-gap', 'test-quality', 'ci'] },
  project: { type: 'string', enum: ['cli', 'update', 'daemon', 'config', 'commons', 'filelock', 'repo'] },
  windows: { type: 'boolean' },
  issue_type: { type: 'string', enum: ['Bug', 'Feature', 'Task'] },
  title: { type: 'string' },
  body: { type: 'string' },
} }

const FILED = { type: 'object', required: ['created', 'skipped', 'comment_posted', 'checkbox_ticked'], properties: {
  created: { type: 'array', items: { type: 'object', required: ['number', 'title'], properties: { number: { type: 'number' }, title: { type: 'string' } } } },
  skipped: { type: 'array', items: { type: 'object', required: ['title', 'reason'], properties: { title: { type: 'string' }, reason: { type: 'string' }, existing: { type: 'number' } } } },
  comment_posted: { type: 'boolean' },
  checkbox_ticked: { type: 'boolean' },
} }

const reviewPrompt = u => `${RULES}

You are one reviewer in a larger review; other reviewers cover other files and other lenses, so stay inside yours and go deep.

Scope: ${u.paths}
Lens: ${u.focus}

Be exhaustive within scope, and report only what you can support: every finding needs repo-relative path:line locations and the concrete evidence (the code or text, and the input, sequence or comparison that shows the problem). Check each suspicion against callers, tests and the relevant ADR in docs/adr before reporting it; a behaviour an ADR chose deliberately is not a bug, though it can still be a feature gap. No style nits, no speculative "could be" items. One finding per distinct problem. ${SEV}
Return an empty list if you find nothing.`

const verifyPrompt = (f, u) => `${RULES}

A reviewer of unit "${u.id}" reported the finding below. Your job is to try to REFUTE it by reading the code yourself: follow callers, guards, build tags, tests and the ADRs in docs/adr. Where a quick command or a throwaway test outside the repository settles it, run it.
- "confirmed": you checked it and it holds (correct any wrong detail in what you return).
- "refuted": it is wrong, already handled, or a deliberate documented decision (for a feature gap: the feature exists).
- "uncertain": you could not establish it either way.
Default to refuted or uncertain when the evidence is not solid.

FINDING
${JSON.stringify(f, null, 1)}

When confirmed, also return severity (${SEV}), category, project (cli = anything under source/cli; repo = CI, tools, docs spanning projects), windows (true only if specific to Windows), issue_type (Bug for defects including security and performance; Feature for a missing capability; Task for documentation, test and CI upkeep), a title (plain sentence stating the problem, under 90 characters, no prefix), and an issue body in GitHub markdown with exactly these sections: "## Summary", "## Where" (bullets linking https://github.com/${OWNER}/${REPO}/blob/${COMMIT}/<path>#L<line>), "## Evidence" (short code quotes and the triggering input or sequence; for a feature gap, what exists today and which comparable project offers it), "## Impact", "## Suggested direction". Describe the code as it is; no mention of the review process, of agents or of who found it.`

const filePrompt = (u, drafts, rejected, tick) => `You record the result of code-review unit "${u.id}" in GitHub repository ${OWNER}/${REPO}. Load the GitHub MCP tools with ToolSearch ("select:mcp__github__list_issues,mcp__github__issue_write,mcp__github__issue_read,mcp__github__add_issue_comment"). Do not touch the local repository and do not delegate.

1. List all OPEN issues (fields number, title, labels; follow pagination). Issues labelled "code-review" were filed by this review; the rest were open before it.
2. For each draft below, in order:
   - if an open "code-review" issue already describes the same problem at the same place, skip it (reason "duplicate", existing = that number);
   - else if an open issue without that label already tracks the same work, skip it (reason "overlaps existing", existing = that number);
   - else create it with issue_write method "create", using the draft's title, type, labels and body exactly as given. If creation fails because of the type, retry once without type. Two drafts in this batch that describe the same problem: create only the first.
3. Post ONE comment on issue #${CONTROL} with add_issue_comment, in this form:
   "### Unit \`${u.id}\` done\\n\\nFiled: <#numbers with titles, or 'none'>\\n\\nSkipped: <title — reason — #existing, or 'none'>\\n\\nNot filed after verification: <the list given below, or 'none'>"
${tick ? `4. Read issue #${CONTROL} (issue_read, method get) and update its body with issue_write method "update": change the single line "- [ ] \`${u.id}\`" to "- [x] \`${u.id}\`", and replace any literal placeholder "THIS" with ${CONTROL}. Every other character of the body stays exactly as it is. Pass only the body field besides the identifiers. Read the issue again to check the box is ticked.` : `4. Do not edit the body of issue #${CONTROL}; return checkbox_ticked true.`}
No text you publish may contain a footer, a credit to a tool or model, or a session link.

DRAFTS
${JSON.stringify(drafts, null, 1)}

NOT FILED AFTER VERIFICATION
${JSON.stringify(rejected, null, 1)}`

let lock = Promise.resolve()
const serial = fn => { const p = lock.then(fn, fn); lock = p.then(() => {}, () => {}); return p }

const CAT_LABEL = { security: 'security', performance: 'performance', 'docs-drift': 'docs', 'test-quality': 'testing', ci: 'ci-cd' }

async function runUnit(u, tick) {
  const r = await agent(reviewPrompt(u), { label: `review ${u.id}`, phase: 'Review', schema: FINDINGS, model: u.model })
  if (!r) { log(`${u.id}: review did not finish — left unticked`); return { id: u.id, ok: false } }
  const verdicts = await parallel(r.findings.map((f, i) => () =>
    agent(verifyPrompt(f, u), { label: `verify ${u.id} #${i + 1}`, phase: 'Verify', schema: VERDICT, model: 'fable' })))
  if (verdicts.some(v => !v)) { log(`${u.id}: a verifier did not finish — left unticked`); return { id: u.id, ok: false } }
  const drafts = [], rejected = []
  verdicts.forEach((v, i) => {
    const f = r.findings[i]
    if (v.verdict !== 'confirmed' || !v.title || !v.body) { rejected.push({ title: f.title, verdict: v.verdict === 'confirmed' ? 'uncertain' : v.verdict, reason: v.reason }); return }
    const cat = v.category || f.category, sev = v.severity || f.severity
    const labels = ['code-review', `severity:${sev}`, `project:${v.project || 'repo'}`]
    if (CAT_LABEL[cat]) labels.push(CAT_LABEL[cat])
    if (v.windows) labels.push('windows')
    drafts.push({ title: v.title, type: v.issue_type || 'Task', labels,
      body: `${v.body}\n\n---\nPart of the repository code review tracked in #${CONTROL} (unit \`${u.id}\`, commit \`${COMMIT}\`).` })
  })
  const filed = await serial(() => agent(filePrompt(u, drafts, rejected, tick), { label: `file ${u.id}`, phase: 'File', schema: FILED, model: 'sonnet' }))
  if (!filed || !filed.comment_posted || !filed.checkbox_ticked) { log(`${u.id}: filing did not complete — left unticked`); return { id: u.id, ok: false } }
  log(`${u.id}: ${r.findings.length} found, ${filed.created.length} filed, ${filed.skipped.length} skipped, ${rejected.length} rejected`)
  return { id: u.id, ok: true, found: r.findings.length, created: filed.created, skipped: filed.skipped, rejected }
}

phase('State')
const state = await agent(`Load the GitHub MCP tool with ToolSearch ("select:mcp__github__issue_read") and read issue #${CONTROL} of ${OWNER}/${REPO} (method get). In its body, find every checklist line of the form "- [x] \`<id>\`" (ticked). Return the ids of the ticked lines only, exactly as written between the backticks. Do nothing else.`,
  { label: 'read progress', schema: { type: 'object', required: ['done'], properties: { done: { type: 'array', items: { type: 'string' } } } }, model: 'haiku' })
if (!state) throw new Error('could not read the control issue; nothing was run')
const done = new Set(state.done)
const todo = UNITS.filter(u => !done.has(u.id))
log(`${UNITS.length - todo.length} of ${UNITS.length} units already ticked; running ${todo.length}`)

const results = (await pipeline(todo, u => runUnit(u, true))).map((r, i) => r || { id: todo[i].id, ok: false })
const failed = results.filter(r => !r.ok).map(r => r.id)
if (failed.length) {
  log(`Stopped before the completeness pass: ${failed.length} units unticked (${failed.join(', ')}). Re-run to resume.`)
  return { control: CONTROL, complete: false, unticked: failed, results }
}

let extras = []
if (!done.has('completeness')) {
  phase('Completeness')
  const crit = await agent(`${RULES}

A review of this repository ran these units (id — scope):
${UNITS.map(u => `- ${u.id} — ${u.paths}`).join('\n')}

Load the GitHub MCP tools with ToolSearch ("select:mcp__github__list_issues,mcp__github__issue_read") and read the open issues labelled "code-review" in ${OWNER}/${REPO} and the comments on issue #${CONTROL} to see what was found. Then look at the repository itself and decide what the review has NOT examined or examined too thinly: files in no unit's scope, cross-project interactions (how cli/core, cli/spec, update, daemon, config, cli/state and filelock use each other), a class of problem found in one project that likely recurs in another, a lens not applied to an area. Propose as many further review units as the gaps justify, each narrow and concrete, the most valuable first. Return none if coverage is adequate.`,
    { label: 'completeness critic', schema: { type: 'object', required: ['units'], properties: { units: { type: 'array', items: { type: 'object', required: ['id', 'paths', 'focus', 'project'], properties: { id: { type: 'string', description: 'short kebab-case id' }, paths: { type: 'string' }, focus: { type: 'string' }, project: { type: 'string' } } } } } }, model: 'fable' })
  if (!crit) { log('Completeness critic did not finish. Re-run to resume.'); return { control: CONTROL, complete: false, unticked: ['completeness'], results } }
  const ex = crit.units.map(x => ({ id: `extra-${x.id}`, model: 'fable', project: x.project, paths: x.paths, focus: x.focus }))
  extras = (await pipeline(ex, u => runUnit(u, false))).map((r, i) => r || { id: ex[i].id, ok: false })
  if (extras.some(r => !r.ok)) { log('An extra unit did not finish; completeness left unticked. Re-run to resume.'); return { control: CONTROL, complete: false, unticked: ['completeness'], results, extras } }
}

phase('Report')
const report = await agent(`Load the GitHub MCP tools with ToolSearch ("select:mcp__github__list_issues,mcp__github__issue_write,mcp__github__issue_read,mcp__github__add_issue_comment"). In ${OWNER}/${REPO}:
1. List every issue (open and closed) labelled "code-review" except #${CONTROL} (fields number, title, labels; follow pagination).
2. Post one comment on #${CONTROL} headed "## Review complete" with: the total count; a table of counts by severity label and by category (Bug/security/performance/docs/testing/ci-cd/feature) ; then the issues grouped by project label, most severe first, as "#number title".
3. Read #${CONTROL} and update its body (issue_write method "update", body only): change "- [ ] \`completeness\`" to "- [x] \`completeness\`" and the line starting "**Status:**" to "**Status:** complete — last run at commit \`${COMMIT}\`.". Every other character stays as it is.
No footer, tool or model credit, or session link in anything you publish. Return the total number of issues and whether both writes succeeded.`,
  { label: 'final summary', schema: { type: 'object', required: ['total', 'ok'], properties: { total: { type: 'number' }, ok: { type: 'boolean' } } }, model: 'sonnet' })

return { control: CONTROL, complete: !!(report && report.ok), total_issues: report && report.total, results, extras }
