#!/usr/bin/env bash
# run.sh — the self-test of the release scripts in .github/scripts/release.
#
# Usage: run.sh [case-name...]
#
# Each case builds its own git repository in a temporary directory, runs one
# script against it and asserts on the result. Offline and quick: no network,
# no fetch, no Go toolchain download. Prints one PASS or FAIL line per case and
# exits non-zero when any case fails. With arguments, only the named cases run.
#
# A case is a function registered with `register <name> <function>`; each
# script under test has its own section below.
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
scripts="$(cd "$here/.." && pwd)"

# shellcheck source=fixture.sh
source "$here/fixture.sh"

export GOWORK=off GOTOOLCHAIN=local GOFLAGS=-mod=mod GOPROXY=off

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

cases=()
declare -A case_fn=()

# register <name> <function>: add a case.
register() {
  cases+=("$1")
  case_fn[$1]="$2"
}

# fail <message>: fail the current case.
fail() {
  printf '  %s\n' "$*" >&2
  return 1
}

# --- pending.sh ---------------------------------------------------------------

# pending [releases...]: run pending.sh on $FIXTURE with its local tags and the
# given Release tags. Sets $out, $err and $status.
pending() {
  local releases="$FIXTURE.releases"
  printf '%s\n' "$@" | grep -v '^$' > "$releases" || true
  status=0
  out="$("$scripts/pending.sh" "$FIXTURE" "$(fixture_tags_file)" "$releases" 2> "$FIXTURE.err")" || status=$?
  err="$(cat "$FIXTURE.err")"
}

# expect_json <jq-expected>: pending.sh succeeded and printed, after `jq -S`,
# the JSON given in jq syntax.
expect_json() {
  local want got
  ((status == 0)) || fail "pending.sh exited $status: $err" || return 1
  want="$(jq -nSc "$1")"
  got="$(jq -Sc . <<< "$out")" || fail "output is not JSON: $out" || return 1
  [[ "$got" == "$want" ]] || fail "want $want" "got  $got" || return 1
}

# expect_error <pattern>: pending.sh failed and its stderr matches the pattern.
expect_error() {
  ((status != 0)) || fail "pending.sh succeeded, printing $out" || return 1
  grep -qE "$1" <<< "$err" || fail "stderr does not match /$1/: $err" || return 1
}

pending_empty_set() {
  fixture_repo empty
  fixture_project a
  fixture_commit
  pending
  expect_json '{tiers: [], unannounced: []}'
}
register "pending: an empty set" pending_empty_set

pending_no_projects() {
  fixture_repo no-projects
  fixture_commit
  pending
  expect_json '{tiers: [], unannounced: []}'
}
register "pending: a repository with no project" pending_no_projects

pending_one_project() {
  fixture_repo one
  fixture_project cli/core
  fixture_notes cli/core v0.1.0
  fixture_commit
  pending
  expect_json '{tiers: [[{project: "cli/core", version: "v0.1.0"}]], unannounced: []}'
}
register "pending: one project" pending_one_project

pending_chain() {
  fixture_repo chain
  fixture_project commons
  fixture_project cli/spec commons
  fixture_project update cli/spec
  for p in commons cli/spec update; do fixture_notes "$p" v0.2.0; done
  fixture_commit
  pending
  expect_json '{tiers: [[{project: "commons", version: "v0.2.0"}],
                        [{project: "cli/spec", version: "v0.2.0"}],
                        [{project: "update", version: "v0.2.0"}]],
                unannounced: []}'
}
register "pending: a 3-deep chain in tier order" pending_chain

pending_through_released() {
  fixture_repo through-released
  fixture_project a
  fixture_project b a
  fixture_project c b
  fixture_notes b v0.1.0
  fixture_commit
  fixture_tag source/b/v0.1.0
  fixture_notes a v0.1.0
  fixture_notes c v0.1.0
  fixture_commit
  pending source/b/v0.1.0
  expect_json '{tiers: [[{project: "a", version: "v0.1.0"}], [{project: "c", version: "v0.1.0"}]],
                unannounced: []}'
}
register "pending: a dependency through a released project still orders" pending_through_released

pending_major_suffix() {
  fixture_repo major-suffix
  fixture_project a
  fixture_project b a/v2
  fixture_notes a v0.1.0
  fixture_notes b v0.1.0
  fixture_commit
  pending
  expect_json '{tiers: [[{project: "a", version: "v0.1.0"}], [{project: "b", version: "v0.1.0"}]],
                unannounced: []}'
}
register "pending: a require with a major suffix is an edge to its project" pending_major_suffix

pending_independent() {
  fixture_repo independent
  fixture_project config
  fixture_project commons
  fixture_project filelock
  fixture_notes filelock v1.0.0
  fixture_notes commons v0.3.0
  fixture_notes config v0.1.1
  fixture_commit
  pending
  expect_json '{tiers: [[{project: "commons", version: "v0.3.0"},
                         {project: "config", version: "v0.1.1"},
                         {project: "filelock", version: "v1.0.0"}]],
                unannounced: []}'
}
register "pending: independent projects share one sorted tier" pending_independent

pending_unannounced() {
  fixture_repo unannounced
  fixture_project a
  fixture_project b
  fixture_notes a v0.1.0
  fixture_notes b v0.1.0
  fixture_commit
  fixture_tag source/a/v0.1.0
  fixture_tag source/b/v0.1.0
  pending source/b/v0.1.0
  expect_json '{tiers: [], unannounced: ["source/a/v0.1.0"]}'
}
register "pending: a tag with notes and no Release is unannounced" pending_unannounced

pending_tag_without_notes() {
  fixture_repo tag-without-notes
  fixture_project spike/probe
  fixture_commit
  fixture_tag source/spike/probe/v0.0.1
  fixture_tag tools/vanity-worker/v1.0.0
  pending
  expect_json '{tiers: [], unannounced: []}'
}
register "pending: a tag without notes is ignored" pending_tag_without_notes

pending_tag_commit_lacks_notes() {
  fixture_repo tag-commit-lacks-notes
  fixture_project a
  fixture_commit
  fixture_tag source/a/v0.1.0
  fixture_notes a v0.1.0
  fixture_commit "notes after the tag"
  pending
  expect_json '{tiers: [], unannounced: []}'
}
register "pending: a tag whose commit lacks its notes is not unannounced" pending_tag_commit_lacks_notes

pending_notes_without_project() {
  fixture_repo notes-without-project
  fixture_project cli/core
  fixture_notes gone v0.1.0
  fixture_notes cli v0.1.0
  fixture_notes cli/core/extra v0.1.0
  mkdir -p "$FIXTURE/docs/release-notes/cli/core"
  printf 'notes index\n' > "$FIXTURE/docs/release-notes/cli/core/README.md"
  fixture_commit
  pending
  expect_json '{tiers: [], unannounced: []}'
}
register "pending: notes outside a project, or not named for a version, are ignored" pending_notes_without_project

pending_ls_remote_input() {
  fixture_repo ls-remote-input
  fixture_project a
  fixture_project b
  fixture_notes a v0.1.0
  fixture_notes b v0.2.0
  fixture_commit
  fixture_tag source/a/v0.1.0
  local tags="$FIXTURE.remote" releases="$FIXTURE.releases"
  fixture_git for-each-ref --format='%(objectname)%09%(refname)' refs/tags > "$tags"
  fixture_git for-each-ref --format='%(*objectname)%09%(refname)^{}' refs/tags >> "$tags"
  : > "$releases"
  status=0
  out="$("$scripts/pending.sh" "$FIXTURE" "$tags" "$releases" 2> "$FIXTURE.err")" || status=$?
  err="$(cat "$FIXTURE.err")"
  expect_json '{tiers: [[{project: "b", version: "v0.2.0"}]], unannounced: ["source/a/v0.1.0"]}'
}
register "pending: reads git ls-remote output, peeled lines included" pending_ls_remote_input

pending_tag_not_fetched() {
  fixture_repo tag-not-fetched
  fixture_project a
  fixture_commit
  local tags="$FIXTURE.remote" releases="$FIXTURE.releases"
  printf 'source/a/v0.1.0\n' > "$tags"
  : > "$releases"
  status=0
  out="$("$scripts/pending.sh" "$FIXTURE" "$tags" "$releases" 2> "$FIXTURE.err")" || status=$?
  err="$(cat "$FIXTURE.err")"
  expect_error 'source/a/v0.1.0 is listed but not present'
}
register "pending: a listed tag missing from the checkout is an error" pending_tag_not_fetched

pending_two_versions() {
  fixture_repo two-versions
  fixture_project a
  fixture_notes a v0.1.0
  fixture_notes a v0.2.0
  fixture_commit
  pending
  expect_error 'a has more than one pending version: v0.1.0, v0.2.0'
}
register "pending: two pending versions of one project is an error" pending_two_versions

pending_cycle() {
  fixture_repo cycle
  fixture_project a c
  fixture_project b a
  fixture_project c b
  fixture_project d
  fixture_notes d v0.1.0
  fixture_commit
  pending
  expect_error 'dependency cycle among projects: a, b, c$'
}
register "pending: a dependency cycle is an error" pending_cycle

# chain_of <n>: projects p1 … p<n>, each requiring the one before, all pending.
chain_of() {
  local i
  fixture_project p1
  fixture_notes p1 v0.1.0
  for ((i = 2; i <= $1; i++)); do
    fixture_project "p$i" "p$((i - 1))"
    fixture_notes "p$i" v0.1.0
  done
  fixture_commit
}

pending_depth_eight() {
  fixture_repo depth-eight
  chain_of 8
  pending
  expect_json '{tiers: [range(1; 9) | [{project: "p\(.)", version: "v0.1.0"}]], unannounced: []}'
}
register "pending: 8 tiers are accepted" pending_depth_eight

pending_depth_nine() {
  fixture_repo depth-nine
  chain_of 9
  pending
  expect_error 'pending projects are 9 tiers deep, more than 8: p9'
}
register "pending: more than 8 tiers is an error" pending_depth_nine

# --- release-project.sh -------------------------------------------------------

fixture_shims

# release_project <args...>: run release-project.sh on $FIXTURE, with the
# stubs of gh and `go list` first on PATH, short retry budgets and no host git
# config. Sets $out, $err and $status; stub calls go to $STUB_LOG.
release_project() {
  export STUB_LOG="$FIXTURE.calls"
  touch "$STUB_LOG"
  status=0
  out="$(PATH="$WORK/bin:$PATH" RELEASE_ROOT="$FIXTURE" \
    RELEASE_VANITY_BASE="${RELEASE_VANITY_BASE:-http://127.0.0.1:9}" \
    RELEASE_WORKER_TIMEOUT=1 RELEASE_WORKER_INTERVAL=0.2 \
    RELEASE_PROXY_ATTEMPTS=3 RELEASE_PROXY_INTERVAL=0 \
    GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_NOSYSTEM=1 \
    "$scripts/release-project.sh" "$@" 2> "$FIXTURE.err")" || status=$?
  err="$(cat "$FIXTURE.err")"
}

# expect_ok: release-project.sh succeeded.
expect_ok() {
  ((status == 0)) || fail "release-project.sh exited $status: $err" || return 1
}

# expect_fail <pattern>: release-project.sh failed and its stderr matches the
# pattern.
expect_fail() {
  ((status != 0)) || fail "release-project.sh succeeded, printing $out" || return 1
  grep -qE "$1" <<< "$err" || fail "stderr does not match /$1/: $err" || return 1
}

# expect_calls <expected>: the stub calls, one per line, are exactly these.
expect_calls() {
  local got
  got="$(cat "$STUB_LOG")"
  [[ "$got" == "$1" ]] || fail "want calls:" "$1" "got calls:" "$got" || return 1
}

# releasable: a fixture with origin, where cli/core requires commons, commons
# is tagged v0.1.0 on origin, and the notes of cli/core v0.1.0 are on main.
releasable() {
  fixture_repo "$1"
  fixture_origin
  fixture_project commons
  fixture_notes commons v0.1.0
  fixture_commit "commons"
  fixture_tag source/commons/v0.1.0
  mkdir -p "$FIXTURE/source/cli/core"
  printf 'module go.vipengele.org/cli/core\n\ngo 1.25.0\n\nrequire go.vipengele.org/commons v0.1.0\n' \
    > "$FIXTURE/source/cli/core/go.mod"
  fixture_notes cli/core v0.1.0
  fixture_commit "cli/core"
  fixture_push refs/tags/source/commons/v0.1.0
}

validate_passes() {
  releasable validate-passes
  fixture_worker cli/core
  release_project validate cli/core v0.1.0 "$(fixture_git rev-parse HEAD)"
  expect_ok
  grep -q 'source/cli/core/v0.1.0 at .* is valid' <<< "$out" || fail "no success line: $out"
}
register "release-project: validate passes a releasable commit" validate_passes

validate_replace() {
  releasable validate-replace
  printf '\nreplace go.vipengele.org/commons => ../../commons\n' >> "$FIXTURE/source/cli/core/go.mod"
  fixture_commit "replace"
  fixture_push
  release_project validate cli/core v0.1.0 "$(fixture_git rev-parse HEAD)"
  expect_fail 'replace of go.vipengele.org/commons -> \.\./\.\./commons: a consumer'
}
register "release-project: validate fails a replace" validate_replace

validate_untagged_sibling() {
  releasable validate-untagged-sibling
  # The sibling's tag exists in the checkout but not on origin, which is
  # where a consumer resolves it.
  fixture_tag source/commons/v0.2.0
  sed -i 's/commons v0.1.0/commons v0.2.0/' "$FIXTURE/source/cli/core/go.mod"
  fixture_commit "require an unreleased commons"
  fixture_push
  release_project validate cli/core v0.1.0 "$(fixture_git rev-parse HEAD)"
  expect_fail 'go.vipengele.org/commons v0.2.0 has no tag source/commons/v0.2.0 on origin'
}
register "release-project: validate fails a sibling untagged on origin" validate_untagged_sibling

validate_pseudo_version() {
  releasable validate-pseudo-version
  sed -i 's/commons v0.1.0/commons v0.1.1-0.20261117043900-0123456789ab/' "$FIXTURE/source/cli/core/go.mod"
  fixture_commit "pseudo-version"
  fixture_push
  release_project validate cli/core v0.1.0 "$(fixture_git rev-parse HEAD)"
  expect_fail 'required at the pseudo-version v0.1.1-0.20261117043900-0123456789ab'
}
register "release-project: validate fails a sibling at a pseudo-version" validate_pseudo_version

validate_module_path() {
  releasable validate-module-path
  sed -i 's|^module .*|module go.vipengele.org/core|' "$FIXTURE/source/cli/core/go.mod"
  fixture_commit "wrong module path"
  fixture_push
  release_project validate cli/core v0.1.0 "$(fixture_git rev-parse HEAD)"
  expect_fail 'module path is go.vipengele.org/core, expected go.vipengele.org/cli/core'
}
register "release-project: validate fails a module path off the vanity map" validate_module_path

# major_project <name> <module>: a fixture with origin, where source/a declares
# <module> and the notes of a v2.0.0 and a v1.1.0 are on main.
major_project() {
  fixture_repo "$1"
  fixture_origin
  mkdir -p "$FIXTURE/source/a"
  printf 'module %s\n\ngo 1.25.0\n' "$2" > "$FIXTURE/source/a/go.mod"
  fixture_notes a v2.0.0
  fixture_notes a v1.1.0
  fixture_commit "a"
  fixture_push
}

validate_major_suffix() {
  major_project validate-major-suffix go.vipengele.org/a/v2
  fixture_worker a
  release_project validate a v2.0.0 "$(fixture_git rev-parse HEAD)"
  expect_ok
}
register "release-project: validate passes a /v2 module path at v2.0.0" validate_major_suffix

validate_major_suffix_at_v1() {
  major_project validate-major-suffix-at-v1 go.vipengele.org/a/v2
  release_project validate a v1.1.0 "$(fixture_git rev-parse HEAD)"
  expect_fail 'module path is go.vipengele.org/a/v2, expected go.vipengele.org/a for v1.1.0'
}
register "release-project: validate fails a /v2 module path at v1.x" validate_major_suffix_at_v1

validate_major_suffix_missing() {
  major_project validate-major-suffix-missing go.vipengele.org/a
  release_project validate a v2.0.0 "$(fixture_git rev-parse HEAD)"
  expect_fail 'module path is go.vipengele.org/a, expected go.vipengele.org/a/v2 for v2.0.0'
}
register "release-project: validate fails a v2.0.0 module path without /v2" validate_major_suffix_missing

validate_major_sibling() {
  fixture_repo validate-major-sibling
  fixture_origin
  mkdir -p "$FIXTURE/source/commons"
  printf 'module go.vipengele.org/commons/v2\n\ngo 1.25.0\n' > "$FIXTURE/source/commons/go.mod"
  fixture_notes commons v2.0.0
  fixture_commit "commons"
  fixture_tag source/commons/v2.0.0
  fixture_project cli/core commons/v2
  fixture_notes cli/core v0.1.0
  fixture_commit "cli/core"
  fixture_push refs/tags/source/commons/v2.0.0
  fixture_worker cli/core
  release_project validate cli/core v0.1.0 "$(fixture_git rev-parse HEAD)"
  expect_ok
}
register "release-project: validate passes a sibling required at /v2 and tagged source/<sibling>/v2.0.0" validate_major_sibling

validate_major_sibling_untagged() {
  fixture_repo validate-major-sibling-untagged
  fixture_origin
  mkdir -p "$FIXTURE/source/commons"
  printf 'module go.vipengele.org/commons/v2\n\ngo 1.25.0\n' > "$FIXTURE/source/commons/go.mod"
  fixture_project cli/core commons/v2
  fixture_notes cli/core v0.1.0
  fixture_commit "cli/core"
  fixture_push
  release_project validate cli/core v0.1.0 "$(fixture_git rev-parse HEAD)"
  expect_fail 'go.vipengele.org/commons/v2 v2.0.0 has no tag source/commons/v2.0.0 on origin'
}
register "release-project: validate fails a /v2 sibling untagged on origin" validate_major_sibling_untagged

validate_missing_gomod() {
  releasable validate-missing-gomod
  fixture_notes absent v0.1.0
  fixture_commit "notes without a project"
  fixture_push
  release_project validate absent v0.1.0 "$(fixture_git rev-parse HEAD)"
  expect_fail 'source/absent/go.mod does not exist at'
}
register "release-project: validate fails a project with no go.mod at the commit" validate_missing_gomod

validate_inside_module() {
  releasable validate-inside-module
  fixture_project cli
  fixture_commit "enclosing module"
  fixture_push
  release_project validate cli/core v0.1.0 "$(fixture_git rev-parse HEAD)"
  expect_fail 'source/cli/core is nested inside the module at source/cli'
}
register "release-project: validate fails a project inside another module" validate_inside_module

validate_missing_notes() {
  releasable validate-missing-notes
  release_project validate cli/core v0.2.0 "$(fixture_git rev-parse HEAD)"
  expect_fail 'docs/release-notes/cli/core/v0.2.0.md is missing'
}
register "release-project: validate fails missing notes" validate_missing_notes

validate_nested_module() {
  releasable validate-nested-module
  fixture_project cli/core/inner
  fixture_commit "nested module"
  fixture_push
  release_project validate cli/core v0.1.0 "$(fixture_git rev-parse HEAD)"
  expect_fail 'source/cli/core contains another module at source/cli/core/inner'
}
register "release-project: validate fails a project holding another module" validate_nested_module

validate_not_on_main() {
  releasable validate-not-on-main
  fixture_git checkout -q -b feature
  fixture_notes cli/core v0.2.0
  fixture_commit "notes off main"
  fixture_push
  release_project validate cli/core v0.2.0 "$(fixture_git rev-parse HEAD)"
  expect_fail 'is not on origin/main'
}
register "release-project: validate fails a commit not on main" validate_not_on_main

validate_tag_exists() {
  releasable validate-tag-exists
  fixture_tag source/cli/core/v0.1.0
  fixture_push refs/tags/source/cli/core/v0.1.0
  fixture_git tag -d source/cli/core/v0.1.0 > /dev/null
  release_project validate cli/core v0.1.0 "$(fixture_git rev-parse HEAD)"
  expect_fail 'source/cli/core/v0.1.0 already exists on origin'
}
register "release-project: validate fails a tag already on origin" validate_tag_exists

validate_prerelease() {
  releasable validate-prerelease
  fixture_notes cli/core v0.2.0-rc.1
  fixture_commit "prerelease notes"
  fixture_push
  release_project validate cli/core v0.2.0-rc.1 "$(fixture_git rev-parse HEAD)"
  expect_fail 'source/cli/core/v0.2.0-rc.1 carries a prerelease suffix'
}
register "release-project: validate fails a prerelease suffix" validate_prerelease

validate_malformed() {
  releasable validate-malformed
  release_project validate cli/core 0.1.0 "$(fixture_git rev-parse HEAD)"
  expect_fail 'source/cli/core/0.1.0 is not source/<project>/vX.Y.Z'
}
register "release-project: validate fails a version that is not vX.Y.Z" validate_malformed

validate_worker_silent() {
  releasable validate-worker-silent
  fixture_worker commons
  release_project validate cli/core v0.1.0 "$(fixture_git rev-parse HEAD)"
  expect_fail '/cli/core\?go-get=1 does not answer for go.vipengele.org/cli/core after 1s'
}
register "release-project: validate fails when the Worker does not answer" validate_worker_silent

tag_pushes() {
  releasable tag-pushes
  local sha bare="$FIXTURE.origin.git" tag=source/cli/core/v0.1.0 body
  sha="$(fixture_git rev-parse HEAD)"
  release_project tag cli/core v0.1.0 "$sha" https://github.com/vipengele/go/actions/runs/42
  expect_ok || return 1
  [[ "$(git -C "$bare" cat-file -t "refs/tags/$tag")" == tag ]] || fail "$tag on origin is not an annotated tag" || return 1
  [[ "$(git -C "$bare" rev-parse "refs/tags/$tag^{commit}")" == "$sha" ]] || fail "$tag on origin is not at $sha" || return 1
  body="$(git -C "$bare" cat-file -p "refs/tags/$tag")"
  grep -q '^tagger github-actions\[bot\] <41898282+github-actions\[bot\]@users.noreply.github.com>' <<< "$body" \
    || fail "tagger is not github-actions[bot]:" "$body" || return 1
  grep -q 'https://github.com/vipengele/go/actions/runs/42' <<< "$body" || fail "no run URL:" "$body" || return 1
  ! grep -q 'BEGIN' <<< "$body" || fail "the tag is signed:" "$body" || return 1
  [[ -z "$(fixture_git config --get user.name || true)" ]] || fail "the bot identity was written to the config" || return 1

  # The same call again fails, and so does a push of that tag from a checkout
  # that lacks it: origin refuses to move a tag.
  release_project tag cli/core v0.1.0 "$sha" https://github.com/vipengele/go/actions/runs/43
  expect_fail 'cannot create the tag|rejected' || return 1
  fixture_git tag -d "$tag" > /dev/null
  release_project tag cli/core v0.1.0 "$(fixture_git rev-parse HEAD~1)" https://github.com/vipengele/go/actions/runs/44
  expect_fail "origin rejected $tag" || return 1
  [[ "$(git -C "$bare" rev-parse "refs/tags/$tag^{commit}")" == "$sha" ]] || fail "$tag on origin moved"
}
register "release-project: tag pushes an annotated bot tag and refuses a second" tag_pushes

# announceable <name> <version>: a fixture with the tag source/a/<version> on
# origin.
announceable() {
  fixture_repo "announce-$1"
  shift
  fixture_origin
  fixture_project a
  fixture_notes a "$1"
  printf 'The notes of a %s.\n' "$1" >> "$FIXTURE/docs/release-notes/a/$1.md"
  fixture_commit
  fixture_tag "source/a/$1"
  fixture_push "refs/tags/source/a/$1"
}

announce_creates_prerelease() {
  announceable prerelease v0.3.0
  # The checkout lacks the tag; announce fetches it from origin.
  fixture_git tag -d source/a/v0.3.0 > /dev/null
  release_project announce source/a/v0.3.0
  expect_ok || return 1
  grep -qx 'gh release create source/a/v0.3.0 --verify-tag --title a v0.3.0 --notes-file .*\.md --prerelease' "$STUB_LOG" \
    || fail "no prerelease create:" "$(cat "$STUB_LOG")" || return 1
  [[ "$(head -2 "$STUB_LOG")" == $'go list -m go.vipengele.org/a@v0.3.0\ngh release view source/a/v0.3.0' ]] \
    || fail "the proxy and the Release are not checked first:" "$(cat "$STUB_LOG")" || return 1
  grep -q 'The notes of a v0.3.0.' "$STUB_LOG.notes" || fail "the notes are not the tag's"
}
register "release-project: announce creates a prerelease for v0.x from the tag's notes" announce_creates_prerelease

announce_stable() {
  announceable stable v1.0.0
  release_project announce source/a/v1.0.0
  expect_ok || return 1
  grep -qx 'gh release create source/a/v1.0.0 --verify-tag --title a v1.0.0 --notes-file .*\.md' "$STUB_LOG" \
    || fail "no stable create:" "$(cat "$STUB_LOG")"
}
register "release-project: announce creates a full release for v1+" announce_stable

announce_major_suffix() {
  fixture_repo announce-major-suffix
  fixture_origin
  mkdir -p "$FIXTURE/source/a"
  printf 'module go.vipengele.org/a/v2\n\ngo 1.25.0\n' > "$FIXTURE/source/a/go.mod"
  fixture_notes a v2.0.0
  fixture_commit
  fixture_tag source/a/v2.0.0
  fixture_push refs/tags/source/a/v2.0.0
  release_project announce source/a/v2.0.0
  expect_ok || return 1
  [[ "$(head -1 "$STUB_LOG")" == 'go list -m go.vipengele.org/a/v2@v2.0.0' ]] \
    || fail "the proxy is not asked for the /v2 module path:" "$(cat "$STUB_LOG")"
}
register "release-project: announce asks the proxy for the /v2 module path of a v2 tag" announce_major_suffix

announce_already_exists() {
  announceable exists v0.1.0
  STUB_GH_CREATE=exists release_project announce source/a/v0.1.0
  expect_ok || return 1
  grep -q 'already exists' <<< "$out" || fail "no already-exists line: $out"
}
register "release-project: announce treats an existing Release as success" announce_already_exists

announce_create_fails() {
  announceable create-fails v0.1.0
  STUB_GH_CREATE=fail release_project announce source/a/v0.1.0
  expect_fail 'gh release create source/a/v0.1.0 failed'
}
register "release-project: announce fails when the Release cannot be created" announce_create_fails

announce_view_exists() {
  announceable view-exists v0.1.0
  STUB_GH_VIEW=0 release_project announce source/a/v0.1.0
  expect_ok || return 1
  expect_calls 'go list -m go.vipengele.org/a@v0.1.0
gh release view source/a/v0.1.0'
}
register "release-project: announce skips create when the Release exists" announce_view_exists

announce_proxy_fails() {
  announceable proxy-fails v0.1.0
  STUB_GO_LIST=1 release_project announce source/a/v0.1.0
  expect_fail 'go.vipengele.org/a@v0.1.0 is not resolvable through the module proxy' || return 1
  expect_calls 'go list -m go.vipengele.org/a@v0.1.0
go list -m go.vipengele.org/a@v0.1.0
go list -m go.vipengele.org/a@v0.1.0'
}
register "release-project: announce fails after the proxy attempts, creating nothing" announce_proxy_fails

finish_check_passes() {
  releasable finish-check-passes
  fixture_tag source/cli/core/v0.1.0
  fixture_push refs/tags/source/cli/core/v0.1.0
  fixture_git tag -d source/cli/core/v0.1.0 > /dev/null
  fixture_worker cli/core
  release_project finish-check source/cli/core/v0.1.0
  expect_ok
}
register "release-project: finish-check passes an existing tag" finish_check_passes

finish_check_local_only() {
  releasable finish-check-local-only
  fixture_tag source/cli/core/v0.1.0
  fixture_worker cli/core
  release_project finish-check source/cli/core/v0.1.0
  expect_fail 'source/cli/core/v0.1.0 does not exist on origin'
}
register "release-project: finish-check fails a tag missing on origin" finish_check_local_only

finish_check_tag_moved() {
  releasable finish-check-tag-moved
  fixture_tag source/cli/core/v0.1.0
  fixture_push refs/tags/source/cli/core/v0.1.0
  fixture_commit "after the tag"
  fixture_push
  fixture_git tag -f -a -m moved source/cli/core/v0.1.0 > /dev/null
  fixture_worker cli/core
  release_project finish-check source/cli/core/v0.1.0
  expect_fail 'points at .*, not at '
}
register "release-project: finish-check fails a tag on origin at another commit than the local tag" finish_check_tag_moved

test_passes() {
  fixture_repo test-passes
  fixture_project a
  fixture_go_source a
  printf 'package fixture\n\n// Windows reports whether the build is for Windows.\nfunc Windows() bool { return true }\n' \
    > "$FIXTURE/source/a/os_windows.go"
  fixture_commit
  release_project test a
  expect_ok || return 1
  grep -qx '==> GOOS=windows go vet ./...' <<< "$out" || fail "no Windows vet: $out" || return 1
  grep -qx '==> go test -race -shuffle=on ./...' <<< "$out" || fail "no test run: $out"
}
register "release-project: test passes a tidy module, vetting it for Windows" test_passes

test_untidy() {
  fixture_repo test-untidy
  fixture_project a
  fixture_go_source a
  # go mod tidy rewrites go.mod in its canonical layout, with a blank line
  # between the module and go lines, so this go.mod is not tidy.
  sed -i "/^$/d" "$FIXTURE/source/a/go.mod"
  fixture_commit
  release_project test a
  expect_fail 'go mod tidy changes go.mod or go.sum of a' || return 1
  ! grep -q 'GOOS=windows' <<< "$out" || fail "Windows vet ran without a Windows file"
}
register "release-project: test fails a module go mod tidy changes" test_untidy

test_build_fails() {
  fixture_repo test-build-fails
  fixture_project a
  fixture_go_source a
  printf 'package fixture\n\nfunc Broken() int { return "not an int" }\n' \
    > "$FIXTURE/source/a/broken.go"
  fixture_commit
  release_project test a
  ((status != 0)) || fail "release-project.sh succeeded, printing $out" || return 1
  grep -q 'not an int' <<< "$out$err" || fail "no compile error in the output: $out $err"
}
register "release-project: test fails a module that does not build" test_build_fails

test_suite_fails() {
  fixture_repo test-suite-fails
  fixture_project a
  fixture_go_source a
  printf 'package fixture\n\nimport "testing"\n\nfunc TestFails(t *testing.T) {\n\tt.Fatal("fails")\n}\n' \
    > "$FIXTURE/source/a/failing_test.go"
  fixture_commit
  release_project test a
  ((status != 0)) || fail "release-project.sh succeeded, printing $out" || return 1
  grep -q 'FAIL' <<< "$out" || fail "no failing test in the output: $out"
}
register "release-project: test fails a module whose suite fails" test_suite_fails

# resolve <mode> <project> <version> <sha> <tag>: run release-project.sh
# resolve with those inputs, from an empty directory that is no repository.
# Sets $out, $err and $status.
resolve() {
  local dir="$WORK/resolve"
  mkdir -p "$dir"
  status=0
  out="$(cd "$dir" && env -u RELEASE_ROOT INPUT_MODE="$1" INPUT_PROJECT="$2" \
    INPUT_VERSION="$3" INPUT_SHA="$4" INPUT_TAG="$5" \
    "$scripts/release-project.sh" resolve 2> "$dir/err")" || status=$?
  err="$(cat "$dir/err")"
}

# expect_resolved <lines>: resolve succeeded and printed exactly these lines.
expect_resolved() {
  ((status == 0)) || fail "release-project.sh exited $status: $err" || return 1
  [[ "$out" == "$1" ]] || fail "want:" "$1" "got:" "$out" || return 1
}

readonly resolve_sha=0123456789abcdef0123456789abcdef01234567

resolve_release() {
  resolve release a v1.2.3 "$resolve_sha" ""
  expect_resolved "mode=release
project=a
version=v1.2.3
tag=source/a/v1.2.3
sha=$resolve_sha
ref=$resolve_sha"
}
register "release-project: resolve names the commit of a release" resolve_release

resolve_nested() {
  resolve release cli/core v0.1.0 "$resolve_sha" ""
  expect_resolved "mode=release
project=cli/core
version=v0.1.0
tag=source/cli/core/v0.1.0
sha=$resolve_sha
ref=$resolve_sha"
}
register "release-project: resolve names a nested project" resolve_nested

resolve_dispatch() {
  resolve "" "" "" "" source/cli/core/v0.1.0
  expect_resolved "mode=finish
project=cli/core
version=v0.1.0
tag=source/cli/core/v0.1.0
sha=
ref=refs/tags/source/cli/core/v0.1.0"
}
register "release-project: resolve finishes the tag of a dispatch" resolve_dispatch

resolve_finish_call() {
  resolve finish a v1.0.0 "" ""
  expect_resolved "mode=finish
project=a
version=v1.0.0
tag=source/a/v1.0.0
sha=
ref=refs/tags/source/a/v1.0.0"
}
register "release-project: resolve finishes the tag a caller names" resolve_finish_call

resolve_not_a_tag() {
  resolve release a v1.2 "$resolve_sha" ""
  expect_fail 'source/a/v1\.2 is not source/<project>/vX\.Y\.Z' || return 1
  resolve release A v1.2.3 "$resolve_sha" ""
  expect_fail 'source/A/v1\.2\.3 is not source/<project>/vX\.Y\.Z' || return 1
  resolve release ../a v1.2.3 "$resolve_sha" ""
  expect_fail 'is not source/<project>/vX\.Y\.Z' || return 1
  [[ -z "$out" ]] || fail "printed $out"
}
register "release-project: resolve fails a project and version that form no tag" resolve_not_a_tag

resolve_split_tag() {
  # source/a/b/v1.0.0 is a release tag, but of project a/b, not of a.
  resolve release a b/v1.0.0 "$resolve_sha" ""
  expect_fail 'project a and version b/v1\.0\.0 do not form one release tag'
}
register "release-project: resolve fails a project and version that form another project's tag" resolve_split_tag

resolve_unknown_mode() {
  resolve publish a v1.2.3 "$resolve_sha" ""
  expect_fail 'mode is publish, expected release or finish'
}
register "release-project: resolve fails an unknown mode" resolve_unknown_mode

resolve_malformed_sha() {
  local sha
  for sha in 0123456 "${resolve_sha^^}" main ""; do
    resolve release a v1.2.3 "$sha" ""
    expect_fail "sha is '${sha}', expected a full commit id" || return 1
  done
}
register "release-project: resolve fails a sha that is not a full commit id" resolve_malformed_sha

resolve_hostile_tag() {
  local tag
  for tag in 'source/a/v1.0.0
ref=main' 'source/a/v1.0.0;id' 'source/$(id)/v1.0.0' ''; do
    resolve "" "" "" "" "$tag"
    expect_fail 'is not source/<project>/vX\.Y\.Z' || return 1
    [[ -z "$out" ]] || fail "printed $out" || return 1
  done
}
register "release-project: resolve fails a tag holding a newline or shell syntax" resolve_hostile_tag

# --- verify-sibling-deps -----------------------------------------------------

verify_script="$(cd "$scripts/../../actions/verify-sibling-deps" && pwd)/verify.sh"

# verify_deps: run the verify-sibling-deps check on $FIXTURE against its
# origin. Sets $out (stdout and stderr) and $status.
verify_deps() {
  status=0
  out="$(GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_NOSYSTEM=1 GITHUB_REPOSITORY= \
    "$verify_script" "$FIXTURE" "$FIXTURE.origin.git" 2>&1)" || status=$?
}

# expect_verified: the check passed.
expect_verified() {
  ((status == 0)) || fail "verify.sh exited $status: $out" || return 1
}

# expect_rejected <pattern>: the check failed, with an error matching the
# pattern.
expect_rejected() {
  ((status != 0)) || fail "verify.sh succeeded, printing $out" || return 1
  grep -E '^::error' <<< "$out" | grep -qE "$1" || fail "no error matches /$1/: $out" || return 1
}

# chain_fixture <name> <commons-version> <replace-target> [mode]: commons is
# tagged v0.1.0 on origin and has the notes of v0.2.0 untagged; cli/core
# requires commons at <commons-version> and, unless the target is empty,
# replaces it by <replace-target>. With mode `strict` cli/core also has
# untagged notes; otherwise its notes are tagged.
chain_fixture() {
  fixture_repo "$1"
  fixture_origin
  fixture_project commons
  fixture_notes commons v0.1.0
  fixture_commit "commons v0.1.0"
  fixture_tag source/commons/v0.1.0
  fixture_notes commons v0.2.0
  mkdir -p "$FIXTURE/source/cli/core"
  {
    printf 'module go.vipengele.org/cli/core\n\ngo 1.25.0\n\nrequire go.vipengele.org/commons %s\n' "$2"
    [[ -z "$3" ]] || printf '\nreplace go.vipengele.org/commons => %s\n' "$3"
  } > "$FIXTURE/source/cli/core/go.mod"
  fixture_notes cli/core v0.1.0
  fixture_commit "cli/core"
  if [[ "${4:-}" != strict ]]; then
    fixture_tag source/cli/core/v0.1.0
  fi
  fixture_push --tags
}

verify_strict_replace_pending() {
  chain_fixture verify-strict-replace-pending v0.2.0 ../../commons strict
  verify_deps
  expect_verified
  grep -q 'cli/core is in strict mode' <<< "$out" || fail "no strict-mode notice: $out"
}
register "verify-sibling-deps: strict mode accepts a directory replace at the sibling's pending version" verify_strict_replace_pending

verify_strict_replace_tagged() {
  chain_fixture verify-strict-replace-tagged v0.1.0 ../../commons strict
  verify_deps
  expect_verified
}
register "verify-sibling-deps: strict mode accepts a directory replace at the sibling's tagged version" verify_strict_replace_tagged

verify_strict_replace_unrelated() {
  chain_fixture verify-strict-replace-unrelated v0.3.0 ../../commons strict
  verify_deps
  expect_rejected 'go.vipengele.org/commons v0.3.0 is replaced by its project directory, but has no tag .* strict mode'
}
register "verify-sibling-deps: strict mode fails a directory replace at a version neither pending nor tagged" verify_strict_replace_unrelated

verify_strict_replace_notes_of_another() {
  # Notes make a version pending only for the project they belong to.
  chain_fixture verify-strict-replace-notes-of-another v0.3.0 ../../commons strict
  fixture_notes cli/core v0.3.0
  fixture_commit "notes of cli/core v0.3.0"
  fixture_push
  verify_deps
  expect_rejected 'go.vipengele.org/commons v0.3.0 is replaced by its project directory'
}
register "verify-sibling-deps: strict mode fails a replace at a version only another project has notes for" verify_strict_replace_notes_of_another

verify_strict_replace_other_directory() {
  chain_fixture verify-strict-replace-other-directory v0.2.0 ../../elsewhere strict
  verify_deps
  expect_rejected 'replace of go.vipengele.org/commons -> \.\./\.\./elsewhere resolves to source/elsewhere'
}
register "verify-sibling-deps: strict mode fails a replace by another directory" verify_strict_replace_other_directory

verify_strict_replace_versioned() {
  chain_fixture verify-strict-replace-versioned v0.2.0 '' strict
  printf '\nreplace go.vipengele.org/commons v0.2.0 => ../../commons\n' >> "$FIXTURE/source/cli/core/go.mod"
  fixture_commit "versioned replace"
  fixture_push
  verify_deps
  expect_rejected 'replace of go.vipengele.org/commons v0.2.0 -> \.\./\.\./commons names a version'
}
register "verify-sibling-deps: strict mode fails a replace naming a version" verify_strict_replace_versioned

verify_strict_no_replace_pending() {
  chain_fixture verify-strict-no-replace-pending v0.2.0 '' strict
  verify_deps
  expect_rejected 'go.vipengele.org/commons v0.2.0 has no tag source/commons/v0.2.0 .* strict mode'
}
register "verify-sibling-deps: strict mode fails a sibling at its pending version without a replace" verify_strict_no_replace_pending

verify_strict_no_replace_tagged() {
  chain_fixture verify-strict-no-replace-tagged v0.1.0 '' strict
  verify_deps
  expect_verified
}
register "verify-sibling-deps: strict mode accepts a tagged sibling without a replace" verify_strict_no_replace_tagged

verify_development_replace_any_version() {
  chain_fixture verify-development-replace v0.3.0 ../../commons
  verify_deps
  expect_verified
  ! grep -q 'cli/core is in strict mode' <<< "$out" || fail "reported strict mode: $out"
}
register "verify-sibling-deps: development mode accepts a directory replace at any version" verify_development_replace_any_version

verify_development_replace_other_directory() {
  chain_fixture verify-development-replace-other v0.1.0 ../../elsewhere
  verify_deps
  expect_rejected 'replace of go.vipengele.org/commons -> \.\./\.\./elsewhere resolves to source/elsewhere'
}
register "verify-sibling-deps: development mode fails a replace by another directory" verify_development_replace_other_directory

verify_development_untagged() {
  chain_fixture verify-development-untagged v0.2.0 ''
  verify_deps
  expect_rejected 'go.vipengele.org/commons v0.2.0 has no tag source/commons/v0.2.0 .* require a tagged release'
}
register "verify-sibling-deps: development mode fails an untagged sibling without a replace" verify_development_untagged

verify_pseudo_version() {
  for mode in development strict; do
    chain_fixture "verify-pseudo-version-$mode" v0.0.0-20261117000000-0123456789ab ../../commons "$mode"
    verify_deps
    expect_rejected 'go.vipengele.org/commons is required at the pseudo-version' || return 1
  done
}
register "verify-sibling-deps: a sibling at a pseudo-version fails in both modes, replaced or not" verify_pseudo_version

verify_non_sibling_replace() {
  chain_fixture verify-non-sibling-replace v0.1.0 '' strict
  printf '\nreplace example.com/other => ../other\n' >> "$FIXTURE/source/cli/core/go.mod"
  fixture_commit "replace a non-sibling"
  fixture_push
  verify_deps
  expect_rejected 'replace of example.com/other -> \.\./other: only a sibling may be replaced'
}
register "verify-sibling-deps: a replace of a non-sibling fails" verify_non_sibling_replace

verify_module_path() {
  chain_fixture verify-module-path v0.1.0 ''
  sed -i 's|^module go.vipengele.org/commons$|module go.vipengele.org/common|' "$FIXTURE/source/commons/go.mod"
  fixture_commit "module path off the vanity map"
  fixture_push
  verify_deps
  expect_rejected 'module path is go.vipengele.org/common, expected go.vipengele.org/commons'
}
register "verify-sibling-deps: a module path off the vanity map fails" verify_module_path

verify_nested_module() {
  chain_fixture verify-nested-module v0.1.0 ''
  fixture_project commons/inner
  fixture_commit "nested module"
  fixture_push
  verify_deps
  expect_rejected 'source/commons/inner/go.mod is inside the project directory of source/commons/go.mod'
}
register "verify-sibling-deps: a project directory holding another go.mod fails" verify_nested_module

verify_remote_unlisted() {
  chain_fixture verify-remote-unlisted v0.1.0 ''
  rm -rf "$FIXTURE.origin.git"
  verify_deps
  expect_rejected 'git ls-remote --tags .* failed'
}
register "verify-sibling-deps: a remote whose tags cannot be listed fails" verify_remote_unlisted

# --- runner -------------------------------------------------------------------

selected=("$@")
((${#selected[@]})) || selected=("${cases[@]}")

failed=0
for name in "${selected[@]}"; do
  fn="${case_fn[$name]:-}"
  if [[ -z "$fn" ]]; then
    echo "FAIL $name (no such case)"
    failed=1
    continue
  fi
  # A case runs in a subshell with errexit in force, so a fixture step that
  # fails fails the case; a case tested by `if` would run with errexit off.
  set +e
  (
    set -e
    "$fn"
  )
  rc=$?
  set -e
  if ((rc == 0)); then
    echo "PASS $name"
  else
    echo "FAIL $name"
    failed=1
  fi
done
exit "$failed"
