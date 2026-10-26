---
about: "Candidate 20261014 claims nil Source reports ErrNoSource; it is now false"
saw:
  - source/update/config.go
  - docs/adr/0016-the-default-update-source-reads-the-github-latest-redirect-then-the-api.md
---

`grep -rn ErrNoSource source` -> 0 hits. config.go resolve() sets Source to NewGitHubSource when nil (ADR-0016 says ErrNoSource does not exist). Also doc/candidate 20261016-update-dev-mode... "Source minimal shape" is outdated.
