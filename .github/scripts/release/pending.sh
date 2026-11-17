#!/usr/bin/env bash
# pending.sh — the release set: which projects to tag, in which order, and
# which tags still need announcing.
#
# Usage: pending.sh <repo-root> <tags-file> <releases-file>
#
#   <repo-root>      a checkout of the commit being released, with every tag in
#                    <tags-file> present locally (`git fetch --tags` first): an
#                    unannounced tag is recognised by the notes in its own
#                    commit, so a tag that cannot be read fails the run rather
#                    than being silently skipped.
#   <tags-file>      the remote's tags, one per line: bare names, `refs/tags/`
#                    refs or `git ls-remote --tags` output; peeled `^{}` lines
#                    name the same tag.
#   <releases-file>  the tag names that have a GitHub Release, one per line.
#
# Either file may be /dev/stdin. Nothing here touches the network.
#
# Output on stdout, one JSON object:
#
#   {"tiers": [[{"project": "commons", "version": "v0.2.0"}], ...],
#    "unannounced": ["source/config/v0.1.0", ...]}
#
# A project is pending when docs/release-notes/<project>/vX.Y.Z.md exists in
# <repo-root> and the tag source/<project>/vX.Y.Z does not. Notes in a
# directory that is not a project are ignored. A tag is unannounced when its
# commit holds its notes and no GitHub Release carries its name; a tag whose
# commit holds no notes (a spike tagged by hand) is ignored.
#
# Tier 1 is every pending project that depends on no other pending project;
# tier N+1 is every pending project whose deepest pending dependency is in
# tier N. Dependencies are the sibling requires of every project, followed
# transitively through projects that are not pending, so a pending project
# that reaches another only through a released one still comes after it.
# Projects within a tier are sorted.
#
# Fails, naming the projects, on a dependency cycle among projects, on more
# than one pending version of a project, and on more than 8 tiers (the number
# of tier jobs in auto-release.yml).
set -euo pipefail

# shellcheck source=lib.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

readonly max_tiers=8

if (($# != 3)); then
  echo "usage: pending.sh <repo-root> <tags-file> <releases-file>" >&2
  exit 2
fi
root="$1"
tags_file="$2"
releases_file="$3"

git -C "$root" rev-parse --git-dir > /dev/null 2>&1 || release_die "$root is not a git repository"
[[ -r "$tags_file" ]] || release_die "cannot read the tag list $tags_file"
[[ -r "$releases_file" ]] || release_die "cannot read the release list $releases_file"

tags="$(release_tag_names "$tags_file" | jq -R . | jq -sc .)"
releases="$(release_tag_names "$releases_file" | jq -R . | jq -sc .)"
edges="$(release_edges "$root")" || release_die "cannot read the go.mod of every project"

# Every notes file of an existing project, as {project, version}. Only the
# files directly in the project's notes directory count: a nested directory is
# the notes of another project.
notes="$(release_projects "$root" | while IFS= read -r project; do
  dir="$root/docs/release-notes/$project"
  [[ -d "$dir" ]] || continue
  for file in "$dir"/*.md; do
    [[ -f "$file" ]] || continue
    version="$(basename "$file" .md)"
    if [[ "$version" =~ ^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$ ]]; then
      jq -nc --arg project "$project" --arg version "$version" '{project: $project, version: $version}'
    fi
  done
done | jq -sc .)"

# The tags among those listed whose own commit holds their notes.
noted_tags="$(jq -r '.[] | select(test("^source/.+/v[0-9]"))' <<< "$tags" | while IFS= read -r tag; do
  if ! git -C "$root" rev-parse -q --verify "refs/tags/${tag}^{commit}" > /dev/null; then
    release_die "tag $tag is listed but not present in $root; fetch the tags before running pending.sh"
  fi
  if release_tag_has_notes "$root" "$tag"; then
    printf '%s\n' "$tag"
  fi
done | jq -R . | jq -sc .)"

result="$(jq -nc \
  --argjson edges "$edges" \
  --argjson notes "$notes" \
  --argjson tags "$tags" \
  --argjson releases "$releases" \
  --argjson noted "$noted_tags" \
  --argjson max "$max_tiers" '
  def fix(f): f as $next | if $next == . then . else $next | fix(f) end;

  ($edges | map({key: .project, value: .deps}) | from_entries) as $deps

  # reach[p]: every project p depends on, directly or transitively.
  | ($deps | fix(. as $r | with_entries(.value = ((.value + [.value[] | $r[.][]?]) | unique))))
    as $reach

  | [$reach | to_entries[] | select(.key as $k | any(.value[]; . == $k)) | .key] as $cyclic

  | [$notes[] | select(("source/" + .project + "/" + .version) as $t | $tags | index($t) | not)]
    as $pending
  | [$pending | group_by(.project)[] | select(length > 1)
     | "\(.[0].project) has more than one pending version: \(map(.version) | join(", "))"]
    as $duplicates

  | ($pending | map(.project) | unique) as $pendingProjects
  | ($pendingProjects | map({key: ., value: [($reach[.] // [])[] as $d | select($pendingProjects | index($d)) | $d]})
     | from_entries) as $pendingDeps

  # depth[p]: 1 + the deepest pending project p depends on. On a cycle the
  # depths never settle, so they are computed only for an acyclic graph.
  | (if ($cyclic | length) > 0 or ($duplicates | length) > 0 then {}
     else $pendingDeps | map_values(1)
       | fix(. as $d | with_entries(.value = 1 + ([$pendingDeps[.key][] | $d[.]] | max // 0)))
     end) as $depth
  | ([$depth[]] | max // 0) as $tiers

  | {errors: (
      (if ($cyclic | length) > 0
       then ["dependency cycle among projects: \($cyclic | join(", "))"] else [] end)
      + $duplicates
      + (if $tiers > $max
         then ["the pending projects are \($tiers) tiers deep, more than \($max): \([$depth | to_entries[] | select(.value > $max) | .key] | join(", "))"]
         else [] end)),
     tiers: [range(1; $tiers + 1) as $t
             | [$pending | sort_by(.project)[] | select($depth[.project] == $t) | {project, version}]],
     unannounced: [$noted[] as $t | select($releases | index($t) | not) | $t] | sort}')"

if jq -e '.errors | length > 0' <<< "$result" > /dev/null; then
  jq -r '.errors[] | "error: " + .' <<< "$result" >&2
  exit 1
fi
jq '{tiers, unannounced}' <<< "$result"
