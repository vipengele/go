# The service harness drives the library in a podman systemd container

Status: accepted. It adds a test harness to the systemd driver of ADR-0024
(`0024-service-drivers-are-selected-at-runtime.md`), whose unit rendering depends on the options of
ADR-0028 (`0028-driver-specific-settings-are-options-not-config-fields.md`).

The `service` package renders a unit and drives `systemctl`, and its tests run against a fake
runner. Nothing there proves that a real systemd accepts the unit, restarts the process after a
kill, reports a crash loop, or removes everything on uninstall. `go.vipengele.org/daemon/servicetest`
gives a consumer one call, `servicetest.Run`, that proves its own service binary under a real
systemd: it boots a podman container with systemd as PID 1 for the service's scope and drives
install, kill, reinstall, crash loop and uninstall through `service.New`.

## The rules

- **The harness ships its own driver.** `servicetest/driver` is a program the harness copies into
  the container. It reads a `wire.Request` on standard input, calls `service.New`, then `Install`,
  `Uninstall` or `Status`, and writes a `wire.Response` on standard output. It does not run the
  consumer's own commands.
- **The driver is a public `package main`.** The harness builds it with
  `GOOS=linux CGO_ENABLED=0 go build go.vipengele.org/daemon/servicetest/driver` in the caller's
  module, so the driver is the version of the library the consumer requires. The build fails under
  `vendor/`, which holds only the packages the module imports, and outside a module.
  `Target.Driver` takes a prebuilt Linux driver for those cases.
- **`Target.Binary` is the host path of a Linux binary.** It is copied into the container at
  `Config.Binary`.
- **Options travel as data.** `Target` carries notify (the watchdog included), hardening and
  identity beside `Config`. They cross to the driver in `wire.Request`, and the driver rebuilds
  the `service.Option`s from it.
- **Gating is by environment.** Without `SERVICETEST=1`, `Run` and `SkipUnlessEnabled` skip. With
  it set and podman unusable, the test fails. There is no build tag, so the package is vetted and
  linted on Linux and Windows like the rest of the project.
- **The image is an embedded Containerfile.** It is Debian stable pinned by digest, with an
  overridable base (`Target.BaseImage`), built by `podman build` and tagged with a hash of the
  file and the base, so an unchanged image is built once. Digest bumps are manual; the
  Containerfile documents the steps.
- **The container is privileged.** `podman run --privileged --systemd=always`, so hardening
  directives apply as on a host. The podman command line is `SERVICETEST_PODMAN`, default
  `podman`; `sudo podman` runs it rootful. Hosted ubuntu runners boot both scopes rootless.
- **The crash loop uses the consumer's config with `Binary` replaced by `/bin/false`.** The unit
  has `Restart=always`, `RestartSec=5` and systemd's default start limit (5 starts within 10
  seconds), which starts 5 seconds apart never reach. The unit therefore never settles in
  `ActiveState=failed`: it cycles through `activating (auto-restart)` with `Result=exit-code`. The
  `service` package maps an activating unit with restarts and a result other than `success` to
  `StateFailed` with a `Detail` (`systemd_status.go`), and the scenario waits for that state.
- **Placement.** The harness is a public package of the daemon project and is released with the
  daemon tag. A project directory holds no second `go.mod` (ADR-0004), so it is not a project of
  its own, and it is not repository tooling, so nothing of it is under `tools/`.

## Considered options

- **Drive the consumer's own commands.** It would prove the whole command line, but the daemon
  command surface is still open work (vipengele/go#80) and the harness would depend on a shape that
  does not exist. Command-level coverage is a follow-up.
- **A build tag for the harness.** It keeps the code out of a plain build, and hides it from
  `go vet` and `golangci-lint` on every platform that does not set the tag. The environment gate
  costs one skipped test.
- **Skip when podman is missing.** A CI job with a broken podman would pass without running the
  scenarios. With `SERVICETEST=1`, an unusable podman fails.
- **Pull a prebuilt systemd image from a registry, or an unpinned tag.** Both make a run depend on
  what the registry serves that day. The pinned digest makes the base reproducible and the
  content-hash tag makes a change to it a rebuild.
- **Pass only `Config` to the driver.** The rendered unit depends on the options (ADR-0028), so the
  harness would prove the default unit and not the consumer's.
- **Serialise the `service.Option`s.** They are functions and cannot be serialised. The harness
  carries the data they are built from.
- **An embedded prebuilt driver binary.** It is per architecture and bloats the module.
- **An `internal` driver package.** A consumer's module cannot build it by path.

## Consequences

- **A consumer proves its service with one test function** and a Linux build of its binary.
- **The daemon's own `TestSupervisor` runs in CI only.** The `test-supervisor` job of `ci-test.yml`
  sets `SERVICETEST=1`; a developer's `go test ./...` skips it.
- **The base image ages until someone bumps it.** The dependabot configuration is managed by `gt`
  and has no docker ecosystem, so the digest in the Containerfile is a manual bump.
- **Rootless podman needs cgroup delegation to boot systemd.** Where a host lacks it,
  `SERVICETEST_PODMAN="sudo podman"` runs the containers rootful.
