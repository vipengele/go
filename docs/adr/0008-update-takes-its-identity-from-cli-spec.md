# `update` takes its `Identity` from `cli/spec`

Status: amends ADR-0003 and ADR-0004.

`update.Config` embeds `spec.Identity`, the type ADR-0004 places in `cli/spec`. The environment
variable that opts a tool out of update checks is derived from the `Identity` with its `EnvVar`
method, so `Config` has no field of its own for it. A tool declares its `Identity` once in `main`
and hands the same value to `cli/core` and to `update`.

## Dependencies

```
update ──► cli/spec
update ──► commons
```

- `update` requires `cli/spec` and `commons`, and no other sibling. It does not import `cli/core`.
- A tool that wants only self-update takes `cli/spec` and `commons` along with `update`.
  ADR-0003 promised such a tool would take nothing else; that promise is withdrawn.
- Cobra stays out of `update` except in the `updatecmd` package. The `depguard` rules in
  `source/update/.golangci.yml` enforce it.

## Considered options

- **`Identity` re-declared on `update.Config`.** Keeps `update` free of `cli/spec`, but the same
  values are then declared in two places, and a tool whose two declarations drift updates from a
  repository or reads an environment variable that its shell does not.
- **`Identity` moved to `commons`.** Gives `update` the type without `cli/spec`, but puts a
  CLI-and-update concept into the project meant to know nothing of either. ADR-0004 rejected it.
- **A fourth project holding only the shared identity.** ADR-0003 rejected it as a module to release
  for a handful of lines, and ADR-0004 placed `Identity` in `cli/spec` instead.
