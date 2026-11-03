---
about: "installscript's shell tests run under a PATH of symlinked tools, and printf must be on it because install.sh runs `find -exec printf`"
saw:
  - source/update/installscript/harness_test.go
  - source/update/installscript/extract-archive.sh.in
---

- `harness_test.go:31-39` (`scriptTools`) lists every external command the script and the
  completion snippet run; the harness PATH holds symlinks to exactly those plus one downloader and
  one hasher, and a missing tool skips the test.
- `extract-archive.sh.in:3` counts matching entries with `find ... -exec printf . \;`, which runs the
  external printf, not the shell builtin. A new external command in the script sources, or in
  `completioninstall.Snippet`, needs a matching entry in `scriptTools`, or every shell test fails
  with "not found".
- `brew` is deliberately absent so the snippet's zsh target never depends on the host.
