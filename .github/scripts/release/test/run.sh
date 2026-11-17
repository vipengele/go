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
