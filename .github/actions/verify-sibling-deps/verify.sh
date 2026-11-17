#!/usr/bin/env bash
# verify.sh — the check of the verify-sibling-deps action; action.yml states
# the rules it enforces.
#
# Usage: verify.sh <root> <remote>
#
# <root> is the repository checkout, <remote> the git remote whose tags are the
# releases (`https://github.com/<owner>/<repo>.git`). Prints one workflow
# annotation per finding and exits non-zero when any is an error.
set -euo pipefail

if (($# != 2)); then
  echo "usage: verify.sh <root> <remote>" >&2
  exit 2
fi
cd "$1"
remote="$2"
where="${GITHUB_REPOSITORY:-$remote}"
prefix="go.vipengele.org/"
status=0

# has_tag <tag> answers from a single listing of the remote's tags. A listing
# that fails leaves every verdict unknown, so the check stops.
declare -A tags=()
tags_listed=0
has_tag() {
  if ((tags_listed == 0)); then
    local listing
    if ! listing="$(git ls-remote --tags --refs "$remote" 2> /dev/null)"; then
      echo "::error::git ls-remote --tags ${remote} failed; the tag lookup is unauthenticated and needs a public repository"
      exit 1
    fi
    while read -r _ ref; do
      if [[ -n "$ref" ]]; then
        tags["${ref#refs/tags/}"]=1
      fi
    done <<< "$listing"
    tags_listed=1
  fi
  [[ -n "${tags[$1]:-}" ]]
}

# is_pending <project> <version>: the version's release notes exist and its
# tag does not.
is_pending() {
  [[ -f "docs/release-notes/$1/$2.md" ]] && ! has_tag "source/$1/$2"
}

mapfile -t gomods < <(find source -name go.mod -type f 2> /dev/null | sort)

# A project directory must not contain another go.mod.
for outer in "${gomods[@]}"; do
  for inner in "${gomods[@]}"; do
    if [[ "$inner" != "$outer" && "$inner" == "${outer%/go.mod}/"* ]]; then
      echo "::error file=${inner}::${inner} is inside the project directory of ${outer}; a project directory must not contain another go.mod"
      status=1
    fi
  done
done

for gomod in "${gomods[@]}"; do
  project="${gomod#source/}"
  project="${project%/go.mod}"
  dir="${gomod%/go.mod}"
  # The Go command refuses a version on a directory replace target
  # (`=> ../commons v0.1.0`), so that replace fails here.
  if ! json="$(go mod edit -json "$gomod" 2>&1)"; then
    echo "::error file=${gomod}::${gomod} does not parse: ${json//$'\n'/ }"
    status=1
    continue
  fi

  want="${prefix}${project}"
  have="$(jq -r .Module.Path <<< "$json")"
  if [[ "$have" != "$want" ]]; then
    echo "::error file=${gomod}::module path is ${have}, expected ${want}"
    status=1
  fi

  # Strict while any version with release notes has no tag. Notes of a nested
  # project sit in their own subdirectory, so only files directly in the
  # project's notes directory count.
  untagged=()
  notes="docs/release-notes/${project}"
  if [[ -d "$notes" ]]; then
    while IFS= read -r note; do
      version="$(basename "$note" .md)"
      if ! has_tag "source/${project}/${version}"; then
        untagged+=("$version")
      fi
    done < <(find "$notes" -mindepth 1 -maxdepth 1 -type f -name 'v*.md' | sort)
  fi
  strict=0
  if ((${#untagged[@]} > 0)); then
    strict=1
    echo "::notice file=${gomod}::${project} is in strict mode: release notes ${untagged[*]} have no tag under source/${project}/, so it requires a replaced sibling at the sibling's pending or tagged version and every other sibling at a tagged version"
  fi

  # A replace is held to the same rule in both modes; strict mode narrows the
  # version a replaced sibling is required at, below.
  declare -A replaced=()
  while IFS=$'\x1f' read -r old oldv new newv; do
    if [[ "$old" != "$prefix"* ]]; then
      echo "::error file=${gomod}::replace of ${old} -> ${new}${newv:+ $newv}: only a sibling may be replaced, and only by its own project directory"
      status=1
      continue
    fi
    sibling="${old#"$prefix"}"
    replaced["$old"]=1
    if [[ -n "$oldv" || -n "$newv" ]]; then
      echo "::error file=${gomod}::replace of ${old}${oldv:+ $oldv} -> ${new}${newv:+ $newv} names a version; a sibling is replaced by its project directory alone"
      status=1
      continue
    fi
    if [[ "$new" == /* ]]; then
      target="$(realpath -m --relative-to=. -- "$new")"
    else
      target="$(realpath -m --relative-to=. -- "${dir}/${new}")"
    fi
    own="$(realpath -m --relative-to=. -- "source/${sibling}")"
    if [[ ! -f "source/${sibling}/go.mod" || "$target" != "$own" ]]; then
      echo "::error file=${gomod}::replace of ${old} -> ${new} resolves to ${target}; a sibling is replaced only by its own project directory source/${sibling}"
      status=1
    fi
  done < <(jq -r '(.Replace // [])[] | [.Old.Path, (.Old.Version // ""), .New.Path, (.New.Version // "")] | join("\u001f")' <<< "$json")

  # A replaced sibling's required version is never looked up in development
  # mode. In strict mode it is the version the release commit requires once
  # the replace is dropped, so it is the sibling's pending version, released
  # ahead of this project, or one already tagged.
  while read -r path version; do
    sibling="${path#"$prefix"}"
    if [[ "$version" =~ ^v[0-9]+\.[0-9]+\.[0-9]+-.*[0-9]{14}-[0-9a-f]{12}$ ]]; then
      echo "::error file=${gomod}::${path} is required at the pseudo-version ${version}; require a tagged release, or replace the sibling by its project directory"
      status=1
    elif [[ -n "${replaced[$path]:-}" ]]; then
      if ((strict)) && ! has_tag "source/${sibling}/${version}" && ! is_pending "$sibling" "$version"; then
        echo "::error file=${gomod}::${path} ${version} is replaced by its project directory, but has no tag source/${sibling}/${version} in ${where} and no release notes docs/release-notes/${sibling}/${version}.md; ${project} is in strict mode, so a replaced sibling is required at its pending or a tagged version"
        status=1
      fi
    elif ! has_tag "source/${sibling}/${version}"; then
      if ((strict)); then
        echo "::error file=${gomod}::${path} ${version} has no tag source/${sibling}/${version} in ${where}; ${project} is in strict mode, so every sibling it requires without a replace is a tagged release"
      else
        echo "::error file=${gomod}::${path} ${version} has no tag source/${sibling}/${version} in ${where}; require a tagged release, or replace the sibling by its project directory"
      fi
      status=1
    fi
  done < <(jq -r '(.Require // [])[] | select(.Path | startswith("'"$prefix"'")) | "\(.Path) \(.Version)"' <<< "$json")
  unset replaced
done

exit "$status"
