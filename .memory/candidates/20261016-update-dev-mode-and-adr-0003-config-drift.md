---
about: "source/update is in development mode for verify-sibling-deps (no release notes, no tags); ADR-0003's Config sketch (Token func, StateDir passed in) is not the code and ADR-0008/0009 supersede its dependency claims"
saw:
  - docs/release-notes
  - source/update/go.mod
  - source/update/config.go
  - source/update/.golangci.yml
  - docs/adr/0003-the-library-is-three-projects-cli-update-and-daemon.md
  - docs/adr/0008-update-takes-its-identity-from-cli-spec.md
---

`ls docs/release-notes` -> only `commons/`; `git tag -l 'source/update/*'` -> empty. So update has no
note file without a tag: development mode, the directory replaces `../cli/spec` and `../commons`
in source/update/go.mod are legal. Release-notes files for update v0.1.0 would flip it to strict mode
until the tag exists.
Config (config.go) has no Token field and no Version field; ADR-0003's sketch is historical. The
update -> cli/spec + commons dependency is ADR-0008; "update requires no sibling" in ADR-0003 is withdrawn.
The lydite component for update (.lydite/components.yml:24) is a plain `go test -race ./...`; no
coverage threshold lives in the repo (.lydite/config.yml holds licence and go settings only).
