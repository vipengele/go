---
about: "TestGitHubSource_TimeoutIsPerRequest leaves each request only 0.9s of slack (2s timeout, 1.1s sleep), so it can fail under the full -race -shuffle suite's parallel load while passing alone"
saw:
  - source/update/github_test.go
  - source/update/github.go
---

- `TestGitHubSource_TimeoutIsPerRequest` (source/update/github_test.go) delays each of two requests
  by `delay = 1100ms` against `timeout = 2s`, so the pair exceeds a shared timeout while each request
  fits its own. The slack per request is `timeout - delay`, 0.9s.
- Under `GOWORK=off go test -race -shuffle=on ./...` with the whole package's parallel tests, one
  run failed with `context deadline exceeded` on the API request; the same test passed 5/5 alone
  with `-race -count=5`, with and without the branch's changes. A rerun of the full suite passed.
  Treat a lone failure of it as load, not a regression; widening the gap (larger timeout, same
  delay) is the fix if it recurs in CI.
