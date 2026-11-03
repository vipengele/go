---
about: "installscript's shell tests run under a PATH of symlinked tools, and printf must be on it because install.sh runs `find -exec printf`"
saw:
  - source/update/installscript/harness_test.go
  - source/update/installscript/install.sh.tmpl
---

- `harness_test.go:29-34` (`scriptTools`) lists every external command the script and the
  completion snippet run; the harness PATH holds symlinks to exactly those plus one downloader and
  one hasher, and a missing tool skips the test.
- `install.sh.tmpl:216` counts matching entries with `find ... -exec printf . \;`, which runs the
  external printf, not the shell builtin. A new external command in the template, or in
  `completioninstall.Snippet`, needs a matching entry in `scriptTools`, or every shell test fails
  with "not found".
- `brew` is deliberately absent so the snippet's zsh target never depends on the host.
