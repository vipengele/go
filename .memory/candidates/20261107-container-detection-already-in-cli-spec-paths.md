---
about: "container detection lives in cli/spec/paths: InContainer is the raw Linux heuristic and InContainerFor adds the <PREFIX>_CONTAINER tri-state override that update and daemon/service both use"
saw:
  - source/cli/spec/paths/container.go
  - source/update/config.go
  - source/update/managed.go
  - source/daemon/service/manager.go
  - source/daemon/service/option.go
---

- `paths.InContainer(Probes)` (container.go:54) probes /.dockerenv, /run/.containerenv, the `container`
  and KUBERNETES_SERVICE_HOST variables and /proc/1/cgroup; it is false on every GOOS but linux.
  Probes{GOOS,LookupEnv,Exists,ReadFile} are the seams; a nil field reads the real system.
- `paths.InContainerFor(id, Probes)` (container.go:105) reads `id.EnvVar("CONTAINER")`: unset or empty
  defers to the heuristic, a strconv.ParseBool value decides, any other non-empty value ("yes") is true.
  It applies on any GOOS. An invalid EnvPrefix falls back to the heuristic. This is unlike update's
  `<PREFIX>_NO_UPDATE_CHECK` (any non-empty value, "0" included, opts out; check.go:58).
- `update.Config.InContainer` defaults to `paths.InContainerFor(r.Identity, probes)` (config.go:533) and
  feeds the built-in container detector in `CheckManaged` -> `*ManagedInstallError` "pull a newer image
  of the container".
- `daemon/service.New` (manager.go:44) validates an optional `WithIdentity` first, then refuses with
  `*ContainerError` (matches `ErrContainer`) before Config validation and the GOOS switch (manager.go:58).
  `WithContainer(func() bool)` replaces detection outright; tests must pass it (or a forced env through
  the test-only `WithLookupEnv`) so no result depends on the host. See ADR-0029.
