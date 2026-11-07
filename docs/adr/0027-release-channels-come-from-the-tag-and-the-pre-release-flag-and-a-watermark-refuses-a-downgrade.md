# Release channels come from the tag and the pre-release flag, and a watermark refuses a downgrade

Status: amends ADR-0016, whose pre-release rule reads the tag alone and whose `IncludePrereleases`
field is replaced by a channel, and ADR-0024, whose downgrade and replay consequence it settles.

A tool wants to offer its users more than its stable releases: a beta line for the adventurous, an
edge line cut on every merge. It also wants an update never to move a user to an older version than
they have run, because a signed older release is still a valid release (ADR-0024). `update` follows
a **channel** and keeps a **watermark**, the highest version applied, below which it refuses to
install without being asked.

## The rules

- **Three channels, cumulative.** `Stable < Beta < Edge`. A follower of a channel receives every
  release whose own channel is at or below it, so Edge receives all releases and Stable only the
  stable ones. The zero value is Stable.
- **A release's channel is the higher of its tag's and its flag's.** A tag with no pre-release
  suffix is Stable. A tag whose first suffix identifier is `beta` or `rc` is Beta. Any other
  suffix (`edge.147`, `alpha.1`, `dev.3`) is Edge. A release the source marks as a pre-release
  (GitHub `prerelease: true`) is at least Beta. A draft, and a tag that is not a release version,
  is never eligible.
- **Ordering is semver on every channel.** Releases are compared with `cli/spec/version.Newer`
  (ADR-0010), and so is the watermark. An edge tag carries an increasing pre-release number
  (`v1.4.0-edge.147`) so that the order is the one semver defines.
- **The channel is a parameter of every Source.** `Source.Latest(ctx, ch)` returns the highest
  release `ch` receives. `Release` gains no field: the channel is derived from the tag and the
  flag when a source judges eligibility, and nothing downstream needs it.
- **Stable keeps the redirect, then the API** (ADR-0016). Beta and Edge read the release list, 100
  to a page, newest-created first, until a page holds a receivable release, at most 10 pages, or
  until a short or empty page. The highest receivable release across the pages read wins. No
  receivable release within that bound is an error.
- **Selection.** `Config.Channel` sets the channel. `update --channel stable|beta|edge` overrides it
  for one run. A tool that keeps the channel in its own configuration maps the value through
  `ParseChannel`.
- **The background check is per channel.** `State.LatestChannel` records the channel
  `LatestVersion` was learnt on. On another channel the saved version is not announced and the
  throttle counts as elapsed, so a switch is checked at once. Every attempt, a failed one
  included, records its channel; a failed attempt after a switch clears the other channel's
  version. Without that, an unreachable source would leave the old channel recorded and be queried
  on every run after the switch.
- **The watermark is `State.HighestApplied`.** The floor is the higher of it and the running
  `Version`, each ignored when it is not a release version. A tag strictly below the floor is a
  `*DowngradeError`; a tag equal to it is allowed; a tag that is not a release version is below
  every floor. A rollback never lowers the watermark. A corrupt or deleted state file resets it,
  and the floor falls back to the running `Version`; an older build of the tool that rewrites the
  file drops the key.
- **The guard fails closed, the record fails open.** `CheckDowngrade` refuses when the state cannot
  be read, rather than guess at the floor. Recording the watermark after `Install` ignores a failed
  lock, load or save: the update has happened, and the new binary's `Version` floors the next run.
  `AllowDowngrade` skips the guard and its read.
- **An explicit ask lifts the guard.** `update.AllowDowngrade()` on `Apply` and `--allow-downgrade`
  on the command. `--to` alone does not: naming a version is how a user updates to a release that
  is not the latest, and it must not also mean going back.
- **The floor decides "is there an update" everywhere.** The command runs the guard before its
  confirmation, on the latest path and on the `--to` path. `--check` reports up to date for a latest
  below the floor. The background `Notice` never names a release below `HighestApplied`.
- **Switching to a lower channel never downgrades.** A stable tool running `v1.3.0-beta.2` stays
  there until a stable release sorts above it.

## Considered options

- **A release manifest** naming the release of each channel. It needs a second published artifact
  to be kept in step with the releases and to be signed (ADR-0024), where the tag and the flag a
  release already carries say the same.
- **Ordering edge releases by recency** (tumika ADR-0007,
  https://github.com/tumika/tumika/blob/HEAD/docs/adr/0007-update-channels-and-recency.md). A
  second ordering beside semver, and one the watermark would have to follow. An increasing
  pre-release number on the edge tag keeps one order for every channel.
- **`--to` as the explicit ask.** It makes the common act of pinning a release also the act of
  downgrading, so a mistyped version installs an older release without a refusal.
- **The channel as a `GitHubSource` field** (`IncludePrereleases`). The channel belongs to the
  installation, not the source: the command's `--channel` and the background check would each have
  to build another source, and a custom `Source` would have no common way to receive it.
- **Failing `Apply` when the record fails.** The binary is already replaced; reporting an error
  for an update that took effect invites a retry of an install that has happened, and the new
  `Version` floors the next run anyway.
- **Reading only the first list page.** The list is newest-created first, so a run of frequent
  edge releases would fill the page and starve Beta of its latest release.

## Consequences

- **`Source.Latest` takes a channel.** A custom `Source` changes its signature and returns the
  highest release the channel receives.
- **`State` gains two keys,** `latest_channel` and `highest_applied`. A file without them reads as
  a version learnt on Stable and no watermark.
- **The watermark is advisory.** It lives in a file the user owns, and deleting it lowers the floor
  to the running version. It protects against a mistaken or stale update, not against a user, and
  not against an attacker who can write the state directory.
- **A beta or edge follower pays more requests,** up to ten list pages, only when releases it does
  not receive are newer than every one it does.
