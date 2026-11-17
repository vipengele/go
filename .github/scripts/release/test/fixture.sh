# fixture.sh — builders for the throwaway repositories the release self-test
# runs against. Sourced by run.sh. Every function acts on the repository in
# $FIXTURE, which fixture_repo creates.

# fixture_git <args...>: git in the fixture, independent of the host's config:
# a fixed identity, no signing, and no global or system config.
fixture_git() {
  GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_NOSYSTEM=1 \
    GIT_AUTHOR_NAME=fixture GIT_AUTHOR_EMAIL=fixture@example.invalid \
    GIT_COMMITTER_NAME=fixture GIT_COMMITTER_EMAIL=fixture@example.invalid \
    git -C "$FIXTURE" -c commit.gpgsign=false -c tag.gpgsign=false \
    -c init.defaultBranch=main "$@"
}

# fixture_repo <name>: a new empty repository under $WORK, as $FIXTURE.
fixture_repo() {
  FIXTURE="$WORK/$1"
  mkdir -p "$FIXTURE"
  fixture_git init -q
}

# fixture_project <project> [sibling...]: source/<project>/go.mod requiring
# each sibling at v0.0.0, or at vN.0.0 for a sibling with a major suffix
# (`a/v2`).
fixture_project() {
  local project="$1" sibling version
  shift
  mkdir -p "$FIXTURE/source/$project"
  {
    printf 'module go.vipengele.org/%s\n\ngo 1.25.0\n' "$project"
    for sibling in "$@"; do
      version=v0.0.0
      [[ "$sibling" =~ /v([0-9]+)$ ]] && version="v${BASH_REMATCH[1]}.0.0"
      printf '\nrequire go.vipengele.org/%s %s\n' "$sibling" "$version"
    done
  } > "$FIXTURE/source/$project/go.mod"
}

# fixture_notes <project> <version>: docs/release-notes/<project>/<version>.md.
fixture_notes() {
  mkdir -p "$FIXTURE/docs/release-notes/$1"
  printf '# %s %s\n' "$1" "$2" > "$FIXTURE/docs/release-notes/$1/$2.md"
}

# fixture_commit [message]: commit everything in the working tree.
fixture_commit() {
  fixture_git add -A
  fixture_git commit -q --allow-empty -m "${1:-fixture}"
}

# fixture_tag <tag>: an annotated tag on HEAD.
fixture_tag() {
  fixture_git tag -a -m "$1" "$1"
}

# fixture_tags_file: the fixture's local tags, one name per line, in a file
# whose path is printed.
fixture_tags_file() {
  local file="$FIXTURE.tags"
  fixture_git tag -l > "$file"
  printf '%s\n' "$file"
}
