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

Consumers of this repository are on Go 1.27 or later.

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
  `golang.org/x/net` and `golang.org/x/sys` [F3]. The README: "Go 1.26.0 or newer is required" [F1].
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
- **Dependencies and licence.** MIT [Q8]. `go.mod` names `go 1.26.0` and requires `quic-go/qpack`,
  `golang.org/x/crypto`, `golang.org/x/net`, `golang.org/x/sync` and `golang.org/x/sys`, besides
  test dependencies [Q5]. The module is pre-1.0, at v0.65.0 [Q8].
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

## Benchmark

This measurement checks the published claims above against the three Engines on one machine. It
does not replace them, and it scores nothing.

**What it measures.** Client and server run in one process over loopback, so every figure is the
CPU cost of a client stack and a server stack together, sharing the same cores. There is no packet
loss, no added latency and no bandwidth limit: nothing here says how an Engine behaves on a real
network. `B/op` and `allocs/op` are counted for the whole process, server included. All TCP rows
use the same `net/http` server, so a difference between two TCP rows is a difference between their
clients; the HTTP/3 row uses `quic-go`'s `http3.Server`, so it compares stack against stack. In the
parallel workloads `ns/op` is wall time divided by the requests completed, the inverse of
throughput, not a request's latency.

**Engines and transports.** Six rows. `-h1` is plain TCP, `-h1tls` and `-h2tls` are TLS 1.3 over
TCP, `-h3` is QUIC over UDP:

- `nethttp-h1`, `nethttp-h1tls`: `net/http` with `Transport.Protocols` set to HTTP/1 only.
- `nethttp-h2tls`: `net/http` with `Transport.Protocols` set to HTTP/2 only, against the same TLS
  server, which offers both through ALPN.
- `fasthttp-h1`: `fasthttp.Client` over plain TCP, its native case. `fasthttp-h1tls`: the same
  client over TLS against the TLS server; it offers no ALPN and gets HTTP/1.1.
- `quicgo-h3`: `http3.Transport` against `http3.Server` on a loopback UDP socket.

**Workloads.** Every request reads the whole response body, checks status 200 and the body length,
and closes or releases the response. The handler answers `GET /?n=N` with `N` bytes and `POST` with
the decimal length of the body it read.

- `Small`: 1 KiB response, one goroutine, one reused connection.
- `Large`: 16 MiB response, one goroutine, one reused connection. `net/http` and `quic-go` copy the
  body to `io.Discard` as it streams; `fasthttp` buffers the whole body, its default, and the check
  reads it from memory.
- `Upload`: 16 MiB request body, small response, one goroutine, one reused connection.
- `Reuse`: 1 KiB responses from `b.RunParallel` at GOMAXPROCS goroutines (15), the pool capped at
  15 connections (`MaxConnsPerHost` and `MaxIdleConnsPerHost` for `net/http`, `MaxConnsPerHost`
  for `fasthttp`; `http3.Transport` has no cap).
- `Concurrent`: 1 KiB responses from exactly 15 and exactly 64 goroutines sharing one client, the
  pool capped at 64. GOMAXPROCS of 15 does not divide 64, so the goroutines are started by the
  harness rather than `b.RunParallel`.

The parallel workloads warm the pool with one concurrent request per goroutine before the timer
starts. `conns` is the number of connections the server accepted while the benchmark ran, the
warm-up included; `errors` counts failed requests, `fasthttp`'s `ErrNoFreeConns` and `quic-go`
errors among them; `srvlog` counts lines the TCP server logged inside the measured window.

### Environment

- Machine: Apple M5 Pro, 15 cores (`hw.ncpu`), 24 GiB RAM, macOS 26.7.2 (25G332), darwin/arm64.
- Go: go1.27.4; GOMAXPROCS 15, the default.
- Modules: `github.com/valyala/fasthttp` v1.79.0, `github.com/quic-go/quic-go` v0.65.0 (with
  `quic-go/qpack` v0.6.0), `golang.org/x/net` v0.62.0, `golang.org/x/crypto` v0.58.0,
  `golang.org/x/sys` v0.50.0. Both `fasthttp` and `quic-go` name `go 1.26.0`, which go1.27.4
  satisfies.
- benchstat: `golang.org/x/perf` v0.0.0-20261117185222-4febdb082b2c.
- Date: 2026-11-18. The listed run took 08:32 to 08:36 UTC; the two others (see Results) ran from
  08:23 to 08:32 UTC.

### Commands

```sh
mkdir bench && cd bench
go mod init bench
go get github.com/valyala/fasthttp@v1.79.0 github.com/quic-go/quic-go@v0.65.0
go mod edit -go=1.27.4 && go mod tidy    # after writing harness_test.go
go test -run '^TestH2ColdStart$' -v -count=3 .
go test -run '^$' -bench . -benchmem -count=5 > results.txt
go install golang.org/x/perf/cmd/benchstat@latest && benchstat results.txt
```

`go.mod` after `go mod tidy`:

```
module bench

go 1.27.4

require (
	github.com/quic-go/quic-go v0.65.0
	github.com/valyala/fasthttp v1.79.0
)

require (
	github.com/klauspost/compress v1.20.4 // indirect
	github.com/molecule-man/go-brrr v1.3.1 // indirect
	github.com/quic-go/qpack v0.6.0 // indirect
	github.com/valyala/bytebufferpool v1.0.0 // indirect
	golang.org/x/crypto v0.58.0 // indirect
	golang.org/x/net v0.62.0 // indirect
	golang.org/x/sys v0.50.0 // indirect
	golang.org/x/text v0.43.0 // indirect
)
```

### Harness

`harness_test.go`:

```go
package bench

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"fmt"
	"io"
	"log"
	"math/big"
	"net"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/quic-go/quic-go"
	"github.com/quic-go/quic-go/http3"
	"github.com/valyala/fasthttp"
)

const (
	small = 1 << 10  // 1 KiB
	large = 16 << 20 // 16 MiB
)

var payload = bytes.Repeat([]byte("x"), large)

// handler answers GET /?n=N with N bytes and POST with the decimal length of the body it read.
func handler(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		n, err := io.Copy(io.Discard, r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		s := strconv.FormatInt(n, 10)
		w.Header().Set("Content-Length", strconv.Itoa(len(s)))
		io.WriteString(w, s)
		return
	}
	n, _ := strconv.Atoi(r.URL.Query().Get("n"))
	w.Header().Set("Content-Length", strconv.Itoa(n))
	w.Write(payload[:n])
}

// client is one Engine's view of the shared work: read the whole body, check 200 and the length.
type client interface {
	get(url string, n int) error
	post(url string, body []byte) error
	close()
}

type engine struct {
	name  string
	base  string        // scheme://host:port
	conns *atomic.Int64 // connections the server accepted
	new   func(maxConns int) client
}

var (
	engines []engine
	roots   = x509.NewCertPool()
	srvErrs atomic.Int64 // lines the TCP servers logged, mostly TLS handshakes the client abandoned
)

type countWriter struct{}

func (countWriter) Write(p []byte) (int, error) { srvErrs.Add(1); return len(p), nil }

func TestMain(m *testing.M) {
	cert := selfSigned()
	srvTLS := &tls.Config{Certificates: []tls.Certificate{cert}}
	cliTLS := func() *tls.Config { return &tls.Config{RootCAs: roots} }

	h1, h1n := serveTCP(nil)              // plain HTTP/1.1
	h1s, h1sn := serveTCP(srvTLS.Clone()) // HTTP/1.1 over TLS; ALPN offers h2 and http/1.1
	h3, h3n := serveH3(srvTLS.Clone())    // HTTP/3 over UDP

	nethttp := func(p func(*http.Protocols)) func(int) client {
		return func(maxConns int) client {
			var protos http.Protocols
			p(&protos)
			tr := &http.Transport{TLSClientConfig: cliTLS(), Protocols: &protos,
				MaxIdleConnsPerHost: maxConns, MaxConnsPerHost: maxConns}
			return &stdClient{c: &http.Client{Transport: tr}, closeFn: tr.CloseIdleConnections}
		}
	}
	fast := func(maxConns int) client {
		return &fastClient{c: &fasthttp.Client{TLSConfig: cliTLS(), MaxConnsPerHost: maxConns}}
	}
	engines = []engine{
		{"nethttp-h1", "http://" + h1, h1n, nethttp(func(p *http.Protocols) { p.SetHTTP1(true) })},
		{"nethttp-h1tls", "https://" + h1s, h1sn, nethttp(func(p *http.Protocols) { p.SetHTTP1(true) })},
		{"nethttp-h2tls", "https://" + h1s, h1sn, nethttp(func(p *http.Protocols) { p.SetHTTP2(true) })},
		{"fasthttp-h1", "http://" + h1, h1n, fast},
		{"fasthttp-h1tls", "https://" + h1s, h1sn, fast},
		{"quicgo-h3", "https://" + h3, h3n, func(int) client {
			tr := &http3.Transport{TLSClientConfig: cliTLS()}
			return &stdClient{c: &http.Client{Transport: tr}, closeFn: func() { tr.Close() }}
		}},
	}
	os.Exit(m.Run())
}

func selfSigned() tls.Certificate {
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "bench"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(24 * time.Hour),
		IPAddresses: []net.IP{net.IPv4(127, 0, 0, 1)}, IsCA: true, BasicConstraintsValid: true,
		KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign}
	der, _ := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	leaf, _ := x509.ParseCertificate(der)
	roots.AddCert(leaf)
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key, Leaf: leaf}
}

func serveTCP(tc *tls.Config) (string, *atomic.Int64) {
	n := new(atomic.Int64)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		panic(err)
	}
	srv := &http.Server{Handler: http.HandlerFunc(handler), TLSConfig: tc,
		ErrorLog: log.New(countWriter{}, "", 0), ConnState: func(_ net.Conn, s http.ConnState) {
			if s == http.StateNew {
				n.Add(1)
			}
		}}
	if tc != nil {
		go srv.ServeTLS(ln, "", "")
	} else {
		go srv.Serve(ln)
	}
	return ln.Addr().String(), n
}

func serveH3(tc *tls.Config) (string, *atomic.Int64) {
	n := new(atomic.Int64)
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		panic(err)
	}
	srv := &http3.Server{Handler: http.HandlerFunc(handler), TLSConfig: tc,
		ConnContext: func(ctx context.Context, _ *quic.Conn) context.Context { n.Add(1); return ctx }}
	go srv.Serve(pc)
	return pc.LocalAddr().String(), n
}

type stdClient struct {
	c       *http.Client
	closeFn func()
}

func (s *stdClient) check(resp *http.Response, want int64) error {
	defer resp.Body.Close()
	got, err := io.Copy(io.Discard, resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK || got != want {
		return fmt.Errorf("status %d, %d bytes, want %d", resp.StatusCode, got, want)
	}
	return nil
}

func (s *stdClient) get(url string, n int) error {
	resp, err := s.c.Get(url)
	if err != nil {
		return err
	}
	return s.check(resp, int64(n))
}

func (s *stdClient) post(url string, body []byte) error {
	resp, err := s.c.Post(url, "application/octet-stream", bytes.NewReader(body))
	if err != nil {
		return err
	}
	return s.check(resp, int64(len(strconv.Itoa(len(body)))))
}

func (s *stdClient) close() { s.closeFn() }

type fastClient struct{ c *fasthttp.Client }

func (f *fastClient) do(url string, body []byte, want int) error {
	req, resp := fasthttp.AcquireRequest(), fasthttp.AcquireResponse()
	defer fasthttp.ReleaseRequest(req)
	defer fasthttp.ReleaseResponse(resp)
	req.SetRequestURI(url)
	if body != nil {
		req.Header.SetMethod(fasthttp.MethodPost)
		req.SetBodyRaw(body)
	}
	if err := f.c.Do(req, resp); err != nil {
		return err
	}
	if resp.StatusCode() != fasthttp.StatusOK || len(resp.Body()) != want {
		return fmt.Errorf("status %d, %d bytes, want %d", resp.StatusCode(), len(resp.Body()), want)
	}
	return nil
}

func (f *fastClient) get(url string, n int) error { return f.do(url, nil, n) }
func (f *fastClient) post(url string, body []byte) error {
	return f.do(url, body, len(strconv.Itoa(len(body))))
}
func (f *fastClient) close() { f.c.CloseIdleConnections() }

func getURL(e engine, n int) string { return e.base + "/?n=" + strconv.Itoa(n) }

// serial runs op on one goroutine over one client capped at one connection, after a warm-up
// request has opened it; size is the body size behind MB/s, 0 for none.
func serial(b *testing.B, size int64, op func(engine, client) error) {
	for _, e := range engines {
		b.Run(e.name, func(b *testing.B) {
			c := e.new(1)
			defer c.close()
			if err := op(e, c); err != nil {
				b.Fatal(err)
			}
			before := e.conns.Load()
			b.SetBytes(size)
			b.ReportAllocs()
			for b.Loop() {
				if err := op(e, c); err != nil {
					b.Fatal(err)
				}
			}
			b.ReportMetric(float64(e.conns.Load()-before), "conns")
		})
	}
}

func BenchmarkSmall(b *testing.B) {
	serial(b, 0, func(e engine, c client) error { return c.get(getURL(e, small), small) })
}

func BenchmarkLarge(b *testing.B) {
	serial(b, large, func(e engine, c client) error { return c.get(getURL(e, large), large) })
}

func BenchmarkUpload(b *testing.B) {
	serial(b, large, func(e engine, c client) error { return c.post(e.base+"/", payload) })
}

// parallel runs b.N small GETs on g goroutines sharing one client capped at maxConns, after g
// concurrent warm-up requests, and reports the connections the server accepted and the errors.
func parallel(b *testing.B, e engine, g, maxConns int, runParallel bool) {
	url := getURL(e, small)
	c := e.new(maxConns)
	defer c.close()
	start, startSrv := e.conns.Load(), srvErrs.Load()
	var errs atomic.Int64
	var first sync.Once
	do := func() {
		if err := c.get(url, small); err != nil {
			errs.Add(1)
			first.Do(func() { b.Logf("first error: %v", err) })
		}
	}
	var wg sync.WaitGroup
	for range g {
		wg.Go(do)
	}
	wg.Wait()
	b.ReportAllocs()
	b.ResetTimer()
	if runParallel {
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				do()
			}
		})
	} else {
		var next atomic.Int64
		for range g {
			wg.Go(func() {
				for next.Add(1) <= int64(b.N) {
					do()
				}
			})
		}
		wg.Wait()
	}
	b.StopTimer()
	b.ReportMetric(float64(e.conns.Load()-start), "conns")
	b.ReportMetric(float64(errs.Load()), "errors")
	b.ReportMetric(float64(srvErrs.Load()-startSrv), "srvlog")
}

// BenchmarkReuse runs GOMAXPROCS goroutines over a pool capped at GOMAXPROCS connections.
func BenchmarkReuse(b *testing.B) {
	p := runtime.GOMAXPROCS(0)
	for _, e := range engines {
		b.Run(e.name, func(b *testing.B) { parallel(b, e, p, p, true) })
	}
}

// BenchmarkConcurrent runs GOMAXPROCS and 64 goroutines over a pool capped at 64 connections.
func BenchmarkConcurrent(b *testing.B) {
	for _, e := range engines {
		for _, g := range []int{runtime.GOMAXPROCS(0), 64} {
			b.Run(fmt.Sprintf("%s/g=%d", e.name, g), func(b *testing.B) { parallel(b, e, g, 64, false) })
		}
	}
}

// TestH2ColdStart counts the connections net/http's HTTP/2 client opens for 15 concurrent requests
// on an empty pool, and after one request has opened a connection first.
func TestH2ColdStart(t *testing.T) {
	e := engines[2]
	for _, warm := range []bool{false, true} {
		c := e.new(64)
		start := e.conns.Load()
		if warm {
			c.get(getURL(e, small), small)
		}
		var wg sync.WaitGroup
		for range 15 {
			wg.Go(func() { c.get(getURL(e, small), small) })
		}
		wg.Wait()
		t.Logf("warm=%v conns=%d", warm, e.conns.Load()-start)
		c.close()
	}
}
```

### Results

Medians of five samples (`-count=5`; benchstat gives no confidence interval below six). The suite
ran three times in a row; the tables give the run of the harness listed above. The other two runs
used the same workloads and differ only in reporting: one set no `MB/s`, and both let the TLS
server's log share the benchmark output, which made a few of their samples unparseable. **Spread**
is the largest per-run median of `ns/op` over the smallest, across the three runs: a difference
between two rows smaller than their spread is not a finding. `MB/s` is decimal, as `go test`
reports it.

Serial workloads. Each opened its one connection in the warm-up request and no other (`conns` 0 in
every sample).

| Workload | Row            |       ns/op |  MB/s |       B/op | allocs/op | Spread |
|----------|----------------|------------:|------:|-----------:|----------:|-------:|
| Small    | nethttp-h1     |      35,219 |       |      6,329 |        75 |   1.03 |
| Small    | nethttp-h1tls  |      36,881 |       |      6,332 |        75 |   1.04 |
| Small    | nethttp-h2tls  |      48,682 |       |      6,982 |        76 |   1.05 |
| Small    | fasthttp-h1    |      26,508 |       |      2,672 |        25 |   1.26 |
| Small    | fasthttp-h1tls |      27,061 |       |      2,672 |        25 |   1.38 |
| Small    | quicgo-h3      |      75,786 |       |     15,107 |       227 |   1.10 |
| Large    | nethttp-h1     |   2,079,280 | 8,069 |      6,386 |        75 |   1.13 |
| Large    | nethttp-h1tls  |   5,460,811 | 3,072 |      6,555 |        75 |   1.03 |
| Large    | nethttp-h2tls  |  11,329,130 | 1,481 |     87,694 |     4,255 |   1.42 |
| Large    | fasthttp-h1    |   1,931,005 | 8,688 | 16,793,291 |        38 |   1.27 |
| Large    | fasthttp-h1tls |   7,522,063 | 2,230 | 16,809,557 |        44 |   1.37 |
| Large    | quicgo-h3      | 130,608,286 |   128 |  5,550,424 |   171,433 |   1.35 |
| Upload   | nethttp-h1     |   2,689,207 | 6,239 |     40,411 |        89 |   1.12 |
| Upload   | nethttp-h1tls  |   6,725,073 | 2,495 |     40,133 |        88 |   1.11 |
| Upload   | nethttp-h2tls  |   8,479,065 | 1,979 |    132,681 |     8,338 |   1.17 |
| Upload   | fasthttp-h1    |   2,129,931 | 7,877 |      2,501 |        27 |   1.33 |
| Upload   | fasthttp-h1tls |   7,317,299 | 2,293 |      3,071 |        27 |   1.29 |
| Upload   | quicgo-h3      | 153,253,649 |   109 |  6,311,132 |   205,852 |   1.41 |

Parallel workloads, 1 KiB responses. `conns` gives the range over the five samples.

| Workload        | Row            |  ns/op |   B/op | allocs/op | conns | Spread |
|-----------------|----------------|-------:|-------:|----------:|------:|-------:|
| Reuse (15)      | nethttp-h1     | 11,296 |  6,304 |        72 |    15 |   1.08 |
| Reuse (15)      | nethttp-h1tls  | 11,526 |  6,307 |        72 |    15 |   1.18 |
| Reuse (15)      | nethttp-h2tls  | 16,646 |  6,915 |        72 |    15 |   2.81 |
| Reuse (15)      | fasthttp-h1    | 11,339 |  2,640 |        23 |    15 |   1.10 |
| Reuse (15)      | fasthttp-h1tls | 10,741 |  2,642 |        23 |    15 |   1.04 |
| Reuse (15)      | quicgo-h3      | 17,571 | 14,612 |       206 |     1 |   1.03 |
| Concurrent g=15 | nethttp-h1     | 11,304 |  6,302 |        72 |    15 |   1.04 |
| Concurrent g=15 | nethttp-h1tls  | 13,497 |  6,331 |        72 | 28–44 |   1.24 |
| Concurrent g=15 | nethttp-h2tls  | 19,905 |  6,922 |        73 |    15 |   1.20 |
| Concurrent g=15 | fasthttp-h1    | 11,196 |  2,639 |        23 |    15 |   1.12 |
| Concurrent g=15 | fasthttp-h1tls | 10,692 |  2,642 |        23 |    15 |   1.24 |
| Concurrent g=15 | quicgo-h3      | 17,287 | 14,612 |       206 |     1 |   1.19 |
| Concurrent g=64 | nethttp-h1     |  8,810 |  6,315 |        72 |    64 |   1.11 |
| Concurrent g=64 | nethttp-h1tls  |  8,651 |  6,327 |        72 |    64 |   1.08 |
| Concurrent g=64 | nethttp-h2tls  | 17,349 |  6,894 |        72 | 43–64 |   1.46 |
| Concurrent g=64 | fasthttp-h1    |  7,909 |  2,642 |        23 |    64 |   1.08 |
| Concurrent g=64 | fasthttp-h1tls |  7,800 |  2,645 |        23 |    64 |   1.14 |
| Concurrent g=64 | quicgo-h3      | 11,701 | 14,529 |       199 |     1 |   1.01 |

`errors` and `srvlog` are 0 in every sample of every row. Every Engine completed every workload;
`fasthttp` never returned `ErrNoFreeConns`, since no workload ran more goroutines than its pool
cap. Across the three runs the TLS server logged `http: TLS handshake error` lines (`connection
reset by peer`, `EOF`) during the `nethttp-h1tls` and `nethttp-h2tls` parallel benchmarks, for
connections the client opened and dropped; none fell inside a measured window.

`TestH2ColdStart`, three repetitions: 15 concurrent requests on an empty `nethttp-h2tls` pool open
15 TCP connections every time; after one request has completed first, the same 15 open none and
share its one connection.

### What the numbers say

- **`fasthttp`'s "up to 4 times faster" [F1]: the direction holds, the magnitude does not.**
  Serially, on one reused connection, `fasthttp` takes 26.5 µs per small request against
  `net/http`'s 35.2 µs, 1.33 times faster (1.36 over TLS); the README's own TCP figures give 2.1
  times. Under parallel load the gap closes: 1.00 times at `Reuse`, 1.01 to 1.26 times at 15
  goroutines and 1.11 times at 64, mostly within the spread. On a 16 MiB body over plain TCP
  `fasthttp` is 1.08 times faster downloading and 1.26 times uploading. Over TLS neither client is
  consistently ahead: on downloads `net/http` leads by 1.38 times in the listed run and the two are
  within 4% in the others, and on uploads the lead changes sides between runs, within 1.10 times.
  `fasthttp` allocates 50 fewer objects and about 3.6 KiB less per small request against the same
  server, which is consistent with the README's 0 allocs/op client but does not isolate it, since
  the server's allocations are in every row. It also buffers the whole response: 16.8 MB per
  16 MiB download, against 6.4 KB for `net/http` streaming the same body.
- **Zhang et al.'s data rate up to 45.2% lower for QUIC and HTTP/3 than for TCP, TLS and HTTP/2
  [Q9]: the direction holds, and the gap here is about twice as large.** `quic-go` downloads 16 MiB
  at 128 MB/s against 1,481 MB/s for `net/http` over HTTP/2, 91% lower, and against 3,072 MB/s over
  HTTP/1.1 with TLS, 96% lower; uploads are about 95% lower than over HTTP/2. It allocates 171,433
  objects per 16 MiB download against 4,255 for HTTP/2. Both ends are `quic-go` in one process on
  darwin, and `quic-go`'s UDP send and receive path is platform-specific, so this run says nothing
  about Linux and does not isolate where the time goes. On small requests `quic-go` is 1.56 times
  slower than HTTP/2 serially, but at 64 goroutines it is faster than `net/http`'s HTTP/2 (11.7 µs
  against 17.3 µs), on one connection against HTTP/2's 43 to 64.
- **HTTP/2 pooling [N6, N7]: partly testable here.** On a warm pool `net/http`'s HTTP/2 client puts
  15 concurrent requests on one connection, as [N6] describes, since none reaches the server's
  stream limit. On a cold pool it does the opposite: every concurrent first request dials its own
  connection, 15 for 15 goroutines and 43 to 64 for 64. Whether one connection limits throughput
  on a real link, [N6]'s complaint, and how requests fare under packet loss, [N7]'s, cannot be
  measured on loopback. HTTP/2 also costs more than HTTP/1.1 over the same TLS server in every
  serial workload: 1.32 times the time per small request, half the download rate with 57 times the
  allocations, and its `Reuse` row is the least stable in the suite, with medians from 13.3 µs to
  37.4 µs across the three runs.
- **`net/http` over HTTP/1.1 with TLS** opened 28 to 44 connections for 15 goroutines in
  `Concurrent` (pool cap 64), where plain TCP opened exactly 15. The harness does not show which
  requests dialled the extra connections. `fasthttp` opened exactly one connection per goroutine in
  every parallel workload.
