---
about: "lydite rejects a component name containing '/' when it loads the config, so a nested project's component is named with '-' and keeps its real path in dir"
saw:
  - .lydite/components.yml
---

`lydite` validates `name:` in `.lydite/components.yml` at load time, so every subcommand fails on a name
such as `cli/core`: "name contains '/' — a name is a --component value, a CI matrix job name and an
artifact name, so it may hold only letters, digits, '.', '_' and '-'". The existing entries follow it:
the name is the project in kebab form and `dir:` holds the path (`vanity-worker` with
`dir: tools/vanity-worker`). A project at `source/<dir>/<sub>` takes the component name
`<dir>-<sub>` with `dir: source/<dir>/<sub>`.
