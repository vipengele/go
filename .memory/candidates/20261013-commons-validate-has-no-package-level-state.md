---
about: "commons enables gochecknoglobals, so validate uses hand-written byte loops instead of package-level regexps and its only exported package-level variables are the Err-prefixed sentinels"
saw:
  - source/commons/.golangci.yml
  - source/commons/validate/validate.go
  - source/commons/envvar/envvar.go
---

`source/commons/.golangci.yml` enables `gochecknoglobals` on top of the `standard` linter set.
That linter exempts error sentinels named with the `Err` prefix and flags every other
package-level variable, including a precompiled `regexp.MustCompile`. `validate` therefore
checks each rule with a byte loop over the input (`Slug`, `GitHubOwner`, `RepoName`, `EnvVar`,
`PathSegment` in `source/commons/validate/validate.go`) and keeps `ErrEmpty` and `ErrInvalid` as
its only package-level variables; the length bounds are constants. The loops accept only the
bytes `a-z`, `A-Z`, `0-9`, `-`, `.` and `_`, so no accepted value holds a path separator, NUL,
whitespace or a non-ASCII byte, which the fuzz tests in the package assert.

`envvar.Name` (`source/commons/envvar/envvar.go`) validates `prefix` and `suffix` with
`validate.EnvVar` and then the joined name as well, because two parts each within
`validate.MaxEnvVarLen` join to a name that can exceed it. Test files are subject to the same
linter, so tables live inside the test functions.
