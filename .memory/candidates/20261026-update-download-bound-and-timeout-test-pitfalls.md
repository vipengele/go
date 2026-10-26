---
about: "Config.Download pitfalls: limit+1 read bounds overflow at MaxInt64, and short per-request timeouts in tests are shared with the preceding request"
saw:
  - source/update/download.go
  - source/update/download_test.go
  - source/update/github.go
  - source/update/config.go
---

- Config.Validate accepts any non-negative MaxDownloadBytes, so math.MaxInt64 ("no cap") is valid. A read bound written as `MaxDownloadBytes+1` overflows negative there and io.LimitReader reads nothing, which surfaces as a ChecksumMismatchError on an empty file. download.go readBound saturates it. readLimited in github.go also adds 1 to its limit, and is safe only because its callers pass small constants (maxResponseBody, maxListResponseBody).
- DownloadTimeout is one deadline per request, but Download makes the checksums request first. A test that stalls the archive request with a short DownloadTimeout, or a short caller-context deadline, can see the deadline expire during the checksums request under -race load, giving a TimeoutError for the wrong URL. download_test.go avoids it with a generous archive budget, and an expiringContext that the archive handler expires once it is reached.
- GitHubSource tests in github_test.go (TestGitHubSource_Timeout) are timing-sensitive on a cold first run; they failed once before any download code existed.
