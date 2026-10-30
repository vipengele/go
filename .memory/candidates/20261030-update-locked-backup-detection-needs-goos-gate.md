---
about: "Installed.Discard decides a backup is locked from Config.GOOS plus raw errno values 5 and 32, because syscall.ERROR_SHARING_VIOLATION exists only on Windows and the same numbers mean EIO and EPIPE on Linux"
saw:
  - source/update/install.go
  - source/update/install_test.go
  - docs/adr/0022-install-replaces-the-executable-with-two-renames-and-keeps-a-backup.md
---

- `backupLocked` (install.go) returns false unless the resolved `Config.GOOS` is `windows`, then
  matches `syscall.Errno` 5 (access denied) or 32 (sharing violation) through `errors.As`. The codes
  are declared in install.go as `windowsAccessDenied` and `windowsSharingViolation` because
  `syscall.ERROR_SHARING_VIOLATION` is not defined off Windows and a `_windows.go` file would switch
  on the `GOOS=windows go vet` gate (ADR-0022).
- Dropping the GOOS gate would classify a Linux EIO (5) or EPIPE (32) from removing the backup as
  "locked"; install_test.go asserts the same errno on linux is not a `*BackupLockedError`.
- Access denied is matched because removing a running exe's file on Windows reports it, so a genuine
  ACL denial on Windows also reads as locked. Windows behaviour is unverified; only the injected test
  covers the branch.
