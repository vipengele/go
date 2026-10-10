---
name: dependabot-gomod-entry-per-project
kind: gotcha
description: dependabot.yml has one gomod entry per module and is generated from .gt-repo.yaml, so a new source/<project> gets no Go updates until the entry is added there and gt repo sync is run.
anchors:
  - path: .github/dependabot.yml
    blob: 60423088e5bc
  - path: .gt-repo.yaml
    blob: eecd9c0a6f0a
confidence: verified
---

Only `directory: "/source/cli"` (`.github/dependabot.yml:28`) exists; Dependabot does not discover nested modules. The file is gt-managed ("edit .gt-repo.yaml, then run `gt repo sync`"), so the entry goes in `.gt-repo.yaml`'s `dependabot:` list, not the YAML, which hand edits would lose. `.gt-repo.yaml` pins only deliberate overrides: cooldown, commit prefixes and schedules live in gt's templates, and a value restating a gt default is removed by sync. Missing entries fail silently.
