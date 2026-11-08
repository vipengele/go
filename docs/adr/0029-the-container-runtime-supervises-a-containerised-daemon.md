# The container runtime supervises a containerised daemon

Status: accepted. It amends ADR-0024 (`0024-service-drivers-are-selected-at-runtime.md`), whose
`service` package is standard-library only and whose `New` validates the `Config` and then
switches on the `GOOS`: `service` imports `cli/spec` and `cli/spec/paths`, and `New` checks for a
container before it validates the `Config` and before the `GOOS` switch. It keeps to ADR-0013
(`0013-stdout-carries-data-stderr-carries-messages.md`).

In a container the container runtime restarts the process, so a systemd unit, a launchd job or a
Windows service inside it has nothing to supervise. `service.New` refuses with a `*ContainerError`,
which matches `ErrContainer`, and the daemon runs as the image's entrypoint.

## The rules

- **`<PREFIX>_CONTAINER` is a tri-state override.** Unset or empty leaves the decision to
  `paths.InContainer`, the Linux-only heuristic (`/.dockerenv`, `/run/.containerenv`, the
  `container` variable, `KUBERNETES_SERVICE_HOST`, the cgroup of PID 1). A value `strconv.ParseBool`
  reads decides: true forces on, false forces off. Any other non-empty value, such as `yes`, is
  true: setting the variable means forcing the container on. The override applies on every `GOOS`.
  `paths.InContainerFor` reads it with the tool's identity.
- **`New` refuses once, before any driver.** The check is in `service.New`, not in each driver, so
  a new operating system inherits it. It follows the validation of an optional `WithIdentity` and
  precedes the `Config` validation and the `GOOS` switch. An invalid `WithIdentity` is reported
  first, so a bad option is never accepted silently because the process happens to be in a
  container. `WithContainer` replaces detection in a test.
- **The refusal says what to do.** `ContainerError.Instead`: the container runtime supervises this
  process; run it as the image's entrypoint and give the container a restart policy that restarts
  on exit 0. A requested restart is a clean exit, so `on-failure` does not restart it.
- **Logs go to stderr.** The runtime collects stderr, `--log-format json` makes it structured, and
  ADR-0013 reserves stdout for data.
- **The update notice follows the container.** The background update check, which runs in its
  own goroutine and never delays the command, says `<name> <latest> is available (you're on
  <current>); pull a newer image of the container.` in a container, and, as the Instead of a
  managed install, in place of "Run '<name> update' to install."

## Considered options

- **A strict boolean that rejects other values.** An operator who sets the variable is asking to
  force detection; reading `yes` or `on` as an error or as off would surprise them, and an explicit
  false (`0`, `false`) remains the one way to force it off, so set means on.
- **The heuristic alone.** It reads Linux markers only and misses a runtime that sets none, such
  as a minimal image under a runtime that mounts no `/.dockerenv`, and gives no way to force it off
  on a host that happens to carry one.
- **A refusal in each driver.** Three copies of the check, each behind its own `GOOS`, and the
  Windows one never run in CI (ADR-0019, `0019-the-file-lock-is-its-own-project-filelock.md`).

## Consequences

- **`service` is no longer standard-library only.** It imports `cli/spec` for `Identity` and
  `cli/spec/paths` for detection.
- **The order of `New` is part of its contract:** `WithIdentity`, the container check, `Config`
  validation, the `GOOS` switch.
- **Every test fixes the container answer** with `WithContainer` or a forced `<PREFIX>_CONTAINER`,
  so no result depends on the host.
