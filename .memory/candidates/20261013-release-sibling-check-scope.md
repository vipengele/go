---
about: "verify-sibling-deps runs over every go.mod in the repo, in both ci-build detect and release tag job, so an unreleased sibling anywhere blocks every release; release test job (workflow-level GOWORK=off) does not catch a leftover replace"
saw:
  - .github/actions/verify-sibling-deps/action.yml
  - .github/workflows/release.yml
  - .github/workflows/ci-build.yml
---

`verify-sibling-deps/action.yml` loops `find source -name go.mod` (:38-ish) and checks replace (:63-66), pseudo-version (:70-73), `git ls-remote` tag (:74); `expect-tag` (:83-90) only validates the tag form and that the project exists, it does not scope the sibling checks to the tagged project. It is called by `ci-build.yml:47` (all projects, every PR) and by release.yml's `tag` job (:244-246). Any change moving the strict check to release time must scope it to the tagged project's go.mod and drop it from ci-build, or one unreleased sibling blocks releasing unrelated projects.

release.yml `env: GOWORK: "off"` at workflow level (:168-169); `test` job runs only `go vet` and `go test -race -shuffle=on` (:263-264), with no tidy and no -mod flag. Verified in scratch: with a `replace => ../sibling` left in go.mod, `GOWORK=off go build` PASSES (replace is honoured for the main module), so the test job does not detect a leftover replace; with an unreleased placeholder version and no replace it FAILS (missing go.sum / 404), which is a real consumer-view check. The tag check does not prove the released sibling contains the API used; only the GOWORK=off build does.
