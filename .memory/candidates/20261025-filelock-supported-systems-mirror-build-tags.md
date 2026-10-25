---
about: "filelock supports six unix systems on purpose: lock_unix.go's build tag and lock_other.go's negation must stay exact mirrors"
saw:
  - source/filelock/lock_unix.go
  - source/filelock/lock_other.go
  - source/filelock/lock_windows.go
  - source/filelock/AGENTS.md
---

- `lock_unix.go` is tagged `linux || darwin || freebsd || netbsd || openbsd || dragonfly`.
  `lock_other.go` is tagged with the negation of that list plus `windows`. A system in neither file
  fails to build (`undefined: tryLock`); a system in both defines `tryLock` twice.
- `golang.org/x/sys` also provides `unix.Flock` on solaris and illumos, and not on aix. The package
  leaves solaris and illumos to `lock_other.go` (they get `ErrUnsupported`); adding them means
  editing both tags together and vetting `GOOS=solaris` and `GOOS=illumos`.
