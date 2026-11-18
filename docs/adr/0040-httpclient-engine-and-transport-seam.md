# The httpclient engine and its Transport seam

Status: proposed.

`httpclient` sends a caller's requests through an **Engine**, the library that owns connections and
speaks the wire protocol, behind a **Transport** of `httpclient`'s own: one request in, one response
out. **Filters** wrap the Transport and pass the standard `*http.Request` and `*http.Response`. This
record chooses the default Engine and the second Engine that gets an adapter, if any is worth one.
The candidate Engines are the standard library's `net/http`, `fasthttp` and `quic-go`'s HTTP/3. The
client wrappers `resty`, `req`, `go-retryablehttp` and `heimdall` are read as prior art: for the API
a caller meets, and for the Engine each chose underneath. The criteria are protocol support;
connection pooling and reuse; streaming request and response bodies; cancellation through
`context.Context`; control over TLS and proxies; dependency weight and licence; maintenance (last
release, cadence, security advisories); how the Engine is faked in a test; and throughput, latency
and allocations. Between two designs, ease of use for the caller decides.

## Evidence

Every source below was read on 2026-11-18, and every maintenance fact (release dates, open issues,
advisories) is as of that day. A source is **independent** of a candidate when the candidate's
maintainers did not write it. A project's own documentation, source and issue tracker are cited for
what the project states about itself: its API, its limits and its maintainers' answers. A benchmark
published by a candidate's author is labelled as that author's claim and is not independent. A
comparison with no date or no method is left out.

**No independent benchmark of any candidate as a client, with its method and code, was found.** The
only client measurements located are in `fasthttp`'s README, which is its author's claim. The
independent benchmarks that exist measure servers or protocols, and are cited where they apply,
with what they do not establish.

The consumer floor of this repository is Go 1.25: a dependency whose `go.mod` names a later
version raises the floor for every consumer that pulls it in.

### `net/http`

Sources:

- [N1] `net/http` package documentation, https://pkg.go.dev/net/http, read 2026-11-18.
- [N2] `src/net/http/transport.go` on `master`,
  https://raw.githubusercontent.com/golang/go/master/src/net/http/transport.go, read 2026-11-18.
- [N3] `src/net/http/client.go` on `master`,
  https://raw.githubusercontent.com/golang/go/master/src/net/http/client.go, read 2026-11-18.
- [N4] Go 1.27 release notes, https://go.dev/doc/go1.27, read 2026-11-18.
- [N5] Go release history, https://go.dev/doc/devel/release, read 2026-11-18.
- [N6] golang/go#37373, "x/net/http2: client can't fully utilize network resource",
  https://github.com/golang/go/issues/37373, read 2026-11-18.
- [N7] golang/go#63422, "net/http: http/2 connection management magnifies the effect of network
  timeouts", https://github.com/golang/go/issues/63422, read 2026-11-18.
- [N8] Go vulnerability database entries GO-2026-6605, GO-2026-6611, GO-2026-6612 and GO-2026-6090,
  `https://vuln.go.dev/ID/<id>.json`, read 2026-11-18.
- [N9] `net/http/httptest` documentation, https://pkg.go.dev/net/http/httptest, read 2026-11-18.
- [N10] Go licence, https://go.dev/LICENSE, read 2026-11-18.

What they establish:

- **Protocols.** HTTP/1.1, and HTTP/2 over TLS. `DefaultTransport` sets `ForceAttemptHTTP2: true`;
  a Transport given its own `Dial`, `DialTLS`, `DialContext` or `TLSClientConfig` disables HTTP/2
  unless `ForceAttemptHTTP2` is set or `Transport.Protocols` names it [N1, N2]. No HTTP/3: the Go 1.27
  release notes mention none [N4]. On `master`, `Protocols` carries an unexported HTTP/3 bit and the
  Transport a field for an HTTP/3 transport wired in from `net/http/internal/http3`, which no caller
  can set (see the standard-library HTTP/3 entry below) [N2].
- **Pooling and reuse.** `DefaultMaxIdleConnsPerHost` is 2; `DefaultTransport` keeps
  `MaxIdleConns: 100` and `IdleConnTimeout: 90s`; `MaxConnsPerHost` caps connections per host
  [N2]. A connection is reused only after its response body is read and closed [N1]. In Go 1.27,
  closing an HTTP/1 `Response.Body` drains unread content "up to a conservative limit, to allow
  better connection reuse" [N4].
- **HTTP/2 pooling, two reports from outside the Go team.** [N6], open since 2020-02-22 and labelled
  `NeedsInvestigation`: the HTTP/2 pool opens a new TCP connection only when every existing one has
  reached the server's `MaxConcurrentStreams`, so a client cannot spread load over more connections.
  [N7], open since 2023-10-06 and labelled `NeedsInvestigation`: under total packet loss on one
  connection, HTTP/2 keeps assigning requests to it; the reporter's test program, 30,000 requests
  with a 10 ms timeout, failed 25,000 over HTTP/2 against 3 over HTTP/1.1. Both are the reporters'
  measurements, not the Go team's.
- **Streaming.** A response body is an `io.ReadCloser` read on demand; a request body is an
  `io.Reader`, sent chunked over HTTP/1.1 when its length is unknown [N1].
- **Cancellation.** A request carries its `context.Context`, and ending the context cancels the
  request, including a body still being read; `Client.Timeout` bounds the connect, any redirects and
  the body read [N1, N3].
- **TLS and proxies.** `Transport.TLSClientConfig`, `DialContext` and `Proxy func(*Request)
  (*url.URL, error)`; `ProxyFromEnvironment` reads `HTTP_PROXY`, `HTTPS_PROXY` and `NO_PROXY` [N2].
- **Dependencies and licence.** Standard library: no module dependency. BSD-style licence [N10].
- **Maintenance.** go1.27.0 was released 2026-08-19, go1.27.2 on 2026-10-08 with security fixes
  including `net/http` and `crypto/tls`, and go1.27.4 on 2026-11-10 with security fixes including
  `net/http` [N5]. Advisories published 2026-10-08 [N8]: GO-2026-6605 (CVE-2026-56866), "HTTP/1
  client connection desynchronization after CONNECT rejection"; GO-2026-6611 (CVE-2026-78669),
  "Excessive CPU consumption from repeated initial window changes"; GO-2026-6612 (CVE-2026-78663),
  "Double flow control refund on HTTP/2 server streams", a server-side entry. GO-2026-6090
  (CVE-2026-56862, `crypto/tls`, post-handshake message limit) was published 2026-08-13 [N8]. Fixes
  reach a consumer through a patched Go release, not a module bump.
- **Faking in a test.** `httptest.NewServer` and `NewTLSServer` serve on loopback and hand back a
  client configured for them; `ResponseRecorder` records a handler's response [N9]. Go 1.27 adds
  `httptest.NewTestServer`, a server on an in-memory fake network for use with `testing/synctest`
  [N4]. `http.RoundTripper` is the interface `http.Client` calls for each request [N3], so a test can
  replace the Transport with its own implementation; no Go documentation presents that as a testing
  pattern.
- **Performance.** No benchmark of the `net/http` client by an independent author, with method and
  code, was found. The only client figures for it are in `fasthttp`'s README (see below), which is
  `fasthttp`'s author's claim.

Independent sources: two, [N6] and [N7], and both concern HTTP/2 connection management. Every other
fact rests on the Go project's own documentation, source and vulnerability database, which are
authoritative for what the API is and what was fixed, and say nothing comparative.

### `fasthttp`

Sources:

- [F1] `README.md` on `master`, https://raw.githubusercontent.com/valyala/fasthttp/master/README.md,
  read 2026-11-18.
- [F2] `client.go` on `master`, https://raw.githubusercontent.com/valyala/fasthttp/master/client.go,
  read 2026-11-18.
- [F3] `go.mod` on `master`, https://raw.githubusercontent.com/valyala/fasthttp/master/go.mod, read
  2026-11-18.
- [F4] valyala/fasthttp#1632, https://github.com/valyala/fasthttp/issues/1632, read 2026-11-18.
- [F5] valyala/fasthttp#1973, "Http3 support", https://github.com/valyala/fasthttp/issues/1973, read
  2026-11-18.
- [F6] valyala/fasthttp#144, "HTTP/2 support", https://github.com/valyala/fasthttp/issues/144, read
  2026-11-18.
- [F7] valyala/fasthttp#2198, "Client: add DoContext method for context.Context-aware request
  execution", https://github.com/valyala/fasthttp/issues/2198, read 2026-11-18.
- [F8] valyala/fasthttp#1579, https://github.com/valyala/fasthttp/issues/1579, read 2026-11-18.
- [F9] `fasthttpproxy` documentation, https://pkg.go.dev/github.com/valyala/fasthttp/fasthttpproxy,
  read 2026-11-18.
- [F10] `fasthttputil` documentation, https://pkg.go.dev/github.com/valyala/fasthttp/fasthttputil,
  read 2026-11-18.
- [F11] `fasthttpadaptor` documentation,
  https://pkg.go.dev/github.com/valyala/fasthttp/fasthttpadaptor, read 2026-11-18.
- [F12] Releases, https://api.github.com/repos/valyala/fasthttp/releases, and repository metadata,
  https://api.github.com/repos/valyala/fasthttp, read 2026-11-18.
- [F13] Release notes of v1.79.0,
  https://api.github.com/repos/valyala/fasthttp/releases/tags/v1.79.0, read 2026-11-18.
- [F14] Security policy, https://github.com/valyala/fasthttp/security, and repository advisories,
  https://api.github.com/repos/valyala/fasthttp/security-advisories, read 2026-11-18.
- [F15] GitHub advisory GHSA-fx95-883v-4q4h, https://github.com/advisories/GHSA-fx95-883v-4q4h, read
  2026-11-18.
- [F16] V. Soyvural, "I benchmarked fasthttp vs net/http", published 2026-03-27,
  https://dev.to/veysi/i-benchmarked-fasthttp-vs-nethttp-the-results-surprised-me-1ho6, with code at
  https://github.com/soyvural/fasthttp-vs-nethttp, read 2026-11-18.

What they establish:

- **Protocols.** HTTP/1.x only. A maintainer, 2023-10-12: "No, fasthttp doesn't support HTTP2" [F4].
  A maintainer, 2025-03-10: "There's no HTTP2 support. I doubt we will see HTTP3" [F5]. The HTTP/2
  issue has been open since 2016-07-20 [F6].
- **Pooling and reuse.** Pools are per host, bounded by `MaxConnsPerHost`, with
  `MaxIdleConnDuration` and `MaxConnDuration`. When a host has no free connection, `Do` returns
  `ErrNoFreeConns` unless `MaxConnWaitTimeout` is set [F2]. A pooling bug, connections kept active
  with `DisableKeepalive` set, is open since 2023-06-19 [F8].
- **Streaming.** Response bodies are buffered unless `StreamResponseBody` is set [F2]. The README
  warns that a non-positive `MaxResponseBodySize`, the default, "disables the response body limit
  and buffered responses may consume unbounded memory" [F1]. A request body can be streamed with
  `SetBodyStream` [F1].
- **Cancellation.** The `Client` has no `context.Context` API: it offers `Do`, `DoTimeout`,
  `DoDeadline` and `DoRedirects`, and `client.go` does not mention `context.Context` [F2]. A request
  for a `DoContext` method is open since 2026-05-01, labelled `help wanted` [F7]. A deadline is the
  only way to bound a call.
- **TLS and proxies.** `Client.TLSConfig` sets TLS [F2]. A proxy is not a field of the client: it is
  a `fasthttp.DialFunc`, built by `fasthttpproxy`'s `FasthttpHTTPDialer`, `FasthttpProxyHTTPDialer`,
  `FasthttpSocksDialer` or `Dialer.GetDialFunc(useEnv bool)` and set as the client's `Dial` [F9].
- **Types.** `fasthttp` has its own `Request`, `Response`, `URI` and `Args`, and pooled objects from
  `AcquireRequest` and `AcquireResponse` must not be used after release [F1]. In the README's words,
  "fasthttp doesn't provide API identical to net/http", and `net/http` "handles more HTTP corner
  cases" [F1]. `fasthttpadaptor` converts in the server direction only (`NewFastHTTPHandler`,
  `ConvertRequest`); the project ships no client-side `http.RoundTripper` [F11]. Using `fasthttp`
  behind a Transport that passes `*http.Request` and `*http.Response` therefore converts each request
  and each response in an adapter. That conversion is a cost of choosing it as a second Engine.
- **Dependencies and licence.** MIT [F12]. `go.mod` names `go 1.26.0` and requires
  `klauspost/compress`, `molecule-man/go-brrr`, `valyala/bytebufferpool`, `golang.org/x/crypto`,
  `golang.org/x/net` and `golang.org/x/sys` [F3]. The README: "Go 1.26.0 or newer is required" [F1],
  above this repository's consumer floor of Go 1.25.
- **Maintenance.** v1.79.0 was published 2026-11-16, v1.78.0 2026-11-09, v1.77.0 2026-10-26;
  releases are roughly monthly, with a gap from v1.69.0 (2026-01-05) to v1.70.0 (2026-04-07). The
  repository has 64 open issues and was last pushed 2026-11-17 [F12]. It has published no
  repository security advisory, and its security policy states that it skips CVE assignment [F14]. The
  v1.79.0 notes carry a "Security fixes and hardening" section, including pooled compression state
  leaking between streams and redirect credential stripping [F13]. The one advisory in GitHub's
  database is GHSA-fx95-883v-4q4h, a path traversal published 2022-03-18 [F15].
- **Faking in a test.** `fasthttputil.NewInmemoryListener` gives an in-memory listener a client
  reaches through its `Dial` field [F10]. A test server over real TCP, including an `httptest`
  server, also serves it, since it speaks HTTP/1.x.
- **Performance, the author's claim.** The README states "fasthttp client is up to 4 times faster
  than net/http" [F1]. Its figures, `GOMAXPROCS=1` on an Intel Xeon at 2.20 GHz:
  `BenchmarkNetHTTPClientGetEndToEnd1TCP` 55,619 ns/op, 6,296 B/op, 70 allocs/op against
  `BenchmarkClientGetEndToEnd1TCP` 26,558 ns/op, 0 B/op, 0 allocs/op; over an in-memory connection,
  26,878 ns/op and 69 allocs/op against 4,827 ns/op and 0 allocs/op [F1]. The README states no Go
  version for them. The same README warns that "Blind switching from net/http to fasthttp won't
  give you performance boost" and that "For most cases `net/http` is much better" [F1].
- **Performance, independent.** [F16] publishes its method (Go 1.23.4, Apple M2 Pro, load from
  `hey`) and code, and measures `fasthttp` and `net/http` as **servers**. It establishes nothing
  about either client.

Independent sources: two, [F15] and [F16], and neither concerns the client: one is a 2022 advisory,
the other a server benchmark. Everything this record knows about `fasthttp` as a client rests on
its own README, source and tracker, and its client performance on its author's claim alone.

### `quic-go` HTTP/3

Sources:

- [Q1] `http3/transport.go` on `master`,
  https://raw.githubusercontent.com/quic-go/quic-go/master/http3/transport.go, read 2026-11-18.
- [Q2] HTTP/3 client documentation, https://quic-go.net/docs/http3/client/, read 2026-11-18.
- [Q3] HTTP/3 server documentation, https://quic-go.net/docs/http3/server/, read 2026-11-18.
- [Q4] `http3` package documentation, https://pkg.go.dev/github.com/quic-go/quic-go/http3, read
  2026-11-18.
- [Q5] `go.mod` on `master`, https://raw.githubusercontent.com/quic-go/quic-go/master/go.mod, read
  2026-11-18.
- [Q6] quic-go/quic-go#3370, "RoundTripper and Proxy", https://github.com/quic-go/quic-go/issues/3370,
  read 2026-11-18.
- [Q7] Repository security advisories,
  https://api.github.com/repos/quic-go/quic-go/security-advisories, read 2026-11-18.
- [Q8] Releases, https://api.github.com/repos/quic-go/quic-go/releases, and repository metadata,
  https://api.github.com/repos/quic-go/quic-go, read 2026-11-18.
- [Q9] X. Zhang et al., "QUIC is not Quick Enough over Fast Internet", Proceedings of the ACM Web
  Conference 2024 (WWW '24), pages 2713–2722, https://arxiv.org/abs/2310.09423, read 2026-11-18.

What they establish:

- **Protocols.** HTTP/3 only. `http3.Transport` implements `http.RoundTripper`, refuses any scheme
  but `https` and sets ALPN to `h3` [Q1, Q2, Q4]. It runs over UDP and has no TCP fallback and no
  `Alt-Svc` handling: the server documentation describes advertising HTTP/3 through `Alt-Svc` [Q3],
  and a client that should fall back to HTTP/2 or HTTP/1.1 has to arrange it itself [Q1].
- **Pooling and reuse.** The Transport keeps a map of connections by host and closes idle ones on
  `CloseIdleConnections`; with no `Dial` set, it opens one UDP socket on the first request and
  shares it [Q1]. The documentation names no per-host connection cap or idle timeout [Q2, Q4].
- **Streaming.** Request and response bodies are the standard `io.Reader` and `io.ReadCloser` of
  `*http.Request` and `*http.Response`, since it implements `http.RoundTripper` [Q4].
- **Cancellation.** The dial waits on the request's context. A failed request is retried only when
  it can be shown not to have been sent; once a body has been written, the error asks for
  `Request.GetBody` [Q1].
- **TLS and proxies.** `TLSClientConfig` and a `ClientSessionCache` for resumption; 0-RTT only
  through `MethodGet0RTT` and `MethodHead0RTT` [Q2]. No proxy: `transport.go` has no `Proxy` field
  and does not mention proxies [Q1], and an issue asking how to use a system proxy is open since
  2022-04-05 [Q6]. `Transport.Dial` is the only hook [Q1].
- **Dependencies and licence.** MIT [Q8]. `go.mod` names `go 1.26.0`, above this repository's
  consumer floor of Go 1.25, and requires `quic-go/qpack`, `golang.org/x/crypto`, `golang.org/x/net`,
  `golang.org/x/sync` and `golang.org/x/sys`, besides test dependencies [Q5]. The module is
  pre-1.0, at v0.65.0 [Q8].
- **Maintenance.** v0.65.0 was published 2026-11-03, v0.64.1 2026-10-19, v0.64.0 2026-10-16, v0.63.0
  2026-09-22, roughly monthly; last pushed 2026-11-16 [Q8]. Eight repository advisories since 2023
  [Q7], the most recent three: GHSA-vvgj-x9jq-8cj9 (CVE-2026-40898, 2026-05-31, medium), QPACK
  trailer expansion memory exhaustion, affecting `<= v0.59.0` and patched in v0.59.1;
  GHSA-g754-hx8w-x2g6 (CVE-2025-64702, 2025-12-11, medium), QPACK header expansion memory
  exhaustion; GHSA-47m2-4cr7-mhcw (CVE-2025-59530, 2025-10-10, high), a client crash on a premature
  `HANDSHAKE_DONE` frame.
- **Faking in a test.** As an `http.RoundTripper` it is replaced like any other [Q4], and `Dial` and
  `TLSClientConfig` are injectable [Q1]. A real HTTP/3 exchange in a test needs `http3.Server` on a
  loopback UDP socket [Q3]; `httptest` offers no HTTP/3 server [N9].
- **Performance.** [Q9] is independent and peer-reviewed: over fast links the UDP+QUIC+HTTP/3 stack
  shows a data rate up to 45.2% lower than TCP+TLS+HTTP/2, a gap that grows with bandwidth, which
  the authors trace to receiver-side processing and QUIC's user-space acknowledgements. It measures
  the protocol stack in browsers and lightweight clients, and establishes nothing about `quic-go`'s
  implementation in particular. No benchmark comparing `quic-go` with `net/http`, with method and
  code, was found.

Independent sources: one, [Q9], and it concerns the protocol rather than this library. The
advisories [Q7] are the maintainers' own disclosures. The rest rests on `quic-go`'s documentation
and source.

### Client wrappers (prior art)

Sources:

- [W1] `resty` v3 `client.go`, https://raw.githubusercontent.com/go-resty/resty/v3/client.go, and
  `go.mod`, https://raw.githubusercontent.com/go-resty/resty/v3/go.mod, read 2026-11-18.
- [W2] `resty` releases, https://api.github.com/repos/go-resty/resty/releases, read 2026-11-18.
- [W3] `req` `transport.go`, https://raw.githubusercontent.com/imroc/req/master/transport.go, and
  `go.mod`, https://raw.githubusercontent.com/imroc/req/master/go.mod, read 2026-11-18.
- [W4] `req` releases, https://api.github.com/repos/imroc/req/releases, read 2026-11-18.
- [W5] `go-retryablehttp` `client.go`,
  https://raw.githubusercontent.com/hashicorp/go-retryablehttp/main/client.go, and
  `roundtripper.go`,
  https://raw.githubusercontent.com/hashicorp/go-retryablehttp/main/roundtripper.go, read 2026-11-18.
- [W6] `go-retryablehttp` tags and commits,
  https://api.github.com/repos/hashicorp/go-retryablehttp/tags and
  https://api.github.com/repos/hashicorp/go-retryablehttp/commits, read 2026-11-18.
- [W7] `heimdall` `README.md`, https://raw.githubusercontent.com/gojek/heimdall/master/README.md, read
  2026-11-18.
- [W8] `heimdall` releases, https://api.github.com/repos/gojek/heimdall/releases, read 2026-11-18.
- [W9] `go-retryablehttp` `go.mod`,
  https://raw.githubusercontent.com/hashicorp/go-retryablehttp/main/go.mod, and `heimdall` `go.mod`,
  https://raw.githubusercontent.com/gojek/heimdall/master/go.mod, read 2026-11-18.

What they establish:

- **`resty`** (`resty.dev/v3`, MIT) builds on `net/http`: `Client()` returns its `*http.Client`, and
  `SetTransport` takes any `http.RoundTripper`, so the Engine is swapped at the standard interface
  [W1]. Its `go.mod` names `go 1.23.0` [W1]. The latest stable release is v2.17.2 (2026-02-14); v3 is
  at v3.0.0-rc.4 (2026-09-06) [W2].
- **`req`** (MIT) carries its own fork of the standard library's transport with HTTP/1.1, HTTP/2 and
  HTTP/3, the last through `quic-go` v0.61.0, and requires `refraction-networking/utls` [W3]. It
  tracks `Alt-Svc` to move to HTTP/3 [W3]. Its proxy, dial and TLS-handshake hooks are documented as
  "only valid for HTTP1 and HTTP2" [W3]: one client, two sets of rules depending on the protocol in
  use. A consumer cannot replace its Engine. Its `go.mod` names `go 1.25.0` [W3]; the latest release
  is v3.61.0 (2026-08-13) [W4].
- **`go-retryablehttp`** (MPL-2.0) wraps an `HTTPClient *http.Client`; retry policy is the function
  types `CheckRetry` and `Backoff` [W5]. `StandardClient()` returns a plain `*http.Client` whose
  Transport is a retrying `http.RoundTripper`, so its behaviour reaches code that only knows the
  standard client [W5]. The latest tag is v0.7.8, with no GitHub Release; the last commit is
  2026-11-16 [W6].
- **`heimdall`** (Apache-2.0) accepts any `Doer`, "the standard interface implemented by HTTP
  clients (including the standard library's `net/*http.Client`)", through `WithHTTPClient`, and adds
  a hystrix-like circuit breaker [W7]. A `Doer` is the narrowest Engine seam of the four: one method,
  `Do(*http.Request) (*http.Response, error)` [W7]. v8.0.0 was released 2026-07-01 [W8].

Three of the four keep the standard `*http.Request` and `*http.Response` at their Engine seam
(`resty` and `go-retryablehttp` through `http.RoundTripper`, `heimdall` through `Doer`); `req`
reaches HTTP/3 by forking the transport, at the price of protocol-dependent proxy and dial hooks.
None of the four requires `fasthttp` in its `go.mod` [W1, W3, W9].

Independent sources: none. The wrappers are read for the API they offer and the Engine seam they
chose, which each one's own source establishes directly; they are not candidate Engines and carry no
claim this record relies on beyond that.

### Further candidates

- **Standard-library HTTP/3.** golang/go#70914, "proposal: x/net/http3: add experimental HTTP/3
  implementation", opened 2024-12-18, is open and labelled `Proposal-Hold`
  (https://github.com/golang/go/issues/70914, read 2026-11-18). The implementation lives on
  `master` of the Go repository in `src/net/http/internal/http3`, whose package documentation says
  "This package is a work in progress. It is not ready for production usage. Its API is subject to
  change without notice."
  (https://raw.githubusercontent.com/golang/go/master/src/net/http/internal/http3/doc.go, read
  2026-11-18). `net/http`'s `Protocols` carries an HTTP/3 bit that only the package itself can set
  [N2], and the Go 1.27 release notes mention no HTTP/3 [N4]. Not scored: no released Go version
  exposes it, and no import path reaches it.
- **`x/net` internal HTTP/3.** The survey's lead was `golang.org/x/net/internal/http3`. On
  2026-11-18 `golang/net` has no `http3` directory; the commit "net/http/internal/http3: move HTTP/3
  from x/net to std" of 2026-09-21 moved it into the Go repository
  (https://api.github.com/repos/golang/go/commits?path=src/net/http/internal/http3, read
  2026-11-18). It is the standard-library entry above, not a separate candidate.
- **Forks of `quic-go`'s `http3`.** The survey turned up forks of the package. None is scored: a fork
  shares `quic-go`'s design, its QUIC stack and its evidence, and adds a maintainer whose cadence and
  advisories would need evidence of their own. `req`, the one wrapper with HTTP/3, requires `quic-go`
  itself rather than a fork [W3].
