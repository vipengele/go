# Service drivers are selected at runtime

Status: accepted. It extends ADR-0003, which gives `daemon` its project, and keeps to ADR-0019,
which vets Windows only for a project holding a `*_windows.go` file, as ADR-0023 does for the
instance lock. `WithGOOS` follows the semantics of the injectable `GOOS` in the `Options` of
`cli/spec/paths` (ADR-0009). Amended by ADR-0029, which makes `service` import `cli/spec` and
`cli/spec/paths` and has `New` refuse inside a container before it validates the `Config` and
switches on the `GOOS`.

`go.vipengele.org/daemon/service` fronts the operating system's service manager (systemd, launchd,
the Windows service control manager) behind one `Manager` interface. `New(cfg, opts...)` picks the
**Driver** of the target operating system when it is called, from the `GOOS` that `WithGOOS` sets
and `runtime.GOOS` otherwise.

## The rules

- **One selection point.** `New` validates the `Config` against the target `GOOS`, then switches on
  that `GOOS`. A `case` per operating system returns that operating system's Driver, and a `GOOS`
  with no driver returns `ErrUnsupported`.
- **`WithGOOS` is the injectable `GOOS`.** `""` means `runtime.GOOS`, `"windows"` applies the
  Windows rules and every other value applies the POSIX rules. A test renders and validates the
  service of any operating system on any host.
- **Every driver compiles on every operating system.** The rendering of a unit file, a property
  list or a registration is plain Go over the standard library, so the tests of every driver run in
  the one CI job that runs on Linux.
- **Validation follows the target.** `Config.Validate` checks the rules of the target `GOOS`, not of
  the host: a path is absolute by the target's syntax, never by `filepath.IsAbs`, which answers for
  the host.

## Considered options

- **One build-tagged file per operating system.** A driver behind `//go:build linux` is not
  compiled, vetted or tested on any other host, and CI never runs Windows (ADR-0019). Three drivers
  would each be exercised only on its own operating system, and the Windows one never.
- **A driver registry** that each driver fills from an `init`. It needs a package-level map, which
  `gochecknoglobals` forbids, and it makes the set of drivers depend on which files were compiled.

## Consequences

- **No build tags and no `_windows.go` file for rendering or validation.** Only the operating-system
  calls themselves, the Windows service control manager, could ever need a `_windows.go` file; the
  project then falls under the Windows vet of ADR-0019.
- **`Validate` has no host dependence.** The same `Config` gives the same violations on every host,
  in a fixed order.
- **A new operating system is one `case` in `New`** and one driver, with no registration step.
