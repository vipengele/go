# lib.sh — helpers shared by the release scripts. Sourced, never executed: the
# entry script sets the shell options, and every function here takes the
# repository root as its first argument rather than relying on the working
# directory.
#
# A project is a directory under source/, at any depth, holding a go.mod
# (ADR-0001, ADR-0004); its identifier is its path relative to source/
# (`cli/core`). The release of version vX.Y.Z of a project is the tag
# source/<project>/vX.Y.Z, and its notes are docs/release-notes/<project>/vX.Y.Z.md.

# release_error <message>: report an error on stderr, as a workflow error
# annotation when running in GitHub Actions.
release_error() {
  if [[ "${GITHUB_ACTIONS:-}" == true ]]; then
    printf '::error::%s\n' "$*" >&2
  else
    printf 'error: %s\n' "$*" >&2
  fi
}

# release_die <message>: report the error and exit non-zero.
release_die() {
  release_error "$*"
  exit 1
}

# RELEASE_TAG_RE matches a release tag, source/<project>/vX.Y.Z with an
# optional prerelease suffix: BASH_REMATCH[1] is the project and
# BASH_REMATCH[3] the version. A prerelease suffix matches here so that it is
# reported as such rather than as a malformed tag.
# shellcheck disable=SC2034 # read by the scripts that source this file
RELEASE_TAG_RE='^source/([a-z0-9-]+(/[a-z0-9-]+)*)/(v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?)$'

# RELEASE_MODULE_PREFIX is the vanity prefix of every project's module path.
# shellcheck disable=SC2034 # read by the scripts that source this file
RELEASE_MODULE_PREFIX='go.vipengele.org/'

# release_projects <root>: the project identifiers, one per line, sorted.
release_projects() {
  local root="$1"
  [[ -d "$root/source" ]] || return 0
  (cd "$root" && find source -name go.mod -type f) \
    | sed -e 's|^source/||' -e 's|/go\.mod$||' | LC_ALL=C sort -u
}

# release_projects_json <root>: the project identifiers as a sorted JSON array.
release_projects_json() {
  release_projects "$1" | jq -R . | jq -sc .
}

# RELEASE_JQ_OWNER defines owner($path) over the JSON array $all of project
# identifiers: the project whose identifier is the longest prefix of $path at a
# `/` boundary, or null. A major-version suffix (`cli/v2`) therefore resolves
# to `cli`.
# shellcheck disable=SC2034 # read by the scripts that source this file
RELEASE_JQ_OWNER='def owner($path):
  [$all[] as $p | select($path == $p or ($path | startswith($p + "/"))) | $p]
  | max_by(length);'

# release_edges <root>: a JSON array of {project, deps}, one entry per project,
# where deps are the other projects its go.mod requires directly. A require of
# go.vipengele.org/<path> is an edge to the project owning <path> (ADR-0006,
# ADR-0004). A project's require of its own module path (a major-version
# suffix of itself) is not an edge.
#
# `go mod edit -json` reads the file alone and never touches the network.
# GOWORK=off keeps a root go.work, and the toolchain its go directive names,
# out of the reading.
release_edges() {
  local root="$1" all project
  all="$(release_projects_json "$root")"
  while IFS= read -r project; do
    [[ -n "$project" ]] || continue
    (cd "$root" && GOWORK=off go mod edit -json "source/${project}/go.mod") \
      | jq -c --arg project "$project" --argjson all "$all" "${RELEASE_JQ_OWNER}"'
          {project: $project,
           deps: [(.Require // [])[].Path
                  | select(startswith("go.vipengele.org/"))
                  | owner(ltrimstr("go.vipengele.org/"))
                  | select(. != null and . != $project)] | unique}' \
      || return 1
  done < <(release_projects "$root") | jq -sc .
}

# release_tag_names <file>: the tag names in a list of tags, one per line,
# sorted and unique. A line is a bare name (`source/a/v1.0.0`), a ref
# (`refs/tags/source/a/v1.0.0`) or a `git ls-remote --tags` line
# (`<sha>\trefs/tags/...`); a peeled entry (`...^{}`) names the same tag.
release_tag_names() {
  local file="$1"
  awk '{ print $NF }' "$file" \
    | sed -e 's|^refs/tags/||' -e 's|\^{}$||' \
    | grep -v '^$' | LC_ALL=C sort -u || true
}

# release_tag_has_notes <root> <tag>: succeeds when the commit the local tag
# points to holds the notes the tag names; a tag that is not of the form
# source/<project>/<version> has none.
release_tag_has_notes() {
  local root="$1" tag="$2" rest project version
  [[ "$tag" == source/*/v* ]] || return 1
  rest="${tag#source/}"
  project="${rest%/*}"
  version="${rest##*/}"
  git -C "$root" cat-file -e "refs/tags/${tag}^{commit}:docs/release-notes/${project}/${version}.md" 2>/dev/null
}

# release_remote_tags <root>: every tag on the remote `origin` of the
# repository, one `<name>\t<commit>` line each, where <commit> is the commit
# the tag points to (the peeled `^{}` entry of an annotated tag). Fails when
# the remote cannot be listed.
release_remote_tags() {
  local root="$1" listing
  listing="$(git -C "$root" ls-remote --tags origin)" || return 1
  awk -F '\t' '
    $2 ~ /\^\{\}$/ { name = substr($2, 11, length($2) - 13); peeled[name] = $1; next }
    { name = substr($2, 11); direct[name] = $1; order[++n] = name }
    END { for (i = 1; i <= n; i++) print order[i] "\t" (order[i] in peeled ? peeled[order[i]] : direct[order[i]]) }
  ' <<< "$listing"
}
