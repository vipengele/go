package update

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

const testToken = "s3cret-token"

// authLog records the Authorization header of every request a handler it wraps receives.
type authLog struct {
	mu      sync.Mutex
	headers []string
}

// wrap returns h recording each request's Authorization header first.
func (l *authLog) wrap(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		l.mu.Lock()
		l.headers = append(l.headers, r.Header.Get("Authorization"))
		l.mu.Unlock()
		h(w, r)
	}
}

// seen returns the recorded headers.
func (l *authLog) seen() []string {
	l.mu.Lock()
	defer l.mu.Unlock()

	return append([]string(nil), l.headers...)
}

// assertNoAuthorization fails t when any request l recorded carried an Authorization header.
func (l *authLog) assertNoAuthorization(t *testing.T, name string) {
	t.Helper()

	for _, h := range l.seen() {
		if h != "" {
			t.Errorf("%s received Authorization %q, want none", name, h)
		}
	}
}

// splitGitHub serves the latest-release web page and the REST API from two servers, so that a
// header sent to one host and not the other is observable.
type splitGitHub struct {
	web, api  *httptest.Server
	webAuth   authLog
	apiAuth   authLog
	webCalls  atomic.Int32
	apiCalls  atomic.Int32
	apiRoutes *http.ServeMux
}

// newSplitGitHub starts the web server answering webPath with web and the API server answering
// apiPath and listPath with api. Every request either receives is recorded.
func newSplitGitHub(t *testing.T, web, api http.HandlerFunc) *splitGitHub {
	t.Helper()

	g := &splitGitHub{}
	webMux := http.NewServeMux()
	webMux.HandleFunc(webPath, g.webAuth.wrap(func(w http.ResponseWriter, r *http.Request) {
		g.webCalls.Add(1)
		web(w, r)
	}))
	g.web = httptest.NewServer(webMux)
	t.Cleanup(g.web.Close)

	g.apiRoutes = http.NewServeMux()
	apiHandler := g.apiAuth.wrap(func(w http.ResponseWriter, r *http.Request) {
		g.apiCalls.Add(1)
		api(w, r)
	})
	g.apiRoutes.HandleFunc(apiPath, apiHandler)
	g.apiRoutes.HandleFunc(listPath, apiHandler)
	g.api = httptest.NewServer(g.apiRoutes)
	t.Cleanup(g.api.Close)

	return g
}

// source returns a GitHubSource reading g's web page and API with token as its Token.
func (g *splitGitHub) source(token func(context.Context) (string, error)) *GitHubSource {
	return &GitHubSource{
		Owner:      testOwner,
		Repo:       testRepo,
		HTTPClient: &http.Client{},
		WebURL:     g.web.URL,
		APIURL:     g.api.URL,
		Token:      token,
	}
}

// staticToken returns a Token provider that returns token.
func staticToken(token string) func(context.Context) (string, error) {
	return func(context.Context) (string, error) { return token, nil }
}

// listReleaseJSON answers the release list with one release, v1.4.0.
func listReleaseJSON() http.HandlerFunc {
	return respond(http.StatusOK, apiListJSON(listedRelease{tag: "v1.4.0"}))
}

func TestGitHubSource_TokenReachesTheAPIHostOnly(t *testing.T) {
	t.Parallel()

	for _, ch := range []Channel{Stable, Edge} {
		t.Run(ch.String(), func(t *testing.T) {
			t.Parallel()

			api := respond(http.StatusOK, apiReleaseJSON("v1.4.0"))
			if ch != Stable {
				api = listReleaseJSON()
			}
			g := newSplitGitHub(t, respond(http.StatusOK, ""), api)
			s := g.source(staticToken(testToken))

			got, err := s.Latest(context.Background(), ch)
			if err != nil {
				t.Fatalf("Latest() error = %v", err)
			}
			if got.Tag != "v1.4.0" {
				t.Errorf("Latest().Tag = %q, want %q", got.Tag, "v1.4.0")
			}
			if seen := g.apiAuth.seen(); len(seen) != 1 || seen[0] != "Bearer "+testToken {
				t.Errorf("API Authorization = %q, want one %q", seen, "Bearer "+testToken)
			}
			g.webAuth.assertNoAuthorization(t, "the web host")
		})
	}
}

func TestGitHubSource_RedirectHitNeedsNoToken(t *testing.T) {
	t.Parallel()

	g := newSplitGitHub(t, redirectTo(http.StatusFound, tagPagePath+"v1.2.3"), unexpected(t, "API"))
	var calls atomic.Int32
	s := g.source(func(context.Context) (string, error) {
		calls.Add(1)
		return "", errors.New("the token provider is not to be called")
	})

	got, err := s.Latest(context.Background(), Stable)
	if err != nil {
		t.Fatalf("Latest() error = %v", err)
	}
	if got.Tag != "v1.2.3" {
		t.Errorf("Latest().Tag = %q, want %q", got.Tag, "v1.2.3")
	}
	if n := calls.Load(); n != 0 {
		t.Errorf("Token calls = %d, want 0", n)
	}
	g.webAuth.assertNoAuthorization(t, "the web host")
}

func TestGitHubSource_TokenIsReadOncePerLatest(t *testing.T) {
	t.Parallel()

	g := newSplitGitHub(t, respond(http.StatusOK, ""), respond(http.StatusOK, apiReleaseJSON("v1.4.0")))
	var calls atomic.Int32
	s := g.source(func(context.Context) (string, error) {
		calls.Add(1)
		return testToken, nil
	})

	for range 2 {
		if _, err := s.Latest(context.Background(), Stable); err != nil {
			t.Fatalf("Latest() error = %v", err)
		}
	}
	if n := calls.Load(); n != 2 {
		t.Errorf("Token calls over two Latest calls = %d, want 2", n)
	}
}

func TestGitHubSource_TokenIsReadOnceAcrossListPages(t *testing.T) {
	t.Parallel()

	drafts := make([]listedRelease, listPageSize)
	for i := range drafts {
		drafts[i] = listedRelease{tag: fmt.Sprintf("v1.0.%d", i), draft: true}
	}
	g := newSplitGitHub(t, respond(http.StatusOK, ""), func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("page") == "1" {
			respond(http.StatusOK, apiListJSON(drafts...))(w, r)
			return
		}
		respond(http.StatusOK, apiListJSON(listedRelease{tag: "v0.9.0-beta.1"}))(w, r)
	})
	var calls atomic.Int32
	s := g.source(func(context.Context) (string, error) {
		calls.Add(1)
		return testToken, nil
	})

	got, err := s.Latest(context.Background(), Beta)
	if err != nil {
		t.Fatalf("Latest() error = %v", err)
	}
	if got.Tag != "v0.9.0-beta.1" {
		t.Errorf("Latest().Tag = %q, want %q", got.Tag, "v0.9.0-beta.1")
	}
	if n := calls.Load(); n != 1 {
		t.Errorf("Token calls over two pages = %d, want 1", n)
	}
	if seen := g.apiAuth.seen(); len(seen) != 2 || seen[0] != "Bearer "+testToken || seen[1] != "Bearer "+testToken {
		t.Errorf("API Authorization = %q, want two %q", seen, "Bearer "+testToken)
	}
}

func TestGitHubSource_EmptyTokenSendsNoHeader(t *testing.T) {
	t.Parallel()

	for name, token := range map[string]func(context.Context) (string, error){
		"empty token": staticToken(""),
		"nil Token":   nil,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			g := newSplitGitHub(t, respond(http.StatusOK, ""), respond(http.StatusOK, apiReleaseJSON("v1.4.0")))

			if _, err := g.source(token).Latest(context.Background(), Stable); err != nil {
				t.Fatalf("Latest() error = %v", err)
			}
			if n := g.apiCalls.Load(); n != 1 {
				t.Errorf("API requests = %d, want 1", n)
			}
			g.apiAuth.assertNoAuthorization(t, "the API host")
			g.webAuth.assertNoAuthorization(t, "the web host")
		})
	}
}

func TestGitHubSource_TokenProviderErrorFailsTheAPIRequest(t *testing.T) {
	t.Parallel()

	errProvider := errors.New("keychain locked")

	for _, ch := range []Channel{Stable, Edge} {
		g := newSplitGitHub(t, respond(http.StatusOK, ""), unexpected(t, "API"))
		s := g.source(func(context.Context) (string, error) { return "", errProvider })

		_, err := s.Latest(context.Background(), ch)
		if !errors.Is(err, errProvider) {
			t.Fatalf("channel %s: err = %v, want it to wrap the provider's error", ch, err)
		}
		if !strings.HasPrefix(err.Error(), "update: ") {
			t.Errorf("Error() = %q, want the \"update: \" prefix", err.Error())
		}
		if n := g.apiCalls.Load(); n != 0 {
			t.Errorf("channel %s: API requests = %d, want 0", ch, n)
		}
	}
}

func TestGitHubSource_TokenProviderReceivesTheContext(t *testing.T) {
	t.Parallel()

	type key struct{}
	g := newSplitGitHub(t, respond(http.StatusOK, ""), respond(http.StatusOK, apiReleaseJSON("v1.4.0")))
	var got any
	s := g.source(func(ctx context.Context) (string, error) {
		got = ctx.Value(key{})
		return "", nil
	})

	if _, err := s.Latest(context.WithValue(context.Background(), key{}, "marker"), Stable); err != nil {
		t.Fatalf("Latest() error = %v", err)
	}
	if got != "marker" {
		t.Errorf("Token ctx value = %v, want the caller's context", got)
	}
}

func TestGitHubSource_CrossHostAPIRedirectIsRefused(t *testing.T) {
	t.Parallel()

	var other authLog
	var otherCalls atomic.Int32
	elsewhere := httptest.NewServer(other.wrap(func(w http.ResponseWriter, _ *http.Request) {
		otherCalls.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(elsewhere.Close)

	target := elsewhere.URL + apiPath
	g := newSplitGitHub(t, respond(http.StatusOK, ""), redirectTo(http.StatusFound, target))

	for name, token := range map[string]string{"with a token": testToken, "without a token": ""} {
		_, err := g.source(staticToken(token)).Latest(context.Background(), Stable)

		var ch *CrossHostRedirectError
		if !errors.As(err, &ch) {
			t.Fatalf("%s: err = %v, want a *CrossHostRedirectError", name, err)
		}
		if ch.URL != g.api.URL+apiPath || ch.Location != target {
			t.Errorf("%s: CrossHostRedirectError = %+v, want URL %q and Location %q",
				name, *ch, g.api.URL+apiPath, target)
		}
		if !strings.Contains(err.Error(), "update: GET "+g.api.URL+apiPath) || !strings.Contains(err.Error(), target) {
			t.Errorf("%s: Error() = %q, want the request URL and the location", name, err.Error())
		}
	}
	if n := otherCalls.Load(); n != 0 {
		t.Errorf("requests to the other host = %d, want 0", n)
	}
}

func TestGitHubSource_SameHostAPIRedirectKeepsTheToken(t *testing.T) {
	t.Parallel()

	const movedPath = "/repositories/42/releases/latest"
	g := newSplitGitHub(t, respond(http.StatusOK, ""), redirectTo(http.StatusMovedPermanently, movedPath))
	g.apiRoutes.HandleFunc(movedPath, g.apiAuth.wrap(respond(http.StatusOK, apiReleaseJSON("v1.4.0"))))

	got, err := g.source(staticToken(testToken)).Latest(context.Background(), Stable)
	if err != nil {
		t.Fatalf("Latest() error = %v", err)
	}
	if got.Tag != "v1.4.0" {
		t.Errorf("Latest().Tag = %q, want %q", got.Tag, "v1.4.0")
	}
	want := []string{"Bearer " + testToken, "Bearer " + testToken}
	if seen := g.apiAuth.seen(); !reflect.DeepEqual(seen, want) {
		t.Errorf("API Authorization = %q, want %q", seen, want)
	}
}

func TestGitHubSource_SameHostAPIRedirectConsultsTheCallersCheckRedirect(t *testing.T) {
	t.Parallel()

	const movedPath = "/repositories/42/releases/latest"
	errStop := errors.New("caller stops here")
	g := newSplitGitHub(t, respond(http.StatusOK, ""), redirectTo(http.StatusMovedPermanently, movedPath))
	g.apiRoutes.HandleFunc(movedPath, unexpected(t, "moved API"))

	var calls atomic.Int32
	s := g.source(staticToken(testToken))
	s.HTTPClient.CheckRedirect = func(*http.Request, []*http.Request) error {
		calls.Add(1)
		return errStop
	}

	_, err := s.Latest(context.Background(), Stable)
	if !errors.Is(err, errStop) {
		t.Fatalf("err = %v, want the caller's CheckRedirect error", err)
	}
	// The web probe replaces CheckRedirect on its own copy, so only the API redirect reaches it.
	if n := calls.Load(); n != 1 {
		t.Errorf("caller CheckRedirect calls = %d, want 1", n)
	}
}

func TestGitHubSource_SameHostAPIRedirectsStopAfterTen(t *testing.T) {
	t.Parallel()

	g := newSplitGitHub(t, respond(http.StatusOK, ""), redirectTo(http.StatusFound, apiPath))

	_, err := g.source(nil).Latest(context.Background(), Stable)
	if !errors.Is(err, errTooManyAPIRedirects) {
		t.Fatalf("err = %v, want it to stop after 10 redirects", err)
	}
	if n := g.apiCalls.Load(); n != 10 {
		t.Errorf("API requests = %d, want 10", n)
	}
}

// recordingTransport forwards to http.DefaultTransport, recording every request it receives.
type recordingTransport struct {
	mu   sync.Mutex
	reqs []*http.Request
}

func (rt *recordingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	rt.mu.Lock()
	rt.reqs = append(rt.reqs, req)
	rt.mu.Unlock()

	return http.DefaultTransport.RoundTrip(req)
}

func TestGitHubSource_DoesNotModifyTheCallersClient(t *testing.T) {
	t.Parallel()

	g := newSplitGitHub(t, respond(http.StatusOK, ""), respond(http.StatusOK, apiReleaseJSON("v1.4.0")))
	transport := &recordingTransport{}
	checkRedirect := func(*http.Request, []*http.Request) error { return nil }
	client := &http.Client{Transport: transport, CheckRedirect: checkRedirect}
	s := g.source(staticToken(testToken))
	s.HTTPClient = client

	if _, err := s.Latest(context.Background(), Stable); err != nil {
		t.Fatalf("Latest() error = %v", err)
	}
	if client.Transport != transport {
		t.Errorf("caller's Transport = %v, want it unchanged", client.Transport)
	}
	if reflect.ValueOf(client.CheckRedirect).Pointer() != reflect.ValueOf(checkRedirect).Pointer() {
		t.Error("caller's CheckRedirect was replaced")
	}
	if len(transport.reqs) != 2 {
		t.Fatalf("requests through the caller's transport = %d, want 2", len(transport.reqs))
	}
	if h := transport.reqs[0].Header.Get("Authorization"); h != "" {
		t.Errorf("web request Authorization = %q, want none", h)
	}
	if h := transport.reqs[1].Header.Get("Authorization"); h != "Bearer "+testToken {
		t.Errorf("API request Authorization = %q, want %q", h, "Bearer "+testToken)
	}
}

func TestTokenTransport(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name, url, token, want string
	}{
		{name: "API host", url: "https://api.example.test/repos", token: testToken, want: "Bearer " + testToken},
		{name: "API host in another case", url: "https://API.Example.TEST/repos", token: testToken, want: "Bearer " + testToken},
		{name: "another host", url: "https://cdn.example.test/asset", token: testToken},
		{name: "API host on another port", url: "https://api.example.test:8443/repos", token: testToken},
		{name: "empty token", url: "https://api.example.test/repos"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			base := &capturingTransport{}
			tt := &tokenTransport{base: base, host: "api.example.test", token: tc.token}
			req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, tc.url, nil)
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("Accept", gitHubAccept)

			resp, err := tt.RoundTrip(req)
			if err != nil {
				t.Fatalf("RoundTrip() error = %v", err)
			}
			_ = resp.Body.Close()

			if got := base.req.Header.Get("Authorization"); got != tc.want {
				t.Errorf("forwarded Authorization = %q, want %q", got, tc.want)
			}
			if got := base.req.Header.Get("Accept"); got != gitHubAccept {
				t.Errorf("forwarded Accept = %q, want the request's %q", got, gitHubAccept)
			}
			if h := req.Header.Get("Authorization"); h != "" {
				t.Errorf("caller's request Authorization = %q, want the request left unmodified", h)
			}
			if tc.want != "" && base.req == req {
				t.Error("the token was added to the caller's request, want a clone")
			}
		})
	}
}

// capturingTransport answers 200 with an empty body, keeping the last request it received.
type capturingTransport struct{ req *http.Request }

func (c *capturingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	c.req = req

	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{},
		Body:       http.NoBody,
		Request:    req,
	}, nil
}

func TestTokenFromEnv(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		env  map[string]string
		want string
	}{
		{name: "GH_TOKEN wins", env: map[string]string{"GH_TOKEN": "gh", "GITHUB_TOKEN": "github"}, want: "gh"},
		{name: "GITHUB_TOKEN alone", env: map[string]string{"GITHUB_TOKEN": "github"}, want: "github"},
		{name: "empty GH_TOKEN", env: map[string]string{"GH_TOKEN": "", "GITHUB_TOKEN": "github"}, want: "github"},
		{name: "neither", env: nil, want: ""},
	} {
		got, err := tokenFromEnv(envOf(tc.env))(context.Background())
		if err != nil || got != tc.want {
			t.Errorf("%s: token = %q, %v, want %q, nil", tc.name, got, err, tc.want)
		}
	}
}

func TestCrossHostRedirectError(t *testing.T) {
	t.Parallel()

	err := &CrossHostRedirectError{URL: "https://api.test/repos/o/r/releases/latest", Location: "https://elsewhere.test/x"}
	want := "update: GET https://api.test/repos/o/r/releases/latest: refusing redirect to another host: https://elsewhere.test/x"
	if got := err.Error(); got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

func TestDownloadNeverSendsTheToken(t *testing.T) {
	t.Parallel()

	var cdnAuth, assetAuth authLog
	cdnMux := http.NewServeMux()
	cdnMux.HandleFunc(dlArchivePath, cdnAuth.wrap(serveBody(dlContent)))
	cdnMux.HandleFunc(dlChecksumsPath, cdnAuth.wrap(serveBody(dlChecksums())))
	cdn := httptest.NewServer(cdnMux)
	t.Cleanup(cdn.Close)

	assetMux := http.NewServeMux()
	assetMux.HandleFunc("/redirect/archive", assetAuth.wrap(redirectHandler(cdn.URL+dlArchivePath)))
	assetMux.HandleFunc("/redirect/checksums", assetAuth.wrap(redirectHandler(cdn.URL+dlChecksumsPath)))
	assets := httptest.NewServer(assetMux)
	t.Cleanup(assets.Close)

	for name, configure := range map[string]func(*Config){
		"Config.Token": func(c *Config) { c.Token = staticToken(testToken) },
		"GH_TOKEN":     func(c *Config) { c.Getenv = envOf(map[string]string{"GH_TOKEN": testToken}) },
	} {
		c := validConfig(t.TempDir())
		c.TrustedKeys = nil
		c.AllowUnsigned = true
		c.TempDir = t.TempDir()
		configure(&c)
		sel := Selection{
			Tag:       dlTag,
			Archive:   Asset{Name: dlArchiveName, URL: assets.URL + "/redirect/archive"},
			Checksums: Asset{Name: dlChecksumsName, URL: assets.URL + "/redirect/checksums"},
		}

		a, err := c.Download(context.Background(), sel)
		if err != nil {
			t.Fatalf("%s: Download() error = %v", name, err)
		}
		if err := a.Close(); err != nil {
			t.Fatalf("%s: Close: %v", name, err)
		}
	}

	if n := len(assetAuth.seen()); n != 4 {
		t.Errorf("requests to the asset host = %d, want 4", n)
	}
	if n := len(cdnAuth.seen()); n != 4 {
		t.Errorf("requests to the CDN host = %d, want 4", n)
	}
	assetAuth.assertNoAuthorization(t, "the asset host")
	cdnAuth.assertNoAuthorization(t, "the CDN host")
}

func TestHostOf(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct{ name, in, want string }{
		{"host without port", "https://api.example.test/repos", "api.example.test"},
		{"host with port", "http://127.0.0.1:8080/x", "127.0.0.1:8080"},
		{"unparsable URL", "http://[::1", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := hostOf(tc.in); got != tc.want {
				t.Errorf("hostOf(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestSameHost_EmptyAPIHostMatchesNothing(t *testing.T) {
	t.Parallel()

	for _, host := range []string{"", "api.example.test"} {
		if sameHost(host, "") {
			t.Errorf("sameHost(%q, \"\") = true, want false", host)
		}
	}
}
