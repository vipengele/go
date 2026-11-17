---
about: "pflag v1.1.0 reports an invalid integer flag value as \"must be an integer\" instead of the strconv.ParseInt text, and cli/core usage-error tests pin that wording"
saw:
  - source/cli/core/go.mod
  - source/cli/core/execute_test.go
---
- cli/core requires `github.com/spf13/pflag v1.1.0` (go.mod). With it, `--times many` on an int flag
  renders `tool: greet: invalid argument "many" for "--times" flag: must be an integer`; pflag
  v1.0.10 appended `strconv.ParseInt: parsing "many": invalid syntax`.
- `TestExecuteArgsExitCodes` in execute_test.go pins the exact stderr, so a pflag bump that changes
  parse-error text fails there first, and a merge of such a bump into a branch that edits that test
  table needs the expectation carried over.
