---
about: "spec.Identity needs no explicit leading-dot check on DirName because validate.PathSegment requires a lowercase letter or digit first, so such a check would be unreachable"
saw:
  - source/cli/spec/identity.go
  - source/commons/validate/validate.go
---

`PathSegment` in `source/commons/validate/validate.go` accepts a first byte only from `a-z` and `0-9`,
so `.`, `..` and `.hidden` are rejected with `validate.ErrInvalid` before any later rule runs.
`checkDirName` in `source/cli/spec/identity.go` is therefore `validate.PathSegment("DirName", value)`
and nothing more: a separate leading-dot branch there could never be reached, so no test could
cover it. The no-leading-dot guarantee of `DirName` depends on that first-byte rule in `validate`,
and `source/cli/spec/identity_test.go` keeps a `.hidden` case that fails if `PathSegment` ever
admits a leading dot.
