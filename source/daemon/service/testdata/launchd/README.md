# launchd fixtures

- `*.golden` — the property lists `renderPlist` writes; `go test ./service -update` (from
  `source/daemon`) rewrites them.
- `print-*.txt` — `launchctl print gui/501/com.example.demo` output in the shapes macOS
  produces: a running agent, a running agent whose earlier runs crashed or exited cleanly, a job
  waiting to be respawned after a crash, a clean exit, a job that has never exited, a job killed
  by a signal, a job whose nested `endpoints` and `event triggers` blocks carry their own
  `state =` and `pid =` lines, and output that has no `state`.
- `print-disabled.txt` and `print-disabled-bool.txt` — `launchctl print-disabled gui/501` output,
  in the `=> disabled | enabled` form and in the `=> true | false` form of older releases, where
  `true` means disabled.

The `print*` fixtures are transcribed from public macOS output, not captured from a machine:
they pin how `Status` reads the shapes shown here, not how a given macOS release writes them.
The check against a real launchd is the macOS supervisor harness tracked in issue #82.
