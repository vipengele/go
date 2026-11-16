---
about: "systemd as PID 1 boots in rootless podman on GitHub's ubuntu-latest, degraded by kernel mounts, and a crash loop never reaches ActiveState=failed"
saw:
  - .github/workflows/ci-test.yml
  - source/daemon/servicetest/podman.go
  - source/daemon/servicetest/servicetest.go
  - source/daemon/service/systemd_unit.go
---

- The `test-supervisor` job (`.github/workflows/ci-test.yml`) runs `podman run --privileged
  --systemd=always` rootless on ubuntu-latest (Ubuntu 26.04 image) with no apt install and no sudo,
  and both the system manager and a lingering `user@<uid>.service` come up. `sudo podman` was not
  needed.
- `systemctl is-system-running` reports `degraded`, never `running`, in that container:
  proc-sys-fs-binfmt_misc.automount, sys-kernel-{config,debug,tracing}.mount and
  systemd-modules-load.service fail. `waitRunning` in `servicetest/podman.go` accepts degraded for
  that reason; a check that demands `running` never passes there.
- The unit has Restart=always and RestartSec=5 and no StartLimit settings
  (`service/systemd_unit.go`), so default limits (5 starts / 10s) are never hit and a `/bin/false`
  unit cycles activating/auto-restart instead of reaching failed. `service` reports that as
  StateFailed with Detail; `crashLoop` in `servicetest/servicetest.go` waits ~5s for it, and the
  whole TestSupervisor (3 targets x 5 scenarios) took ~52s in CI.
