---
about: "lydite's semgrep gate blocks exec.Command with a non-constant program even under a justified gosec G204 nosec annotation"
saw:
  - source/daemon/servicetest/podman.go
  - source/daemon/service/systemd.go
---

- lydite scan runs gosec and semgrep. A gosec G204 nosec annotation satisfies gosec only; semgrep's
  `go.lang.security.audit.dangerous-exec-command` is blocking and fires on any
  `exec.CommandContext(ctx, <variable>, ...)` where the program is not a constant.
- The pattern that passes both: a constant program at every call site plus a justified
  gosec G204 nosec annotation (with a `--` reason) for the variable arguments, as in `service/systemd.go` (systemctl,
  launchctl, id) and `servicetest/podman.go` `execRunner.run` (`"podman"`, or `"sudo"` with
  `podman` as first arg). That is why `SERVICETEST_PODMAN` accepts only `podman` or `sudo podman`.
- Locally, `lydite scan` reports semgrep as crashed when semgrep is not installed; the CI artifact
  `lydite-reports-scan` (scan/semgrep.log) holds the real finding.
