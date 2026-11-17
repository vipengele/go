---
about: "dependabot-gomod-entry-per-project: body is out of date (there are now per-project gomod entries) but the rule still holds"
saw:
  - .gt-repo.yaml
targets: dependabot-gomod-entry-per-project
verdict: now-false
---
The note says only `directory: "/source/cli"` exists at `.github/dependabot.yml:28`. `.gt-repo.yaml` now lists a `gomod` entry per module (`/source/cli/core`, `/source/update`, `/source/daemon`, `/source/commons`, `/source/cli/spec`, `/source/config`, `/source/cli/state`, `/source/filelock`, `/source/examples/greet`) plus github-actions and npm (`/tools/vanity-worker`). The rule (add entry to `.gt-repo.yaml`, run `gt repo sync`) is still true; the factual body and its `:28` pointer are not. Checked by `cat .gt-repo.yaml`.
