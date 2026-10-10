---
description: Every go.mod and go.work names a full patch release in its go directive, at or above the latest security patch.
---

# The `go` directive names a patched release

`go 1.27` means the first release of that line, 1.27.0, and CI installs exactly the toolchain the
directive of the project's `go.mod` names (`actions/setup-go` reads it). A directive without a patch
number therefore builds and tests against a standard library that still carries every vulnerability
fixed in later patch releases, and the `govulncheck` scan in lydite reports each as a finding.

Write the full version, `go 1.27.2`, and raise it when a standard-library advisory is fixed in a
newer patch. `go mod init` writes the version of the local toolchain, which may be older than the
latest patch; check it against `https://go.dev/dl/?mode=json` before committing.

## Applies to

- The `go` directive of every `source/<project>/go.mod` and of the root `go.work`.
- A `govulncheck` finding on `stdlib`: the fix is the version it names, in the directive.
