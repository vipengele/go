---
about: "daemon/selfupdate restores before it records a rollback, reads a pending record for another version as a lost rollback, and treats a corrupt record as an error"
saw:
  - source/daemon/selfupdate/machine.go
  - source/daemon/selfupdate/store.go
  - source/daemon/selfupdate/rollback.go
  - source/daemon/selfupdate/record.go
  - docs/adr/0033-a-daemon-self-update-restores-the-binary-before-recording-the-rollback.md
---
- `rollBack` (machine.go) calls the `restore` seam inside the same `Store.Update` fn that writes
  `rolled_back`, so the binary is back before the record says so (ADR-0033). A save failure after
  a successful restore returns true with the error.
- `Boot` (machine.go) on a pending record whose `ToVersion` differs from the running version records
  `rolled_back` and touches no file: that is how a restore whose record write was lost is repaired.
  Changing that branch to restore or count an attempt breaks the repair.
- `Boot` counts the attempt in one `Update` and rolls back in a second; `rollBack` reports true for a
  record already `rolled_back` for the running version, so a rollback another process finished in
  the gap still makes the caller exit.
- `Status` (record.go) decodes only the four names; an unknown one, like undecodable JSON, is an
  error from `FileStore.Load` (store.go), never idle. An operator clears it by deleting the file.
- `FileStore.Update` (store.go) hands the filelock to an I/O goroutine that releases it only after its
  last read or write, so a write abandoned on ctx keeps the lock until it finishes.
- Windows restore is `restoreAside` (rollback.go): remove stale `<target>.failed`, rename target
  aside, rename backup in, undo on failure. It is portable code, so its seam tests run on Linux.
