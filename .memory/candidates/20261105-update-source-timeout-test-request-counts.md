---
about: "updatetest counts a request only once its handler runs, so a client deadline can end a request the server never counts; read Server.Requests only after the server has shut down, and give answering requests before a stalled one a seconds-long per-request timeout"
saw:
  - source/update/github_test.go
  - source/update/updatetest/server.go
  - source/update/github.go
---

- `updatetest.Server` increments a route's count in its handler (`serve`, before the `Slow` wait;
  source/update/updatetest/server.go). A 50ms client deadline can cancel a request before any
  handler reads it, so "requests == 1" for the stalled route is not guaranteed by the code; and a
  count read right after `Latest` returns can race the handler. `TestGitHubSource_Timeout`
  (source/update/github_test.go) runs the lookup in a nested subtest so the kit server's cleanup
  (httptest.Server.Close: waits for started handlers, closes unstarted connections) runs before
  the counts are read, and asserts the stalled route "at most once", earlier routes exactly once,
  later routes never.
- `GitHubSource.Timeout` is per request (source/update/github.go;
  `TestGitHubSource_TimeoutIsPerRequest`), so it also bounds every answering request before the
  stalled one. Under `-race` and parallel load a 50ms timeout expired on the redirect before the
  stalled API request and named the wrong URL; that case uses 2s. A short deadline is safe only
  where the stalled request is the first one.
- The `Server` kit's `close` is unexported and panics on a second call (`close(s.done)`), so a test
  cannot shut it down early itself; a nested subtest whose cleanup does it is the way.
