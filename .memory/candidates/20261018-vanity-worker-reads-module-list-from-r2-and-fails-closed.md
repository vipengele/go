---
about: "The vanity Worker reads modules.json from the R2 bucket MODULE_LIST, caches it 60s per isolate, and fails closed (503 unreadable/invalid, 404 unknown module) with no bundled fallback or stale-if-error, so a newly published project can 404 for up to about a minute"
saw:
  - tools/vanity-worker/src/modules-source.ts
  - tools/vanity-worker/src/index.ts
  - tools/vanity-worker/src/module-list.ts
  - tools/vanity-worker/wrangler.toml
  - tools/vanity-worker/package.json
  - .github/workflows/publish-vanity-modules.yml
---

Supersedes `20261011-vanity-worker-module-list-generated-from-directories.md`, which describes
build-time generation that no longer exists. Reject that candidate at curation.

Verified against the code on branch feat/vanity-worker-reads-r2:

- Source: `wrangler.toml:10-13` binds bucket `vanity-modules` as `MODULE_LIST`; the object key is
  `MODULE_LIST_KEY = "modules.json"` (`src/modules-source.ts:4`). `publish-vanity-modules.yml`
  (header, lines 1-9) writes it from every `source/**/go.mod` on main.
- Validation: `readModuleList` (`modules-source.ts:24-53`) parses the body as a JSON array of
  strings and passes it to `validateModuleList` (`src/module-list.ts`: path pattern
  `^[a-z0-9-]+(/[a-z0-9-]+)*$`, and no path nested inside another). Absent object, read error,
  non-JSON, wrong shape and schema rejection all become `ModuleListUnavailableError`.
- Cache: `MODULE_LIST_TTL_MS = 60_000` (`modules-source.ts:11`), held per isolate by
  `ModuleListCache` (`:65-91`). Concurrent callers share one in-flight read; a failed read is
  never cached; `get` clears `cached` once expired, so a list past its TTL is never served.
- Fail closed: `index.ts:76-82` answers 503 when `cache.get` throws; an unknown module is 404 from
  `resolve` (`index.ts:84-87`). The doc comment (`index.ts:54-57`) gives the reason: a 404 would
  tell the Go command a served module does not exist. There is deliberately no bundled fallback
  and no stale-if-error; `grep -in "stale\|bundled\|fallback" src/*.ts` finds nothing outside tests.
- Non-`go-get=1` requests redirect to pkg.go.dev without reading the list (`index.ts:66-74`).
- Consequence: a project whose `go.mod` has merged is resolvable only after the publish workflow
  ran and each isolate's cache expired, so it can 404 for up to about 60s after publish.
- The `generate`, `pretest`, `pretypecheck`, `prebuild` hooks and `src/modules.generated.ts` are
  gone from `package.json`; `scripts/generate-modules.ts` now only exports `findGoModDirs` for the
  publisher.
