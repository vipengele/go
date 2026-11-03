---
about: "installscript's install.sh traps INT and TERM separately with an explicit exit, because a handler shared with EXIT returns and lets the script carry on"
saw:
  - source/update/installscript/install.sh.tmpl
---

- `install.sh.tmpl:42-44`: `trap cleanup EXIT`, `trap 'cleanup; exit 130' INT`,
  `trap 'cleanup; exit 143' TERM`. A single `trap cleanup EXIT INT TERM` runs cleanup on the
  signal and then resumes the script after the interrupted command, so an interrupted install would
  continue with its temp directory removed. The explicit exit re-enters cleanup through EXIT, which
  then finds nothing left to remove.
- A "one trap" simplification of these lines reintroduces that.
