---
about: "gt-managed workflows and dependabot.yml are rendered from .gt-repo.yaml; hand edits are lost and gt's shared policy is deliberately not pinned there"
saw:
  - .gt-repo.yaml
  - .github/workflows/gt-sync.yml
  - .github/workflows/gt-lydite-clearance.yml
  - .github/workflows/dependabot-auto-merge.yml
  - .github/dependabot.yml
---

Kind: invariant (with gotcha).

Each of those files opens "Managed by gt - edit .gt-repo.yaml, then run `gt repo sync`". `.gt-repo.yaml:13-16` says only deliberate overrides belong there; a value that restates a gt default is removed by sync, and a pinned value stops tracking gt. Cooldown (7 days), commit prefixes (ci / build) and schedules live in gt's templates, not in the repo.

Breaks when wrong: editing a managed file directly is overwritten on the next `gt repo sync` (and by the weekly gt-sync PR). Adding a gomod project needs BOTH a `dependabot:` entry in `.gt-repo.yaml:19-23` (currently only /source/cli) followed by `gt repo sync`; otherwise that module gets no Dependabot updates. Also `ci-orchestration.yml` is created once by gt and never touched again (`.gt-repo.yaml:8-11`), so it is hand-owned, unlike the gt-*.yml files.
