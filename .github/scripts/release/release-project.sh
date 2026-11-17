#!/usr/bin/env bash
# release-project.sh — the steps of one project's release: validate it, test
# it, tag it, announce it, and check a tag whose announcement is unfinished.
#
# Usage:
#   release-project.sh validate <project> <version> <sha>
#   release-project.sh test <project>
#   release-project.sh tag <project> <version> <sha> <run-url>
#   release-project.sh announce <tag>
#   release-project.sh finish-check <tag>
#
# Every subcommand acts on the git repository at $RELEASE_ROOT, the current
# directory when unset. Its remote `origin` is the repository releases are
# pushed to: tags are listed there with `git ls-remote`, never read from the
# local refs, so a tag pushed by a concurrent run counts even when this
# checkout has not fetched it.
#
#   validate      Fails unless <sha> may be released as <project> <version>.
#                 Files are read from the commit object <sha>, not from the
#                 working tree, so the checkout's state does not matter. The
#                 checks: the tag source/<project>/<version> is well formed and
#                 carries no prerelease suffix; source/<project>/go.mod exists
#                 and is neither inside nor around another project; <sha> is
#                 on $RELEASE_MAIN_REF; the notes
#                 docs/release-notes/<project>/<version>.md exist; the go.mod
#                 declares go.vipengele.org/<project>, with the suffix /vN
#                 when <version> is vN.x.y for N >= 2, replaces nothing, and
#                 requires every sibling at a version tagged on origin, never a
#                 pseudo-version; the tag does not exist on origin; and the
#                 vanity Worker answers for the module.
#   test          Runs the project's checks in the working tree, as a consumer
#                 of the tag would build it: go.mod and go.sum are tidy, then
#                 go build, GOOS=windows go vet when the project holds a
#                 *_windows.go or *_windows_test.go file, go vet, and
#                 go test -race -shuffle=on.
#   tag           Creates the unsigned annotated tag on <sha> as
#                 github-actions[bot], with <run-url> in its message, and
#                 pushes that tag alone to origin. A rejected push fails.
#   announce      Asks the module proxy for the tagged version of the module
#                 path the tag's go.mod declares, then creates the GitHub
#                 Release from the notes in the tag's commit. An existing
#                 Release counts as success.
#   finish-check  Runs validate's checks at the commit of <tag>, requiring the
#                 tag on origin at that commit instead of its absence.
#
# Environment:
#   RELEASE_ROOT             the repository; default: the current directory
#   RELEASE_MAIN_REF         the ref a released commit must be on;
#                            default: origin/main
#   RELEASE_VANITY_BASE      the vanity Worker's base URL;
#                            default: https://go.vipengele.org
#   RELEASE_WORKER_TIMEOUT   seconds to wait for the Worker; default: 300
#   RELEASE_WORKER_INTERVAL  seconds between Worker attempts; default: 15
#   RELEASE_PROXY_ATTEMPTS   module proxy attempts; default: 6
#   RELEASE_PROXY_INTERVAL   seconds between proxy attempts; default: 10
#
# GOWORK=off throughout: the root go.work is for local development and a
# release never reads it (ADR-0006).
set -euo pipefail

# shellcheck source=lib.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

export GOWORK=off

readonly bot_name='github-actions[bot]'
readonly bot_email='41898282+github-actions[bot]@users.noreply.github.com'

root="$(cd "${RELEASE_ROOT:-.}" && pwd)"
main_ref="${RELEASE_MAIN_REF:-origin/main}"
vanity_base="${RELEASE_VANITY_BASE:-https://go.vipengele.org}"
worker_timeout="${RELEASE_WORKER_TIMEOUT:-300}"
worker_interval="${RELEASE_WORKER_INTERVAL:-15}"
proxy_attempts="${RELEASE_PROXY_ATTEMPTS:-6}"
proxy_interval="${RELEASE_PROXY_INTERVAL:-10}"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

usage() {
  sed -n 's/^#   release-project.sh /usage: release-project.sh /p' "${BASH_SOURCE[0]}" >&2
  exit 2
}

# project_version <tag>: set $project and $version from a release tag, or die.
project_version() {
  local tag="$1"
  [[ "$tag" =~ $RELEASE_TAG_RE ]] || release_die "$tag is not source/<project>/vX.Y.Z"
  project="${BASH_REMATCH[1]}"
  version="${BASH_REMATCH[3]}"
}

# tag_name <project> <version>: print the release tag, dying unless the two
# form one with exactly that project and version.
tag_name() {
  local tag="source/$1/$2"
  [[ "$tag" =~ $RELEASE_TAG_RE && "${BASH_REMATCH[1]}" == "$1" && "${BASH_REMATCH[3]}" == "$2" ]] \
    || release_die "$tag is not source/<project>/vX.Y.Z"
  printf '%s\n' "$tag"
}

# commit_of <rev>: print the commit <rev> names, or die.
commit_of() {
  git -C "$root" rev-parse -q --verify "$1^{commit}" 2> /dev/null \
    || release_die "$1 is not a commit in $root"
}

# local_tag <tag>: make the tag present locally, fetching it from origin when
# the checkout lacks it.
local_tag() {
  local tag="$1"
  git -C "$root" rev-parse -q --verify "refs/tags/$tag" > /dev/null && return 0
  git -C "$root" fetch -q --no-tags origin "refs/tags/$tag:refs/tags/$tag" \
    || release_die "tag $tag is neither in $root nor on origin"
}

# worker_answers <project>: succeed once the vanity Worker answers a go-get
# request for the project with the go-import meta of its module, retrying for
# up to $worker_timeout seconds. The Worker learns a new project only after
# publish-vanity-modules has run for its merged go.mod (ADR-0016), which can
# trail the merge that released it.
worker_answers() {
  local project="$1" url page content deadline
  local -a fields
  url="${vanity_base}/${project}?go-get=1"
  deadline=$((SECONDS + worker_timeout))
  while :; do
    if page="$(curl -fsS --max-time 10 "$url" 2> /dev/null)"; then
      while IFS= read -r content; do
        content="${content#*content=\"}"
        content="${content%\"}"
        read -r -a fields <<< "$content"
        if [[ "${#fields[@]}" == 4 && "${fields[0]}" == "${RELEASE_MODULE_PREFIX}${project}" \
          && "${fields[1]}" == git && "${fields[3]}" == "source/${project}" ]]; then
          echo "$url answers for ${RELEASE_MODULE_PREFIX}${project}"
          return 0
        fi
      done < <(grep -oE '<meta name="go-import" content="[^"]*"' <<< "$page" || true)
    fi
    if ((SECONDS >= deadline)); then
      return 1
    fi
    echo "$url does not answer for ${RELEASE_MODULE_PREFIX}${project} yet; retrying in ${worker_interval}s"
    sleep "$worker_interval"
  done
}

# check <project> <version> <commit> <mode>: every check of validate, against
# the commit object. In mode `release` the tag must not exist on origin; in
# mode `finish` it must, at <commit>.
#
# Checks that need no go.mod parse fail at once; the go.mod checks are all
# reported before failing, so one run lists everything to fix.
check() {
  local project="$1" version="$2" commit="$3" mode="$4"
  local tag gomod parent nested file notes json want have status=0
  tag="$(tag_name "$project" "$version")"
  gomod="source/${project}/go.mod"

  git -C "$root" cat-file -e "${commit}:${gomod}" 2> /dev/null \
    || release_die "$gomod does not exist at $commit"

  # A project is exactly one module. A go.mod in an enclosing directory makes
  # source/<project> a nested module of another project, and a go.mod below it
  # carves packages out of the module being released.
  parent="$project"
  while [[ "$parent" == */* ]]; do
    parent="${parent%/*}"
    if git -C "$root" cat-file -e "${commit}:source/${parent}/go.mod" 2> /dev/null; then
      release_die "source/${project} is nested inside the module at source/${parent}; a project may not sit inside another project"
    fi
  done
  nested=""
  while IFS= read -r file; do
    if [[ "$file" == "source/${project}/"*/go.mod ]]; then
      nested="$file"
      break
    fi
  done < <(git -C "$root" ls-tree -r --name-only "$commit" -- "source/${project}/")
  [[ -z "$nested" ]] \
    || release_die "source/${project} contains another module at ${nested%/go.mod}; a project may not contain another project"

  git -C "$root" rev-parse -q --verify "${main_ref}^{commit}" > /dev/null \
    || release_die "$main_ref does not exist in $root; fetch it before validating"
  git -C "$root" merge-base --is-ancestor "$commit" "$main_ref" \
    || release_die "$commit is not on $main_ref; only a commit on main is released"

  notes="docs/release-notes/${project}/${version}.md"
  git -C "$root" cat-file -e "${commit}:${notes}" 2> /dev/null \
    || release_die "$notes is missing at $commit; a release carries its notes"

  [[ "$version" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]] \
    || release_die "$tag carries a prerelease suffix; only source/<project>/vX.Y.Z is released"

  # The go.mod at the commit is what every consumer of the tag reads, and a
  # pushed tag is permanent. A consumer ignores a replace while a GOWORK=off
  # build here honours it, so no build catches a leftover one: this does.
  mkdir -p "$tmp/gomod"
  git -C "$root" show "${commit}:${gomod}" > "$tmp/gomod/go.mod"
  if ! json="$(cd "$tmp/gomod" && go mod edit -json go.mod 2>&1)"; then
    release_die "$gomod does not parse: ${json//$'\n'/ }"
  fi

  # Go's import compatibility rule: a module at major version 2 or above
  # carries the suffix /vN in its path, and one at v0 or v1 carries none. The
  # tag stays source/<project>/<version> either way.
  local major
  want="${RELEASE_MODULE_PREFIX}${project}"
  major="${version%%.*}"
  major="${major#v}"
  if ((10#$major >= 2)); then
    want="${want}/v$((10#$major))"
  fi
  have="$(jq -r .Module.Path <<< "$json")"
  if [[ "$have" != "$want" ]]; then
    release_error "$gomod: module path is ${have}, expected ${want} for ${version}; a module at v2 or above ends in /vN for its major version N, one at v0 or v1 has no suffix"
    status=1
  fi

  local old oldv new newv
  while IFS=$'\x1f' read -r old oldv new newv; do
    if [[ "$old" == "$RELEASE_MODULE_PREFIX"* ]]; then
      release_error "$gomod: replace of ${old}${oldv:+ $oldv} -> ${new}${newv:+ $newv}: a consumer of $tag ignores it; drop it (go mod edit -dropreplace=${old}) and require the sibling at a tagged release"
    else
      release_error "$gomod: replace of ${old}${oldv:+ $oldv} -> ${new}${newv:+ $newv}: only a sibling may be replaced, and a released go.mod replaces none"
    fi
    status=1
  done < <(jq -r '(.Replace // [])[] | [.Old.Path, (.Old.Version // ""), .New.Path, (.New.Version // "")] | join("\u001f")' <<< "$json")

  local -A remote=()
  local name sha
  release_remote_tags "$root" > "$tmp/remote-tags" \
    || release_die "git ls-remote --tags origin failed; the tags on origin cannot be listed"
  while IFS=$'\t' read -r name sha; do
    [[ -n "$name" ]] && remote["$name"]="$sha"
  done < "$tmp/remote-tags"

  # A sibling's tag is named by the project owning the required path, which
  # drops a major-version suffix: go.vipengele.org/a/v2 v2.1.0 is released by
  # source/a/v2.1.0. A path no project at the commit owns is looked up with
  # only its suffix dropped.
  local all path required sibling
  all="$(git -C "$root" ls-tree -r --name-only "$commit" -- source/ \
    | sed -n -e 's|^source/\(.*\)/go\.mod$|\1|p' | jq -R . | jq -sc .)"
  while read -r path required sibling; do
    [[ -n "$path" ]] || continue
    if [[ -z "$sibling" ]]; then
      sibling="${path#"$RELEASE_MODULE_PREFIX"}"
      [[ "$sibling" =~ ^(.+)/v([0-9]+)$ ]] && ((10#${BASH_REMATCH[2]} >= 2)) && sibling="${BASH_REMATCH[1]}"
    fi
    if [[ "$required" =~ ^v[0-9]+\.[0-9]+\.[0-9]+-.*[0-9]{14}-[0-9a-f]{12}$ ]]; then
      release_error "$gomod: ${path} is required at the pseudo-version ${required}; a released go.mod requires every sibling at a tagged release"
      status=1
    elif [[ -z "${remote["source/${sibling}/${required}"]:-}" ]]; then
      release_error "$gomod: ${path} ${required} has no tag source/${sibling}/${required} on origin; release ${sibling} first, then require that tag"
      status=1
    fi
  done < <(jq -r --arg prefix "$RELEASE_MODULE_PREFIX" --argjson all "$all" "${RELEASE_JQ_OWNER}"'
    (.Require // [])[] | select(.Path | startswith($prefix))
    | "\(.Path) \(.Version) \(owner(.Path | ltrimstr($prefix)) // "")"' <<< "$json")

  ((status == 0)) || exit 1

  case "$mode" in
    release)
      # A tag already on origin was pushed by a concurrent run, or by hand: the
      # version is released and this run has nothing to tag.
      [[ -z "${remote[$tag]:-}" ]] \
        || release_die "$tag already exists on origin; $project $version is released"
      ;;
    finish)
      [[ -n "${remote[$tag]:-}" ]] || release_die "$tag does not exist on origin"
      [[ "${remote[$tag]}" == "$commit" ]] \
        || release_die "$tag on origin points at ${remote[$tag]}, not at $commit"
      ;;
  esac

  # The Worker serves the unsuffixed path, which is also the go-import prefix
  # of a /vN module: the Go command finds go.vipengele.org/<project>/vN inside
  # the repository root that go.vipengele.org/<project> names.
  worker_answers "$project" \
    || release_die "${vanity_base}/${project}?go-get=1 does not answer for ${RELEASE_MODULE_PREFIX}${project} after ${worker_timeout}s. It answers once the project's go.mod is merged and publish-vanity-modules has published the list: check that run, then re-run this one (ADR-0004, ADR-0016)"

  echo "$tag at $commit is valid"
}

cmd_validate() {
  (($# == 3)) || usage
  local commit
  commit="$(commit_of "$3")"
  check "$1" "$2" "$commit" release
}

cmd_finish_check() {
  (($# == 1)) || usage
  local tag="$1" project version commit
  project_version "$tag"
  local_tag "$tag"
  commit="$(commit_of "refs/tags/$tag")"
  check "$project" "$version" "$commit" finish
}

# run <args...>: echo a command, then run it.
run() {
  echo "==> $*"
  "$@"
}

cmd_test() {
  (($# == 1)) || usage
  local project="$1" dir
  dir="$root/source/$project"
  [[ -f "$dir/go.mod" ]] || release_die "source/$project/go.mod does not exist"
  cd "$dir"

  # go mod tidy resolves every requirement through the module proxy, as a
  # consumer's build does. A go.mod or go.sum that tidy changes, or a go.sum it
  # creates, is not the module that was reviewed.
  run go mod tidy
  if [[ -n "$(git status --porcelain -- go.mod go.sum)" ]]; then
    git --no-pager diff -- go.mod go.sum
    release_die "go mod tidy changes go.mod or go.sum of $project; the tagged module is not tidy"
  fi

  run go build ./...
  # A release runs no Windows tests; the gated windows-latest job in ci-test
  # does (ADR-0030). The cross-compile still vets the Windows-only files,
  # writing no artefacts.
  if [[ -n "$(find . \( -name '*_windows.go' -o -name '*_windows_test.go' \) -not -path './vendor/*' -print -quit)" ]]; then
    echo "==> GOOS=windows go vet ./..."
    GOOS=windows go vet ./...
  fi
  run go vet ./...
  run go test -race -shuffle=on ./...
}

cmd_tag() {
  (($# == 4)) || usage
  local project="$1" version="$2" run_url="$4" tag commit
  tag="$(tag_name "$project" "$version")"
  commit="$(commit_of "$3")"

  # The identity lives in the environment of this one command, which outranks
  # any configured user and leaves the repository's config untouched. The tag
  # is unsigned whatever tag.gpgSign says.
  GIT_COMMITTER_NAME="$bot_name" GIT_COMMITTER_EMAIL="$bot_email" \
    GIT_AUTHOR_NAME="$bot_name" GIT_AUTHOR_EMAIL="$bot_email" \
    git -C "$root" -c tag.gpgSign=false tag -a --no-sign \
    -m "$project $version" -m "Released by $run_url" "$tag" "$commit" \
    || release_die "cannot create the tag $tag"

  # Only this tag is pushed. Origin refuses a tag it already has, so a version
  # released concurrently fails here instead of being moved.
  git -C "$root" push origin "refs/tags/$tag" \
    || release_die "origin rejected $tag; it may already be released"
  echo "pushed $tag at $commit"
}

cmd_announce() {
  (($# == 1)) || usage
  local tag="$1" project version module attempt notes output
  project_version "$tag"
  local_tag "$tag"
  cd "$root"

  # The proxy is asked for the module path the tagged go.mod declares, which
  # carries a /vN suffix at v2 and above.
  mkdir -p "$tmp/gomod"
  git -C "$root" show "refs/tags/${tag}^{commit}:source/${project}/go.mod" > "$tmp/gomod/go.mod" 2> /dev/null \
    || release_die "source/${project}/go.mod is missing at $tag"
  module="$(cd "$tmp/gomod" && go mod edit -json go.mod | jq -r .Module.Path)" \
    || release_die "source/${project}/go.mod at $tag does not parse"
  [[ -n "$module" && "$module" != null ]] \
    || release_die "source/${project}/go.mod at $tag declares no module path"

  # Asking the proxy for the version makes it fetch the tag now, so the first
  # consumer does not wait for it, and proves the module path and the tag
  # agree. The proxy can lag the push by a moment, hence the retries.
  for ((attempt = 1; ; attempt++)); do
    if GOPROXY=https://proxy.golang.org go list -m "${module}@${version}"; then
      break
    fi
    if ((attempt >= proxy_attempts)); then
      release_die "${module}@${version} is not resolvable through the module proxy. The proxy resolves it through go.vipengele.org, which answers once the project's go.mod is merged and publish-vanity-modules has published the list: check that run and the Worker, then finish the release (ADR-0004, ADR-0016)"
    fi
    echo "attempt ${attempt} failed; retrying in ${proxy_interval}s"
    sleep "$proxy_interval"
  done

  # A release is idempotent: finishing a partial release, or racing another
  # run that announces the same tag, succeeds on what already exists.
  if gh release view "$tag" > /dev/null 2>&1; then
    echo "release $tag already exists"
    return 0
  fi

  notes="$tmp/notes.md"
  git -C "$root" show "refs/tags/${tag}^{commit}:docs/release-notes/${project}/${version}.md" > "$notes" 2> /dev/null \
    || release_die "docs/release-notes/${project}/${version}.md is missing at $tag"

  local prerelease=()
  if [[ "$version" == *-* || "$version" == v0.* ]]; then prerelease=(--prerelease); fi
  if output="$(gh release create "$tag" \
    --verify-tag \
    --title "$project $version" \
    --notes-file "$notes" \
    "${prerelease[@]}" 2>&1)"; then
    printf '%s\n' "$output"
  elif grep -qiE 'already[ _]exists' <<< "$output"; then
    printf '%s\n' "$output"
    echo "release $tag already exists"
  else
    printf '%s\n' "$output" >&2
    release_die "gh release create $tag failed"
  fi
}

(($# >= 1)) || usage
subcommand="$1"
shift
case "$subcommand" in
  validate) cmd_validate "$@" ;;
  test) cmd_test "$@" ;;
  tag) cmd_tag "$@" ;;
  announce) cmd_announce "$@" ;;
  finish-check) cmd_finish_check "$@" ;;
  *) usage ;;
esac
