---
about: ".gt.yaml setup template runs agtk sync differently for worktrees and the initial clone; clone reads config from the default branch"
saw:
  - .gt.yaml
  - go.work
---

Kind: gotcha.

`.gt.yaml:9-14`: when GT_SETUP_PHASE is `worktree`, `agtk sync` runs from cwd (the worktree has its own checkout). Otherwise (clone) it does `cd "$GT_ROOT"` and runs `agtk sync --config "${GT_ROOT}/${GT_DEFAULT_BRANCH}/.agentic-toolkit.yaml"`, so the rendered .claude lands next to `.bare/` at the gt root and config/lockfile come from main. Consequence: agent config changes on a feature branch are not seen by the clone-phase render, and `sync` (not `render`) fetches over the network here. The installer is curl-piped from agentic-toolkit main if agtk is absent.

`go.work` lists `use ./source/cli` only; a new project needs its own `use` line, but CI never reads go.work (GOWORK=off), so a missing line is invisible to CI and only breaks local multi-module work.
