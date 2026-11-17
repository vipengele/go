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

# fixture_origin: a bare repository beside $FIXTURE, as its remote `origin`.
fixture_origin() {
  GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_NOSYSTEM=1 \
    git -c init.defaultBranch=main init -q --bare "$FIXTURE.origin.git"
  fixture_git remote add origin "$FIXTURE.origin.git"
}

# fixture_push [ref...]: push main, and each given ref, to origin; origin/main
# follows.
fixture_push() {
  fixture_git push -q origin main "$@"
}

# fixture_go_source <project>: a package with one test in source/<project>,
# importing nothing outside the standard library, and the go directive of its
# go.mod set to the running toolchain, which GOTOOLCHAIN=local requires.
fixture_go_source() {
  local dir="$FIXTURE/source/$1"
  mkdir -p "$dir"
  sed -i "s/^go .*/go $(go env GOVERSION | sed 's/^go//')/" "$dir/go.mod"
  printf 'package fixture\n\n// Double returns twice n.\nfunc Double(n int) int { return 2 * n }\n' > "$dir/fixture.go"
  printf 'package fixture\n\nimport "testing"\n\nfunc TestDouble(t *testing.T) {\n\tif Double(2) != 4 {\n\t\tt.Fatal("Double(2) != 4")\n\t}\n}\n' > "$dir/fixture_test.go"
}

# fixture_worker [project...]: a stub of the vanity Worker on a free localhost
# port, answering a go-get request for each given project with the go-import
# page the real Worker serves and every other path with 404. Exports
# RELEASE_VANITY_BASE. The server stops when the calling shell exits.
fixture_worker() {
  local dir="$FIXTURE.worker" port_file="$FIXTURE.worker-port" project i
  mkdir -p "$dir"
  for project in "$@"; do
    mkdir -p "$(dirname "$dir/$project")"
    printf '<!DOCTYPE html>\n<html>\n<head>\n<meta charset="utf-8">\n<meta name="go-import" content="go.vipengele.org/%s git https://github.com/vipengele/go source/%s">\n<title>go.vipengele.org/%s</title>\n</head>\n<body>go.vipengele.org/%s</body>\n</html>\n' \
      "$project" "$project" "$project" "$project" > "$dir/$project"
  done
  python3 -I -c '
import functools, http.server, os, sys
handler = functools.partial(http.server.SimpleHTTPRequestHandler, directory=sys.argv[1])
server = http.server.ThreadingHTTPServer(("127.0.0.1", 0), handler)
with open(sys.argv[2] + ".tmp", "w") as f:
    f.write(str(server.server_address[1]))
os.rename(sys.argv[2] + ".tmp", sys.argv[2])
server.serve_forever()
' "$dir" "$port_file" 2> "$FIXTURE.worker-log" &
  FIXTURE_WORKER_PID=$!
  trap 'kill "$FIXTURE_WORKER_PID" 2> /dev/null || true' EXIT
  for ((i = 0; i < 100; i++)); do
    [[ -s "$port_file" ]] && break
    sleep 0.05
  done
  [[ -s "$port_file" ]] || return 1
  export RELEASE_VANITY_BASE="http://127.0.0.1:$(cat "$port_file")"
}

# fixture_shims: stubs of gh and of `go list` in $WORK/bin, to put first on
# PATH. Each call is appended to $STUB_LOG.
#
#   gh release view      exits $STUB_GH_VIEW (default 1: no Release)
#   gh release create    copies its --notes-file to $STUB_LOG.notes; with
#                        STUB_GH_CREATE=exists it fails as GitHub does for an
#                        existing Release, with STUB_GH_CREATE=fail it fails
#                        otherwise
#   go list              exits $STUB_GO_LIST (default 0); every other go
#                        command runs the real go
fixture_shims() {
  local bin="$WORK/bin" real_go
  [[ -d "$bin" ]] && return 0
  real_go="$(command -v go)"
  mkdir -p "$bin"
  cat > "$bin/gh" << 'SHIM'
#!/usr/bin/env bash
printf 'gh %s\n' "$*" >> "$STUB_LOG"
case "$1 $2" in
  "release view") exit "${STUB_GH_VIEW:-1}" ;;
  "release create")
    while (($#)); do
      if [[ "$1" == --notes-file ]]; then cp "$2" "$STUB_LOG.notes"; fi
      shift
    done
    case "${STUB_GH_CREATE:-}" in
      exists) echo 'HTTP 422: Validation Failed' >&2; echo 'Release.tag_name already exists' >&2; exit 1 ;;
      fail) echo 'HTTP 500: Internal Server Error' >&2; exit 1 ;;
    esac
    exit 0 ;;
esac
echo "stub gh: unexpected call: $*" >&2
exit 1
SHIM
  cat > "$bin/go" << SHIM
#!/usr/bin/env bash
if [[ "\$1" == list ]]; then
  printf 'go %s\n' "\$*" >> "\$STUB_LOG"
  exit "\${STUB_GO_LIST:-0}"
fi
exec "$real_go" "\$@"
SHIM
  chmod +x "$bin/gh" "$bin/go"
}

# fixture_go_dependent <project> <sibling> <version>: source/<project> as a
# module requiring go.vipengele.org/<sibling> at <version> and replacing it by
# the sibling's directory, as a dependent developed with its sibling does, with
# a package and a test that call the sibling's Double. The sibling holds
# fixture_go_source. The go directive is the running toolchain's.
fixture_go_dependent() {
  local project="$1" sibling="$2" version="$3" dir up
  dir="$FIXTURE/source/$project"
  up="$(sed -e 's|[^/][^/]*|..|g' <<< "$project")"
  mkdir -p "$dir"
  printf 'module go.vipengele.org/%s\n\ngo %s\n\nrequire go.vipengele.org/%s %s\n\nreplace go.vipengele.org/%s => %s/%s\n' \
    "$project" "$(go env GOVERSION | sed 's/^go//')" "$sibling" "$version" "$sibling" "$up" "$sibling" \
    > "$dir/go.mod"
  printf 'package dependent\n\nimport fixture "go.vipengele.org/%s"\n\n// Quadruple returns four times n.\nfunc Quadruple(n int) int { return fixture.Double(fixture.Double(n)) }\n' \
    "$sibling" > "$dir/dependent.go"
  printf 'package dependent\n\nimport "testing"\n\nfunc TestQuadruple(t *testing.T) {\n\tif Quadruple(2) != 8 {\n\t\tt.Fatal("Quadruple(2) != 8")\n\t}\n}\n' \
    > "$dir/dependent_test.go"
}

# fixture_proxy <project> <version>: a module proxy in the file system beside
# $FIXTURE serving go.vipengele.org/<project> at <version> from the files of
# source/<project> in the working tree, laid out as GOPROXY's protocol names
# them. Exports GOPROXY as its file:// URL, with GOSUMDB off, since no
# checksum database knows the module, and a module cache of its own under
# $WORK, writable so that $WORK can be removed.
fixture_proxy() {
  local project="$1" version="$2" proxy="$FIXTURE.proxy"
  local dir="$proxy/go.vipengele.org/$project/@v"
  mkdir -p "$dir"
  printf '%s\n' "$version" >> "$dir/list"
  printf '{"Version":"%s","Time":"2026-11-17T00:00:00Z"}\n' "$version" > "$dir/$version.info"
  cp "$FIXTURE/source/$project/go.mod" "$dir/$version.mod"
  python3 -I -c '
import os, sys, zipfile
src, prefix, out = sys.argv[1], sys.argv[2], sys.argv[3]
with zipfile.ZipFile(out, "w") as z:
    for base, dirs, files in os.walk(src):
        dirs.sort()
        for name in sorted(files):
            path = os.path.join(base, name)
            z.write(path, prefix + "/" + os.path.relpath(path, src))
' "$FIXTURE/source/$project" "go.vipengele.org/$project@$version" "$dir/$version.zip"
  export GOPROXY="file://$proxy" GOSUMDB=off GOMODCACHE="$WORK/modcache" \
    GOFLAGS="-mod=mod -modcacherw"
}
