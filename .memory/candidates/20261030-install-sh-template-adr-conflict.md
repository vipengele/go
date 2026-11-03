---
about: "ADR-0021 (completion installer) already decided install.sh is out of scope and the path table + sh snippet live in cli/spec/completioninstall, against ADR-0003 which assigns install-script templates to update"
saw:
  - docs/adr/0021-the-completion-installer-is-cli-spec-completioninstall-with-an-update-post-hook.md
  - docs/adr/0003-the-library-is-three-projects-cli-update-and-daemon.md
  - source/cli/spec/completioninstall/script.go
  - source/update/updatetest/server.go
---

Found while planning issue #64 (install.sh template in source/update).

- ADR-0021 "Template": completioninstall exports `Snippet(name)` (script.go:151), a POSIX-sh
  fragment (`completion_target SHELL`) generated from the same table `Target` uses, with a test that
  runs it under sh and asserts equality with the Go Target. "The full install.sh is out of scope; a
  tool embeds the snippet". It rejects "a template owned by update" because two hand-maintained
  copies drift. ADR-0003:23 still lists "install script and release templates" under update.
- So install.sh in update should embed/consume completioninstall.Snippet, not duplicate the table.
- Two ADRs numbered 0019 (file-lock, missing-asset) and others duplicate numbers; "CI never runs
  Windows (ADR-0019)" in ci-build.yml:92 means the file-lock ADR (its Windows verification section).
- updatetest server (server.go:64-80) serves /<o>/<r>/releases/latest as 302 to tag page,
  /repos/... API, and /<o>/<r>/releases/download/<tag>/<name>; checksums are `<sha256>  <name>`.
  Archive name is for runtime GOOS/GOARCH. Server is a Go httptest, so a shell test must be a Go
  test exec'ing sh with the server URL. No shellcheck anywhere in repo or workflows.
