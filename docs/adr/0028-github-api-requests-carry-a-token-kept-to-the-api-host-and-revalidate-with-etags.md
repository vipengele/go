# GitHub API requests carry a token kept to the API host, and revalidate with ETags

Status: extends ADR-0016, which it does not supersede: the web redirect is still read first and the
REST API is still the fallback. Applies to every page of the release list that ADR-0027 reads on
the Beta and Edge channels. Realises the `Token func(ctx) (string, error)` that ADR-0003 sketches
for the source. Keeps its cache beside the check state of ADR-0023.

An unauthenticated address is limited to sixty REST API requests an hour, shared by everyone behind
it (ADR-0016). `GitHubSource` reaches the API when the redirect does not answer on the Stable channel and always on
Beta and Edge, whose release list may take several pages (ADR-0027), so a tool needs a credential to spend its own quota, and a way to ask again
without paying for an unchanged answer.

## The rules

- **A token source.** `Config.Token` and `GitHubSource.Token` are `func(ctx) (string, error)`. A nil
  `Config.Token` reads `GH_TOKEN`, then `GITHUB_TOKEN` when that is empty, through `Config.Getenv`.
  A token of `""` means unauthenticated, which is the opt-out. A provider error fails the API
  request, wrapped. A supplied `Source` ignores `Config.Token`.
- **A separate, derived API client.** Each `Latest` call reads the token once, however many pages of
  the release list it reads, and only when it makes a REST API request. It builds a shallow copy of the injected `http.Client` whose transport adds
  `Authorization: Bearer <token>` only when the request host equals the `APIURL` host (compared
  case-insensitively, port included), on a clone of the request rather than the request itself, and
  whose `CheckRedirect` refuses a redirect to another host with a `*CrossHostRedirectError`.
  Downloads keep `Config.HTTPClient` and hold no authentication code; the web redirect probe keeps
  the plain client, so the token never reaches the web host.
- **A rejected token is an error.** A 401 to a request that carried a token is a
  `*TokenRejectedError`. The request is not retried without the token, so a stale `GH_TOKEN` shows up
  instead of quietly spending the shared anonymous quota.
- **Typed errors at the API call sites only.** A 403 or 429 with `X-RateLimit-Remaining: 0`, or with
  a `Retry-After`, is a `*RateLimitError{Reset, Limit, Remaining, Authenticated, Status}`. Any other
  403 stays a `*StatusError`. `Error()` names the reset in RFC 3339 UTC, reads no clock (a
  `Retry-After` delay becomes a reset time through `GitHubSource.Now`), and mentions the
  sixty-an-hour unauthenticated limit only when the request was unauthenticated. Both typed errors
  unwrap to the `*StatusError`. The shared status mapping is untouched, so a download that answers
  429 is still a `*StatusError`. `updatecmd` needs no change: the framework renders the error of an
  explicit `update` or `update --check`, and the background check stays silent, so the reset time and
  the token hint live in the error text.
- **Conditional requests from a cache file.** `GitHubSource.Cache` is a `ResponseCache` with `Get`
  and `Put` over a `CachedResponse{ETag, Body}`. A nil `Config.Source` gets
  `NewFileResponseCache`, the file `github-cache.json` in `StateDir`, written through `cli/state`
  (directory 0700, file 0600, atomic) as ADR-0023 does for the check state. Its I/O is bounded by the
  `ctx`, with the goroutine-and-select shape of `fileStore`.
  - A request carries `If-None-Match` only when an entry exists for its URL. A 304 is decoded
    through the path a 200 is. A 200 with an ETag replaces the entry once its body has been read
    within the read cap.
  - Bodies are cached up to the existing read caps, 8 MiB for the latest release and 32 MiB for a
    page of the list, so the list path, which always reaches the API, is cacheable. Each page is a
    URL of its own, with an entry of its own.
  - A cache that fails to load is a miss and one that fails to save is a skipped write. Nothing is
    written once the `ctx` is done. An error response never returns the cached body.
  - GitHub exempts a 304 from the rate limit only when the request is authenticated. Without a token
    a conditional request saves transfer, not quota.

## Considered options

- **Per-request headers on the shared client.** Rejected: it spreads authentication over every call
  site. The guarantee does not come from a separate client either: a client's transport sees every
  redirect hop, so a separate client alone would carry the token across a redirect. It comes from the
  host check in the wrapping transport, with `CheckRedirect` as a second line. The separate client
  keeps the download path free of authentication code.
- **Retrying a 401 without the token.** Rejected: it hides a stale credential behind the anonymous
  quota that many tools share.
- **Typed errors in the shared status mapping.** Rejected: downloads and the redirect probe have no
  rate-limit semantics, and a 429 from a download stays a plain `*StatusError`.
- **The cache in `State`.** Rejected: a custom `StateStore` would not carry the new fields, and the
  source would depend on `StateStore`.

## Consequences

- A tool with `GH_TOKEN` or `GITHUB_TOKEN` set authenticates without configuration, and sets
  `Token` to return `""` to opt out.
- Out of scope: `installscript`, ETag support in the `updatetest` fake, and `Link`-header pagination.
