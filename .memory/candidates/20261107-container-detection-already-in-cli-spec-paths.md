---
about: "container detection already exists as cli/spec/paths.InContainer and update's CheckManaged already refuses in-place update in a container; daemon/service has no container gate and no override env var exists"
saw:
  - source/cli/spec/paths/container.go
  - source/update/managed.go
  - source/update/config.go
  - source/update/check.go
  - source/daemon/service/manager.go
  - source/daemon/go.mod
---

Found while planning issue #81 (container awareness).

- `paths.InContainer(Probes)` (container.go:51) probes /.dockerenv, /run/.containerenv, `container` and
  KUBERNETES_SERVICE_HOST env, /proc/1/cgroup; Probes{GOOS,LookupEnv,Exists,ReadFile} are the test seams; non-linux is false.
- `update.Config.InContainer` (config.go:179, default config.go:530-532) feeds a built-in detector in
  `CheckManaged` (managed.go:50-51) -> `*ManagedInstallError` "pull a newer image of the container". Cannot be turned off.
- Env opt-outs: update's `<PREFIX>_NO_UPDATE_CHECK` is any non-empty value ("0" included) (check.go:54-60);
  completioninstall's `_NO_COMPLETION` is exactly "1" (completioninstall/install.go:118); config binder uses strconv.ParseBool
  (config/bind.go:121). No precedent of a force-on/force-off tri-state.
- daemon requires cli/spec, filelock (and commons indirect), not update (daemon/go.mod); `daemon/service` is a package of daemon,
  with `ErrUnsupported` only for an unknown GOOS (service/manager.go:46-52). No container gate there.
