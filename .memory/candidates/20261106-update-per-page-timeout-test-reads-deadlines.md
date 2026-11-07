---
about: "the GitHubSource per-page timeout test compares each request's context deadline through a recording transport instead of sleeping past the timeout, because sleeping against the 2s timeout across two pages fails under the full parallel -race run"
saw:
  - source/update/github_test.go
  - source/update/github.go
---

highestListed (github.go:276) bounds each page request with its own Timeout through `get`
(github.go:345). The test that proves it uses `deadlineTransport` (github_test.go, near line 1497)
to record `r.Context().Deadline()` for each request. It then checks that page 2's deadline is later
than page 1's, which a single shared timeout could not produce.

The sleep-based pattern of TestGitHubSource_TimeoutIsPerRequest also works: stall each request
close to the 2s per-request timeout. Applied to two list pages, it passes on its own and times out
on page 2 in the full `go test -race -shuffle=on ./...` run. Any new multi-request timeout property
should assert on deadlines, not on wall-clock sleeps (see the earlier candidates on wall-clock
margins and source timeout request counts).
