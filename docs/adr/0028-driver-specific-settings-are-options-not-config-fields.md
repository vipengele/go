# Driver-specific settings are typed options on New, not fields of the shared Config

Status: accepted. It extends ADR-0024, which selects the driver of the target operating system in
`New`, and keeps to its rule that validation follows the target `GOOS`.

`service.Config` describes a service in terms every service manager has: a name, a binary, its
arguments, an environment, a working directory, a user and a scope. It is validated against the
rules of the target `GOOS` (ADR-0024). The systemd driver also writes sandboxing directives
(`NoNewPrivileges=`, `ProtectSystem=`, `ReadWritePaths=` and the like) that launchd and the Windows
service control manager have no counterpart for. Where such a setting lives decides what the shared
`Config` means on the operating systems that ignore it.

## The rules

- **A driver-specific setting is a typed `Option` on `New`.** The systemd driver's hardening is
  `WithSystemdHardening(Hardening)`. `Hardening` is a struct with one typed field per directive, and
  `DefaultHardening(Scope)` returns the defaults a caller adjusts and passes back.
- **The option is validated in `New` on every `GOOS`.** A value that is invalid fails with an error
  wrapping `ErrInvalidConfig`, whether the target is Linux, macOS or Windows, so a mistake does not
  wait for the one host that renders it.
- **Other drivers ignore the option.** A caller builds one set of options for every operating system.
- **Without the option the driver applies `DefaultHardening` for the `Config`'s scope.**

## Considered options

- **Hardening fields on `Config`.** The shared type would carry settings that mean nothing on two of
  three operating systems, and validation would have to say when each applies. Rejected: `Config`
  stays the description every driver honours.
- **A `map[string]string` of raw directives.** It reaches any directive without a new field.
  Rejected: nothing validates a value before it reaches a unit file, a misspelt name is accepted
  silently, and each value would need a caller-side decision about quoting and `%` specifiers.
- **An exported driver type** that a caller constructs and configures directly. Rejected: it bypasses
  the single selection point of ADR-0024 and gives each driver its own constructor to learn.

## Consequences

- **`Config` has no systemd fields.** A new driver-specific setting is a new option and a typed value
  beside it, validated in `New`.
- **A caller sets hardening once** for every target, and only the systemd driver reads it.
- **The values are escaped, never rejected,** when the driver renders them: a path with a space or a
  `%` is written correctly rather than refused.
