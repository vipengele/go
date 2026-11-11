---
about: "The shared atomic write keeps systemd's error strings because systemd tests match them by substring"
saw:
  - source/daemon/service/systemd.go
  - source/daemon/service/systemd_test.go
---

- `writeFileAtomic(path, noun, data)` (systemd.go:207) and `commitFile` serve both drivers; the noun is
  "unit file" for systemd and "plist" for launchd. systemd_test.go matches "rename unit file" (:460) and
  "create unit file" (:560) in error text, so the systemd noun must stay exactly "unit file".
- `commitUnitFile` (systemd.go:198) is a one-line wrapper over `commitFile(..., "unit file")` that only
  systemd_test.go calls (:578, :601). Removing it means rewriting those tests to call `commitFile`.
- `checkAccount(ctx, run, account, service)` (systemd.go:160) and `exitCode` (systemd.go:53) are also shared:
  both drivers check a service account with `id -u --` and read launchctl/systemctl exit codes the same way.
