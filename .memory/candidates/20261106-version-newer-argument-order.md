---
about: "cli/spec/version.Newer(current, latest) is true when latest is newer: the first argument is the baseline, a non-release latest is never newer, and a non-release current makes any release newer"
saw:
  - source/cli/spec/version/version.go
  - source/update/downgrade.go
  - source/update/background.go
---

`Newer(current, latest string) bool` (source/cli/spec/version/version.go:21) reports whether
`latest` sorts above `current`. The asymmetry is easy to invert when writing a guard:

- `Newer(x, tag)` asks whether tag is above x. "Refuse tag below floor" is `Newer(tag, floor)`
  (downgrade.go, CheckDowngrade), and "skip a candidate below the watermark" is
  `Newer(candidate, watermark)` (background.go, Notice).
- A `latest` that is not a release version is never newer, so `Newer(x, "")` is false. An empty
  watermark therefore never suppresses a notice.
- A `current` that is not a release version makes every release newer, so `Newer("", tag)` is
  true. That is why downgradeFloor (downgrade.go:43) filters both inputs through IsRelease before
  comparing, and why a non-release tag is below every floor.
