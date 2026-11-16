---
about: "A package main reaches lydite's 100% patch coverage by re-executing the test binary as main with -test.gocoverdir"
saw:
  - source/daemon/servicetest/driver/main.go
  - source/daemon/servicetest/driver/main_test.go
  - source/daemon/servicetest/podman_test.go
---

- lydite gates patch coverage at the baseline (100% for daemon), so `func main() { os.Exit(run(...)) }`
  alone fails it: os.Exit cannot run in-process and gochecknoglobals forbids an exit-func variable.
- `servicetest/driver/main_test.go` covers it: TestMain calls `main()` when an env switch is set,
  and a test re-executes `os.Executable()` with that switch, `-test.run=^$` and the parent's
  `-test.gocoverdir` (read through `flag.Lookup`), so the child's counters land in the parent's
  profile under `go test -coverprofile`.
- Stand-in programs found through PATH (`go`, `podman`, `sudo` in the servicetest tests) need the
  `.exe` suffix on Windows, or `exec.LookPath` fails on windows-latest; see `standIn` in
  `servicetest/podman_test.go`.
