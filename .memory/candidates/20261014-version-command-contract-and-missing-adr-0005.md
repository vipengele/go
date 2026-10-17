---
about: "the cli version command's --json shape is a contract owned by cli/core (ADR-0007, amending ADR-0003); cobra is allowed only in cli/core/internal/cobra (ADR-0005)"
saw:
  - docs/adr/0003-the-library-is-three-projects-cli-update-and-daemon.md
  - docs/adr/0004-projects-nest-and-take-vanity-module-paths.md
  - docs/adr/0007-the-cli-project-owns-the-version-json-contract.md
  - source/cli/core/buildinfo/doc.go
  - source/cli/core/.golangci.yml
---

ADR-0003's staged-binary preflight runs `<staged> version --json`. ADR-0007 makes cli/core define and
document that shape, in the package documentation of `source/cli/core/buildinfo/doc.go`, versioned by
a `schema` field (1; adding a key keeps it, renaming, removing or retyping a key increments it);
`update` only reads it. ADR-0003 carries the amendment in its Status line and an inline pointer.

ADR-0005 (`docs/adr/0005-commands-are-a-framework-neutral-model-in-cli-spec-and-cobra-is-an-internal-adapter.md`)
supersedes ADR-0003's rule on where cobra is allowed: commands are `cli/spec` data and cobra is
imported only by `source/cli/core/internal/cobra`, enforced by the `cobra-stays-in-internal-cobra`
depguard rule in `source/cli/core/.golangci.yml` and a `github.com/spf13/cobra` deny in
`source/cli/spec/.golangci.yml`. `version.New` returns a `*spec.Command`.
