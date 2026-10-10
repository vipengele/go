---
about: "dependabot.yml has a gomod entry only for /source/cli; a new project gets no Go dependency updates unless .gt-repo.yaml gains an entry"
saw:
  - .github/dependabot.yml
---

Kind: gotcha.

dependabot.yml is gt-managed ("edit .gt-repo.yaml, then `gt repo sync`"). Its `gomod` update has
`directory: "/source/cli"` (line 195) - one entry per module, since Dependabot does not discover
nested modules. A new project under `source/<project>/` is not covered until a `gomod` entry for
it is added in `.gt-repo.yaml` and synced (CLAUDE.md names this step; hand-editing the YAML is lost).
Commit prefixes `ci` / `build` with `include: scope` exist so squash-merged PR titles stay
Conventional Commits; the 7-day cooldown is a supply-chain guard, not a throttle.

What breaks: silent absence of dependency updates for new modules; non-conforming PR titles if the prefix is changed.
Evidence: reading dependabot.yml.
