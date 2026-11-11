---
about: "launchctl exit codes and print formats the launchd driver relies on come from public reports, not from a Mac"
saw:
  - source/daemon/service/launchd.go
  - source/daemon/service/launchd_status.go
  - source/daemon/service/testdata/launchd/README.md
---

- The driver tells launchctl results apart by exit code, never stderr text (launchd.go:20-27): 113 and 3
  (ESRCH) both mean the domain does not hold the job, through the one helper `notLoaded` (launchd.go:195), which
  `loaded`, `bootout` and Status's `print` all use. A check that accepts only 113 in one of them makes `print`
  disagree with `bootout` on releases that answer 3.
- 5 (EIO) is a bootstrap racing an unfinished bootout, which is why Install polls `launchctl print` before
  bootstrap (`waitUnloaded`, launchd.go:230).
- `launchctl print` output is parsed by brace depth, reading only `key = value` lines one level inside the
  `<target> = {` header (`parseLaunchdPrint`, launchd_status.go:152), so `state`/`pid` lines in nested blocks
  such as `endpoints` never count. `print-disabled` accepts both `=> disabled|enabled` and the older
  `=> true|false` (`launchdDisabled`, launchd_status.go:206).
- None of these codes or formats is checked on hardware; the fixtures are transcribed
  (testdata/launchd/README.md). A change in launchctl's output is caught only by running the driver on a Mac.
