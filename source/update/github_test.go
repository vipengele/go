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
	"time"
)

const (
	testOwner   = "octo"
	testRepo    = "tool"
	webPath     = "/octo/tool/releases/latest"
	apiPath     = "/repos/octo/tool/releases/latest"
	tagPagePath = "/octo/tool/releases/tag/"
	listPath    = "/repos/octo/tool/releases"
	listQuery   = "per_page=100"
)

// apiReleaseJSON is a REST API latest-release document for tag with two assets.
func apiReleaseJSON(tag string) string {
	return fmt.Sprintf(`{
		"tag_name": %q,
		"html_url": "https://example.test/octo/tool/releases/tag/%s",
		"draft": false,
		"prerelease": false,
		"assets": [
			{"name": "tool_linux_amd64.tar.gz", "browser_download_url": "https://example.test/a.tar.gz", "size": 1234},
			{"name": "checksums.txt", "browser_download_url": "https://example.test/checksums.txt", "size": 56}
		]
	}`, tag, tag)
}

// apiAssets is the Assets of the release apiReleaseJSON describes.
func apiAssets() []Asset {
	return []Asset{
		{Name: "tool_linux_amd64.tar.gz", URL: "https://example.test/a.tar.gz", Size: 1234},
		{Name: "checksums.txt", URL: "https://example.test/checksums.txt", Size: 56},
	}
}

// fakeGitHub is an httptest server answering the latest-release web page with web and the REST
// API latest release with api, counting the requests each receives.
type fakeGitHub struct {
	server    *httptest.Server
	webCalls  atomic.Int32
	apiCalls  atomic.Int32
	listCalls atomic.Int32
}

func newFakeGitHub(t *testing.T, web, api http.HandlerFunc) *fakeGitHub {
	t.Helper()

	f := &fakeGitHub{}
	mux := http.NewServeMux()
	mux.HandleFunc(webPath, func(w http.ResponseWriter, r *http.Request) {
		f.webCalls.Add(1)
		web(w, r)
	})
	mux.HandleFunc(apiPath, func(w http.ResponseWriter, r *http.Request) {
		f.apiCalls.Add(1)
		api(w, r)
	})
	f.server = httptest.NewServer(mux)
	t.Cleanup(f.server.Close)

	return f
}

// source returns a GitHubSource whose web and API addresses are the fake server.
func (f *fakeGitHub) source() *GitHubSource {
	return &GitHubSource{
		Owner:      testOwner,
		Repo:       testRepo,
		HTTPClient: f.server.Client(),
		WebURL:     f.server.URL,
		APIURL:     f.server.URL,
	}
}

func redirectTo(status int, location string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		if location != "" {
			w.Header().Set("Location", location)
		}
		w.WriteHeader(status)
	}
}

func respond(status int, body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}
}

func unexpected(t *testing.T, name string) http.HandlerFunc {
	t.Helper()

	return func(w http.ResponseWriter, _ *http.Request) {
		t.Errorf("unexpected request to the %s endpoint", name)
		w.WriteHeader(http.StatusTeapot)
	}
}

func TestGitHubSource_RedirectHit(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		status   int
		location string
		wantTag  string
		wantURL  string
	}{
		{name: "301", status: http.StatusMovedPermanently, location: tagPagePath + "v1.2.3", wantTag: "v1.2.3"},
		{name: "302", status: http.StatusFound, location: tagPagePath + "v1.2.3", wantTag: "v1.2.3"},
		{name: "303", status: http.StatusSeeOther, location: tagPagePath + "v1.2.3", wantTag: "v1.2.3"},
		{name: "307", status: http.StatusTemporaryRedirect, location: tagPagePath + "v1.2.3", wantTag: "v1.2.3"},
		{name: "308", status: http.StatusPermanentRedirect, location: tagPagePath + "v1.2.3", wantTag: "v1.2.3"},
		{
			name:     "absolute location on another host",
			status:   http.StatusFound,
			location: "https://elsewhere.test/octo/tool/releases/tag/v0.10.0",
			wantTag:  "v0.10.0",
			wantURL:  "https://elsewhere.test/octo/tool/releases/tag/v0.10.0",
		},
		{
			name:     "owner and repo in another case",
			status:   http.StatusFound,
			location: "/Octo/TOOL/releases/tag/v2.0.0",
			wantTag:  "v2.0.0",
			wantURL:  "/Octo/TOOL/releases/tag/v2.0.0",
		},
		{name: "escaped tag", status: http.StatusFound, location: tagPagePath + "%76" + "3.0.1", wantTag: "v3.0.1"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := newFakeGitHub(t, redirectTo(tc.status, tc.location), unexpected(t, "API"))

			got, err := f.source().Latest(context.Background())
			if err != nil {
				t.Fatalf("Latest() error = %v", err)
			}

			wantURL := tc.wantURL
			if wantURL == "" {
				wantURL = f.server.URL + tc.location
			} else if strings.HasPrefix(wantURL, "/") {
				wantURL = f.server.URL + wantURL
			}

			want := Release{Tag: tc.wantTag, URL: wantURL}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("Latest() = %#v, want %#v", got, want)
			}
			if got.Assets != nil {
				t.Errorf("Latest().Assets = %#v, want nil", got.Assets)
			}
			if n := f.apiCalls.Load(); n != 0 {
				t.Errorf("API requests = %d, want 0", n)
			}
		})
	}
}

func TestGitHubSource_RedirectMissFallsBackToAPI(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		web  http.HandlerFunc
	}{
		{name: "200", web: respond(http.StatusOK, "<html></html>")},
		{name: "300", web: redirectTo(http.StatusMultipleChoices, tagPagePath+"v9.9.9")},
		{name: "304", web: redirectTo(http.StatusNotModified, tagPagePath+"v9.9.9")},
		{name: "305", web: redirectTo(http.StatusUseProxy, tagPagePath+"v9.9.9")},
		{name: "404", web: respond(http.StatusNotFound, "not found")},
		{name: "500", web: respond(http.StatusInternalServerError, "boom")},
		{name: "no Location", web: redirectTo(http.StatusFound, "")},
		{name: "unparseable Location", web: redirectTo(http.StatusFound, "http://[::1")},
		{name: "releases index", web: redirectTo(http.StatusFound, "/octo/tool/releases")},
		{name: "empty tag", web: redirectTo(http.StatusFound, tagPagePath)},
		{name: "another repository", web: redirectTo(http.StatusFound, "/octo/other/releases/tag/v9.9.9")},
		{name: "another owner", web: redirectTo(http.StatusFound, "/someone/tool/releases/tag/v9.9.9")},
		{name: "not a tag page", web: redirectTo(http.StatusFound, "/octo/tool/releases/download/v9.9.9")},
		{name: "bad escape in tag", web: redirectTo(http.StatusFound, tagPagePath+"v9%zz")},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := newFakeGitHub(t, tc.web, respond(http.StatusOK, apiReleaseJSON("v1.4.0")))

			got, err := f.source().Latest(context.Background())
			if err != nil {
				t.Fatalf("Latest() error = %v", err)
			}

			want := Release{
				Tag:    "v1.4.0",
				URL:    "https://example.test/octo/tool/releases/tag/v1.4.0",
				Assets: apiAssets(),
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("Latest() = %#v, want %#v", got, want)
			}
			if n := f.webCalls.Load(); n != 1 {
				t.Errorf("web requests = %d, want 1", n)
			}
			if n := f.apiCalls.Load(); n != 1 {
				t.Errorf("API requests = %d, want 1", n)
			}
		})
	}
}

func TestGitHubSource_APIAssets(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body string
		want []Asset
	}{
		{name: "no assets field", body: `{"tag_name": "v1.0.0", "html_url": "u"}`, want: []Asset{}},
		{name: "empty assets", body: `{"tag_name": "v1.0.0", "html_url": "u", "assets": []}`, want: []Asset{}},
		{
			name: "one asset",
			body: `{"tag_name": "v1.0.0", "html_url": "u", "assets": [{"name": "n", "browser_download_url": "d", "size": 7}]}`,
			want: []Asset{{Name: "n", URL: "d", Size: 7}},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := newFakeGitHub(t, respond(http.StatusOK, ""), respond(http.StatusOK, tc.body))

			got, err := f.source().Latest(context.Background())
			if err != nil {
				t.Fatalf("Latest() error = %v", err)
			}

			want := Release{Tag: "v1.0.0", URL: "u", Assets: tc.want}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("Latest() = %#v, want %#v", got, want)
			}
			if got.Assets == nil {
				t.Error("Latest().Assets = nil, want a non-nil slice")
			}
		})
	}
}

func TestGitHubSource_APISuccessStatus(t *testing.T) {
	t.Parallel()

	for _, status := range []int{http.StatusOK, http.StatusCreated, 299} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			t.Parallel()

			f := newFakeGitHub(t, respond(http.StatusOK, ""), respond(status, apiReleaseJSON("v1.0.0")))

			got, err := f.source().Latest(context.Background())
			if err != nil {
				t.Fatalf("Latest() error = %v", err)
			}
			if got.Tag != "v1.0.0" {
				t.Errorf("Latest().Tag = %q, want %q", got.Tag, "v1.0.0")
			}
		})
	}
}

func TestGitHubSource_TagErrors(t *testing.T) {
	t.Parallel()

	tags := []struct {
		name    string
		tag     string
		escaped string
	}{
		{name: "no leading v", tag: "1.2.3"},
		{name: "shorthand", tag: "v1.2"},
		{name: "build metadata", tag: "v1.2.3+dirty"},
		{name: "word", tag: "latest"},
		{name: "pseudo-version", tag: "v0.0.0-20240101000000-abcdefabcdef"},
		{name: "prefixed", tag: "source/update/v1.2.3"},
		{name: "prefixed and escaped", tag: "source/update/v1.2.3", escaped: "source%2Fupdate%2Fv1.2.3"},
		{name: "trailing slash", tag: "v1.2.3/"},
	}

	for _, tc := range tags {
		t.Run("redirect "+tc.name, func(t *testing.T) {
			t.Parallel()

			escaped := tc.escaped
			if escaped == "" {
				escaped = tc.tag
			}
			f := newFakeGitHub(t, redirectTo(http.StatusFound, tagPagePath+escaped), unexpected(t, "API"))

			_, err := f.source().Latest(context.Background())

			var tagErr *TagError
			if !errors.As(err, &tagErr) {
				t.Fatalf("Latest() error = %v, want a *TagError", err)
			}
			if tagErr.Tag != tc.tag {
				t.Errorf("TagError.Tag = %q, want %q", tagErr.Tag, tc.tag)
			}
		})

		t.Run("API "+tc.name, func(t *testing.T) {
			t.Parallel()

			f := newFakeGitHub(t, respond(http.StatusOK, ""), respond(http.StatusOK, apiReleaseJSON(tc.tag)))

			_, err := f.source().Latest(context.Background())

			var tagErr *TagError
			if !errors.As(err, &tagErr) {
				t.Fatalf("Latest() error = %v, want a *TagError", err)
			}
			if tagErr.Tag != tc.tag {
				t.Errorf("TagError.Tag = %q, want %q", tagErr.Tag, tc.tag)
			}
		})
	}
}

func TestGitHubSource_Prerelease(t *testing.T) {
	t.Parallel()

	for _, tag := range []string{"v1.2.3-rc.1", "v2.0.0-beta", "v0.1.0-0"} {
		t.Run("redirect "+tag, func(t *testing.T) {
			t.Parallel()

			f := newFakeGitHub(t, redirectTo(http.StatusFound, tagPagePath+tag), unexpected(t, "API"))

			got, err := f.source().Latest(context.Background())
			if !errors.Is(err, ErrPrerelease) {
				t.Fatalf("Latest() error = %v, want ErrPrerelease", err)
			}
			if !strings.Contains(err.Error(), tag) {
				t.Errorf("Latest() error = %q, want it to name %q", err, tag)
			}
			if !reflect.DeepEqual(got, Release{}) {
				t.Errorf("Latest() = %#v, want the zero Release", got)
			}
		})

		t.Run("API "+tag, func(t *testing.T) {
			t.Parallel()

			f := newFakeGitHub(t, respond(http.StatusOK, ""), respond(http.StatusOK, apiReleaseJSON(tag)))

			got, err := f.source().Latest(context.Background())
			if !errors.Is(err, ErrPrerelease) {
				t.Fatalf("Latest() error = %v, want ErrPrerelease", err)
			}
			if !reflect.DeepEqual(got, Release{}) {
				t.Errorf("Latest() = %#v, want the zero Release", got)
			}
		})
	}
}

func TestGitHubSource_APIStatusError(t *testing.T) {
	t.Parallel()

	long := strings.Repeat("x", 3000)

	tests := []struct {
		name     string
		status   int
		body     string
		wantBody string
	}{
		{name: "404", status: http.StatusNotFound, body: `{"message":"Not Found"}`, wantBody: `{"message":"Not Found"}`},
		{name: "403 empty body", status: http.StatusForbidden, body: "", wantBody: ""},
		{name: "500 long body", status: http.StatusInternalServerError, body: long, wantBody: long[:1024]},
		{name: "body of exactly the limit", status: http.StatusBadGateway, body: long[:1024], wantBody: long[:1024]},
		{
			name:     "cut on a character boundary",
			status:   http.StatusServiceUnavailable,
			body:     strings.Repeat("a", 1023) + "é",
			wantBody: strings.Repeat("a", 1023),
		},
		{name: "300", status: http.StatusMultipleChoices, body: "choices", wantBody: "choices"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := newFakeGitHub(t, respond(http.StatusNotFound, ""), respond(tc.status, tc.body))

			_, err := f.source().Latest(context.Background())

			var statusErr *StatusError
			if !errors.As(err, &statusErr) {
				t.Fatalf("Latest() error = %v, want a *StatusError", err)
			}
			want := StatusError{URL: f.server.URL + apiPath, StatusCode: tc.status, Body: tc.wantBody}
			if *statusErr != want {
				t.Errorf("StatusError = %#v, want %#v", *statusErr, want)
			}
		})
	}
}

func TestGitHubSource_APIBodyLimit(t *testing.T) {
	t.Parallel()

	doc := `{"tag_name": "v1.0.0", "html_url": "u"}`

	tests := []struct {
		name    string
		size    int
		wantErr bool
	}{
		{name: "exactly the limit", size: maxResponseBody, wantErr: false},
		{name: "one byte over", size: maxResponseBody + 1, wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			body := doc + strings.Repeat(" ", tc.size-len(doc))
			f := newFakeGitHub(t, respond(http.StatusOK, ""), respond(http.StatusOK, body))

			got, err := f.source().Latest(context.Background())
			if !tc.wantErr {
				if err != nil {
					t.Fatalf("Latest() error = %v", err)
				}
				if got.Tag != "v1.0.0" {
					t.Errorf("Latest().Tag = %q, want %q", got.Tag, "v1.0.0")
				}
				return
			}

			if err == nil {
				t.Fatal("Latest() error = nil, want a body-limit error")
			}
			if want := fmt.Sprintf("exceeds %d bytes", maxResponseBody); !strings.Contains(err.Error(), want) {
				t.Errorf("Latest() error = %q, want it to contain %q", err, want)
			}
		})
	}
}

func TestGitHubSource_APIInvalidJSON(t *testing.T) {
	t.Parallel()

	f := newFakeGitHub(t, respond(http.StatusOK, ""), respond(http.StatusOK, "{not json"))

	_, err := f.source().Latest(context.Background())
	if err == nil || !strings.Contains(err.Error(), "decoding release") {
		t.Fatalf("Latest() error = %v, want a decoding error", err)
	}
}

func TestGitHubSource_Headers(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	seen := map[string]http.Header{}
	record := func(name string, next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			seen[name] = r.Header.Clone()
			mu.Unlock()
			next(w, r)
		}
	}

	f := newFakeGitHub(t,
		record("web", respond(http.StatusOK, "")),
		record("api", respond(http.StatusOK, apiReleaseJSON("v1.0.0"))),
	)

	if _, err := f.source().Latest(context.Background()); err != nil {
		t.Fatalf("Latest() error = %v", err)
	}

	mu.Lock()
	defer mu.Unlock()

	want := map[string]string{
		"User-Agent":           "go.vipengele.org/update",
		"Accept":               "application/vnd.github+json",
		"X-GitHub-Api-Version": "2022-11-28",
	}
	for _, endpoint := range []string{"web", "api"} {
		h, ok := seen[endpoint]
		if !ok {
			t.Errorf("no request reached the %s endpoint", endpoint)
			continue
		}
		for name, value := range want {
			if got := h.Values(name); len(got) != 1 || got[0] != value {
				t.Errorf("%s request %s = %q, want [%q]", endpoint, name, got, value)
			}
		}
	}
}

// blockUntilDone answers only once the request's context ends.
func blockUntilDone(w http.ResponseWriter, r *http.Request) {
	<-r.Context().Done()
	w.WriteHeader(http.StatusGatewayTimeout)
}

func TestGitHubSource_Timeout(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                 string
		web                  http.HandlerFunc
		api                  http.HandlerFunc
		timeout              time.Duration
		ctxTimeout           time.Duration
		client               func(*http.Client)
		wantPath             string
		wantDeadlineExceeded bool // whether errors.Is(err, context.DeadlineExceeded) is asserted
	}{
		{
			name:                 "source timeout on the redirect",
			web:                  blockUntilDone,
			timeout:              50 * time.Millisecond,
			wantPath:             webPath,
			wantDeadlineExceeded: true,
		},
		{
			name:                 "source timeout on the API",
			web:                  respond(http.StatusOK, ""),
			api:                  blockUntilDone,
			timeout:              50 * time.Millisecond,
			wantPath:             apiPath,
			wantDeadlineExceeded: true,
		},
		{
			name:                 "shorter caller deadline",
			web:                  blockUntilDone,
			ctxTimeout:           50 * time.Millisecond,
			wantPath:             webPath,
			wantDeadlineExceeded: true,
		},
		{
			name:     "HTTP client timeout",
			web:      blockUntilDone,
			client:   func(c *http.Client) { c.Timeout = 50 * time.Millisecond },
			wantPath: webPath,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			api := tc.api
			if api == nil {
				api = unexpected(t, "API")
			}
			f := newFakeGitHub(t, tc.web, api)
			s := f.source()
			s.Timeout = tc.timeout
			if tc.client != nil {
				c := *s.HTTPClient
				tc.client(&c)
				s.HTTPClient = &c
			}

			ctx := context.Background()
			if tc.ctxTimeout > 0 {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, tc.ctxTimeout)
				defer cancel()
			}

			_, err := s.Latest(ctx)

			var timeoutErr *TimeoutError
			if !errors.As(err, &timeoutErr) {
				t.Fatalf("Latest() error = %v, want a *TimeoutError", err)
			}
			if want := f.server.URL + tc.wantPath; timeoutErr.URL != want {
				t.Errorf("TimeoutError.URL = %q, want %q", timeoutErr.URL, want)
			}
			if tc.wantDeadlineExceeded && !errors.Is(err, context.DeadlineExceeded) {
				t.Errorf("errors.Is(err, context.DeadlineExceeded) = false, want true")
			}
		})
	}
}

func TestGitHubSource_TimeoutIsPerRequest(t *testing.T) {
	t.Parallel()

	slow := func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(300 * time.Millisecond)
			next(w, r)
		}
	}
	f := newFakeGitHub(t,
		slow(respond(http.StatusOK, "")),
		slow(respond(http.StatusOK, apiReleaseJSON("v1.0.0"))),
	)
	s := f.source()
	s.Timeout = 500 * time.Millisecond

	got, err := s.Latest(context.Background())
	if err != nil {
		t.Fatalf("Latest() error = %v, want the two requests bounded separately", err)
	}
	if got.Tag != "v1.0.0" {
		t.Errorf("Latest().Tag = %q, want %q", got.Tag, "v1.0.0")
	}
}

func TestGitHubSource_CallerCancellation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		web  func(arrived chan<- struct{}) http.HandlerFunc
		api  func(arrived chan<- struct{}) http.HandlerFunc
	}{
		{
			name: "during the redirect",
			web: func(arrived chan<- struct{}) http.HandlerFunc {
				return func(w http.ResponseWriter, r *http.Request) {
					close(arrived)
					blockUntilDone(w, r)
				}
			},
		},
		{
			name: "during the API request",
			web:  func(chan<- struct{}) http.HandlerFunc { return respond(http.StatusOK, "") },
			api: func(arrived chan<- struct{}) http.HandlerFunc {
				return func(w http.ResponseWriter, r *http.Request) {
					close(arrived)
					blockUntilDone(w, r)
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			arrived := make(chan struct{})
			api := unexpected(t, "API")
			if tc.api != nil {
				api = tc.api(arrived)
			}
			f := newFakeGitHub(t, tc.web(arrived), api)

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			go func() {
				<-arrived
				cancel()
			}()

			_, err := f.source().Latest(ctx)
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("Latest() error = %v, want context.Canceled", err)
			}
			var timeoutErr *TimeoutError
			if errors.As(err, &timeoutErr) {
				t.Errorf("Latest() error = %v, want no *TimeoutError", err)
			}
		})
	}
}

func TestGitHubSource_DoesNotFollowOrModifyCallerClient(t *testing.T) {
	t.Parallel()

	f := newFakeGitHub(t, redirectTo(http.StatusFound, tagPagePath+"v1.2.3"), unexpected(t, "API"))
	client := f.server.Client()
	s := f.source()
	s.HTTPClient = client

	if _, err := s.Latest(context.Background()); err != nil {
		t.Fatalf("Latest() error = %v", err)
	}
	if client.CheckRedirect != nil {
		t.Error("Latest() set CheckRedirect on the caller's HTTPClient")
	}
}

func TestGitHubSource_RedirectRequestFailureFallsBackToAPI(t *testing.T) {
	t.Parallel()

	unreachable := httptest.NewServer(http.NotFoundHandler())
	unreachable.Close()

	f := newFakeGitHub(t, unexpected(t, "web"), respond(http.StatusOK, apiReleaseJSON("v1.4.0")))
	s := f.source()
	s.WebURL = unreachable.URL

	got, err := s.Latest(context.Background())
	if err != nil {
		t.Fatalf("Latest() error = %v", err)
	}
	if got.Tag != "v1.4.0" {
		t.Errorf("Latest().Tag = %q, want %q", got.Tag, "v1.4.0")
	}
	if n := f.apiCalls.Load(); n != 1 {
		t.Errorf("API requests = %d, want 1", n)
	}
}

func TestGitHubSource_TrailingSlashInBaseURLs(t *testing.T) {
	t.Parallel()

	f := newFakeGitHub(t, respond(http.StatusOK, ""), respond(http.StatusOK, apiReleaseJSON("v1.0.0")))
	s := f.source()
	s.WebURL += "/"
	s.APIURL += "/"

	if _, err := s.Latest(context.Background()); err != nil {
		t.Fatalf("Latest() error = %v", err)
	}
	if n := f.webCalls.Load(); n != 1 {
		t.Errorf("web requests = %d, want 1", n)
	}
	if n := f.apiCalls.Load(); n != 1 {
		t.Errorf("API requests = %d, want 1", n)
	}
}

func TestGitHubSource_Defaults(t *testing.T) {
	t.Parallel()

	var zero GitHubSource
	if got := zero.webURL(); got != "https://github.com" {
		t.Errorf("webURL() = %q, want %q", got, "https://github.com")
	}
	if got := zero.apiURL(); got != "https://api.github.com" {
		t.Errorf("apiURL() = %q, want %q", got, "https://api.github.com")
	}
	if got := zero.client(); got == nil || got == http.DefaultClient {
		t.Errorf("client() = %p, want a new client", got)
	}
	if zero.client() == zero.client() {
		t.Error("client() returned the same client twice, want a new one each time")
	}

	set := GitHubSource{WebURL: "https://web.test", APIURL: "https://api.test", HTTPClient: &http.Client{}}
	if got := set.webURL(); got != "https://web.test" {
		t.Errorf("webURL() = %q, want %q", got, "https://web.test")
	}
	if got := set.apiURL(); got != "https://api.test" {
		t.Errorf("apiURL() = %q, want %q", got, "https://api.test")
	}
	if got := set.client(); got != set.HTTPClient {
		t.Errorf("client() = %p, want HTTPClient %p", got, set.HTTPClient)
	}

	timeouts := []struct {
		timeout time.Duration
		want    time.Duration
	}{
		{timeout: 0, want: 10 * time.Second},
		{timeout: -time.Second, want: 10 * time.Second},
		{timeout: time.Nanosecond, want: time.Nanosecond},
		{timeout: time.Minute, want: time.Minute},
	}
	for _, tc := range timeouts {
		s := GitHubSource{Timeout: tc.timeout}
		if got := s.timeout(); got != tc.want {
			t.Errorf("timeout() with Timeout %v = %v, want %v", tc.timeout, got, tc.want)
		}
	}
}

func TestNewGitHubSource(t *testing.T) {
	t.Parallel()

	client := &http.Client{}
	got := NewGitHubSource(validIdentity(), client)
	want := &GitHubSource{Owner: "pedromvgomes", Repo: "agentic-toolkit", HTTPClient: client}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("NewGitHubSource() = %#v, want %#v", got, want)
	}
	if got.HTTPClient != client {
		t.Error("NewGitHubSource() did not keep the given HTTPClient")
	}

	if got := NewGitHubSource(validIdentity(), nil); got.HTTPClient != nil {
		t.Errorf("NewGitHubSource(nil client).HTTPClient = %p, want nil", got.HTTPClient)
	}

	var _ Source = NewGitHubSource(validIdentity(), nil)
}

func TestIsRedirect(t *testing.T) {
	t.Parallel()

	for code := 100; code < 600; code++ {
		want := code == 301 || code == 302 || code == 303 || code == 307 || code == 308
		if got := isRedirect(code); got != want {
			t.Errorf("isRedirect(%d) = %v, want %v", code, got, want)
		}
	}
}

// newFakeGitHubList is an httptest server answering the REST API release list with list. A
// request to the latest-release web page or REST API endpoint fails the test.
func newFakeGitHubList(t *testing.T, list http.HandlerFunc) *fakeGitHub {
	t.Helper()

	f := &fakeGitHub{}
	web := unexpected(t, "web")
	api := unexpected(t, "API")
	mux := http.NewServeMux()
	mux.HandleFunc(webPath, func(w http.ResponseWriter, r *http.Request) {
		f.webCalls.Add(1)
		web(w, r)
	})
	mux.HandleFunc(apiPath, func(w http.ResponseWriter, r *http.Request) {
		f.apiCalls.Add(1)
		api(w, r)
	})
	mux.HandleFunc(listPath, func(w http.ResponseWriter, r *http.Request) {
		f.listCalls.Add(1)
		list(w, r)
	})
	f.server = httptest.NewServer(mux)
	t.Cleanup(f.server.Close)

	return f
}

// listSource returns a GitHubSource with IncludePrereleases set whose addresses are the fake
// server.
func (f *fakeGitHub) listSource() *GitHubSource {
	s := f.source()
	s.IncludePrereleases = true

	return s
}

// listedRelease is one entry of a REST API release list document.
type listedRelease struct {
	tag   string
	draft bool
}

// apiListJSON is a REST API release list document holding entries in order. Each entry's html_url
// is the release page of its tag and its one asset is named after its tag.
func apiListJSON(entries ...listedRelease) string {
	parts := make([]string, 0, len(entries))
	for _, e := range entries {
		parts = append(parts, fmt.Sprintf(`{
			"tag_name": %q,
			"html_url": "https://example.test/octo/tool/releases/tag/%s",
			"draft": %t,
			"prerelease": false,
			"assets": [{"name": "tool_%s.tar.gz", "browser_download_url": "https://example.test/%s.tar.gz", "size": 10}]
		}`, e.tag, e.tag, e.draft, e.tag, e.tag))
	}

	return "[" + strings.Join(parts, ",") + "]"
}

// listedReleaseOf is the Release apiListJSON describes for tag.
func listedReleaseOf(tag string) Release {
	return Release{
		Tag: tag,
		URL: "https://example.test/octo/tool/releases/tag/" + tag,
		Assets: []Asset{
			{Name: "tool_" + tag + ".tar.gz", URL: "https://example.test/" + tag + ".tar.gz", Size: 10},
		},
	}
}

func TestGitHubSource_ListReturnsHighestEligible(t *testing.T) {
	t.Parallel()

	stable := func(tag string) listedRelease { return listedRelease{tag: tag} }
	draft := func(tag string) listedRelease { return listedRelease{tag: tag, draft: true} }

	tests := []struct {
		name    string
		entries []listedRelease
		want    string
	}{
		{name: "only entry", entries: []listedRelease{stable("v1.0.0")}, want: "v1.0.0"},
		{
			name:    "highest in the middle",
			entries: []listedRelease{stable("v1.2.0"), stable("v1.10.0"), stable("v1.9.0")},
			want:    "v1.10.0",
		},
		{
			name:    "highest last",
			entries: []listedRelease{stable("v0.9.0"), stable("v1.0.0"), stable("v2.0.0")},
			want:    "v2.0.0",
		},
		{
			name:    "highest first",
			entries: []listedRelease{stable("v3.0.0"), stable("v1.0.0"), stable("v2.0.0")},
			want:    "v3.0.0",
		},
		{
			name:    "pre-release above the highest stable",
			entries: []listedRelease{stable("v1.2.0"), stable("v1.3.0-rc.1"), stable("v1.2.9")},
			want:    "v1.3.0-rc.1",
		},
		{
			name:    "stable above its own pre-release",
			entries: []listedRelease{stable("v1.3.0-rc.1"), stable("v1.3.0"), stable("v1.3.0-rc.2")},
			want:    "v1.3.0",
		},
		{
			name:    "later pre-release above an earlier one",
			entries: []listedRelease{stable("v2.0.0-alpha"), stable("v2.0.0-beta"), stable("v1.0.0")},
			want:    "v2.0.0-beta",
		},
		{
			name:    "highest draft skipped",
			entries: []listedRelease{stable("v1.0.0"), draft("v9.0.0"), stable("v1.1.0")},
			want:    "v1.1.0",
		},
		{
			name:    "draft pre-release skipped",
			entries: []listedRelease{draft("v2.0.0-rc.1"), stable("v1.0.0")},
			want:    "v1.0.0",
		},
		{
			name: "odd tags skipped",
			entries: []listedRelease{
				stable("9.0.0"),
				stable("source/update/v9.0.0"),
				stable("v9.0.0+dirty"),
				stable("v9.0"),
				stable("latest"),
				stable("v0.0.0-20990101000000-abcdefabcdef"),
				stable("v1.0.0"),
				stable(""),
			},
			want: "v1.0.0",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := newFakeGitHubList(t, respond(http.StatusOK, apiListJSON(tc.entries...)))

			got, err := f.listSource().Latest(context.Background())
			if err != nil {
				t.Fatalf("Latest() error = %v", err)
			}

			if want := listedReleaseOf(tc.want); !reflect.DeepEqual(got, want) {
				t.Errorf("Latest() = %#v, want %#v", got, want)
			}
			if n := f.listCalls.Load(); n != 1 {
				t.Errorf("list requests = %d, want 1", n)
			}
		})
	}
}

func TestGitHubSource_ListAssets(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body string
		want []Asset
	}{
		{name: "no assets field", body: `[{"tag_name": "v1.0.0", "html_url": "u"}]`, want: []Asset{}},
		{name: "empty assets", body: `[{"tag_name": "v1.0.0", "html_url": "u", "assets": []}]`, want: []Asset{}},
		{
			name: "two assets",
			body: `[{"tag_name": "v1.0.0", "html_url": "u", "assets": [
				{"name": "a", "browser_download_url": "da", "size": 1},
				{"name": "b", "browser_download_url": "db", "size": 2}
			]}]`,
			want: []Asset{{Name: "a", URL: "da", Size: 1}, {Name: "b", URL: "db", Size: 2}},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := newFakeGitHubList(t, respond(http.StatusOK, tc.body))

			got, err := f.listSource().Latest(context.Background())
			if err != nil {
				t.Fatalf("Latest() error = %v", err)
			}

			want := Release{Tag: "v1.0.0", URL: "u", Assets: tc.want}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("Latest() = %#v, want %#v", got, want)
			}
			if got.Assets == nil {
				t.Error("Latest().Assets = nil, want a non-nil slice")
			}
		})
	}
}

func TestGitHubSource_PrereleaseOnlyWhenIncluded(t *testing.T) {
	t.Parallel()

	const tag = "v2.0.0-rc.1"

	t.Run("included", func(t *testing.T) {
		t.Parallel()

		f := newFakeGitHubList(t, respond(http.StatusOK, apiListJSON(listedRelease{tag: tag})))

		got, err := f.listSource().Latest(context.Background())
		if err != nil {
			t.Fatalf("Latest() error = %v", err)
		}
		if want := listedReleaseOf(tag); !reflect.DeepEqual(got, want) {
			t.Errorf("Latest() = %#v, want %#v", got, want)
		}
	})

	t.Run("not included", func(t *testing.T) {
		t.Parallel()

		f := newFakeGitHub(t, redirectTo(http.StatusFound, tagPagePath+tag), unexpected(t, "API"))

		_, err := f.source().Latest(context.Background())
		if !errors.Is(err, ErrPrerelease) {
			t.Fatalf("Latest() error = %v, want ErrPrerelease", err)
		}
	})
}

func TestGitHubSource_ListWithoutEligibleRelease(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body string
	}{
		{name: "empty list", body: `[]`},
		{name: "null", body: `null`},
		{name: "only drafts", body: apiListJSON(listedRelease{tag: "v1.0.0", draft: true})},
		{
			name: "only odd tags",
			body: apiListJSON(listedRelease{tag: "1.0.0"}, listedRelease{tag: "source/update/v1.0.0"}),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := newFakeGitHubList(t, respond(http.StatusOK, tc.body))

			got, err := f.listSource().Latest(context.Background())
			if !errors.Is(err, errNoListedRelease) {
				t.Fatalf("Latest() error = %v, want errNoListedRelease", err)
			}
			if want := f.server.URL + listPath + "?" + listQuery; !strings.Contains(err.Error(), want) {
				t.Errorf("Latest() error = %q, want it to name %q", err, want)
			}
			if !reflect.DeepEqual(got, Release{}) {
				t.Errorf("Latest() = %#v, want the zero Release", got)
			}
		})
	}
}

func TestGitHubSource_ListRequest(t *testing.T) {
	t.Parallel()

	var (
		mu      sync.Mutex
		path    string
		query   string
		headers http.Header
	)
	f := newFakeGitHubList(t, func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		path, query, headers = r.URL.Path, r.URL.RawQuery, r.Header.Clone()
		mu.Unlock()
		respond(http.StatusOK, apiListJSON(listedRelease{tag: "v1.0.0"}))(w, r)
	})
	s := f.listSource()
	s.APIURL += "/"

	if _, err := s.Latest(context.Background()); err != nil {
		t.Fatalf("Latest() error = %v", err)
	}

	mu.Lock()
	defer mu.Unlock()

	if path != listPath {
		t.Errorf("list request path = %q, want %q", path, listPath)
	}
	if query != listQuery {
		t.Errorf("list request query = %q, want %q", query, listQuery)
	}
	want := map[string]string{
		"User-Agent":           "go.vipengele.org/update",
		"Accept":               "application/vnd.github+json",
		"X-GitHub-Api-Version": "2022-11-28",
	}
	for name, value := range want {
		if got := headers.Values(name); len(got) != 1 || got[0] != value {
			t.Errorf("list request %s = %q, want [%q]", name, got, value)
		}
	}
	if n := f.listCalls.Load(); n != 1 {
		t.Errorf("list requests = %d, want 1", n)
	}
	if n := f.webCalls.Load(); n != 0 {
		t.Errorf("web requests = %d, want 0", n)
	}
	if n := f.apiCalls.Load(); n != 0 {
		t.Errorf("latest-release API requests = %d, want 0", n)
	}
}

func TestGitHubSource_ListStatusError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		status int
		body   string
	}{
		{name: "404", status: http.StatusNotFound, body: `{"message":"Not Found"}`},
		{name: "500", status: http.StatusInternalServerError, body: "boom"},
		{name: "300", status: http.StatusMultipleChoices, body: ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := newFakeGitHubList(t, respond(tc.status, tc.body))

			_, err := f.listSource().Latest(context.Background())

			var statusErr *StatusError
			if !errors.As(err, &statusErr) {
				t.Fatalf("Latest() error = %v, want a *StatusError", err)
			}
			want := StatusError{URL: f.server.URL + listPath + "?" + listQuery, StatusCode: tc.status, Body: tc.body}
			if *statusErr != want {
				t.Errorf("StatusError = %#v, want %#v", *statusErr, want)
			}
		})
	}
}

func TestGitHubSource_ListSuccessStatus(t *testing.T) {
	t.Parallel()

	for _, status := range []int{http.StatusOK, http.StatusCreated, 299} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			t.Parallel()

			f := newFakeGitHubList(t, respond(status, apiListJSON(listedRelease{tag: "v1.0.0"})))

			got, err := f.listSource().Latest(context.Background())
			if err != nil {
				t.Fatalf("Latest() error = %v", err)
			}
			if got.Tag != "v1.0.0" {
				t.Errorf("Latest().Tag = %q, want %q", got.Tag, "v1.0.0")
			}
		})
	}
}

func TestGitHubSource_ListInvalidJSON(t *testing.T) {
	t.Parallel()

	for _, body := range []string{"[{not json", `{"tag_name": "v1.0.0"}`, ""} {
		t.Run(fmt.Sprintf("%q", body), func(t *testing.T) {
			t.Parallel()

			f := newFakeGitHubList(t, respond(http.StatusOK, body))

			_, err := f.listSource().Latest(context.Background())
			if err == nil || !strings.Contains(err.Error(), "decoding releases") {
				t.Fatalf("Latest() error = %v, want a decoding error", err)
			}
			if errors.Is(err, errNoListedRelease) {
				t.Errorf("Latest() error = %v, want a decoding error, not errNoListedRelease", err)
			}
		})
	}
}

func TestGitHubSource_ListBodyLimit(t *testing.T) {
	t.Parallel()

	doc := `[{"tag_name": "v1.0.0", "html_url": "u"}]`

	tests := []struct {
		name    string
		size    int
		wantErr bool
	}{
		{name: "larger than a single release may be", size: maxResponseBody + 1, wantErr: false},
		{name: "exactly the limit", size: maxListResponseBody, wantErr: false},
		{name: "one byte over", size: maxListResponseBody + 1, wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			body := doc + strings.Repeat(" ", tc.size-len(doc))
			f := newFakeGitHubList(t, respond(http.StatusOK, body))

			got, err := f.listSource().Latest(context.Background())
			if !tc.wantErr {
				if err != nil {
					t.Fatalf("Latest() error = %v", err)
				}
				if got.Tag != "v1.0.0" {
					t.Errorf("Latest().Tag = %q, want %q", got.Tag, "v1.0.0")
				}
				return
			}

			if want := fmt.Sprintf("exceeds %d bytes", maxListResponseBody); err == nil || !strings.Contains(err.Error(), want) {
				t.Errorf("Latest() error = %v, want it to contain %q", err, want)
			}
		})
	}
}

func TestGitHubSource_ListTimeout(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                 string
		timeout              time.Duration
		ctxTimeout           time.Duration
		client               func(*http.Client)
		wantDeadlineExceeded bool // whether errors.Is(err, context.DeadlineExceeded) is asserted
	}{
		{name: "source timeout", timeout: 50 * time.Millisecond, wantDeadlineExceeded: true},
		{name: "shorter caller deadline", ctxTimeout: 50 * time.Millisecond, wantDeadlineExceeded: true},
		{name: "HTTP client timeout", client: func(c *http.Client) { c.Timeout = 50 * time.Millisecond }},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := newFakeGitHubList(t, blockUntilDone)
			s := f.listSource()
			s.Timeout = tc.timeout
			if tc.client != nil {
				c := *s.HTTPClient
				tc.client(&c)
				s.HTTPClient = &c
			}

			ctx := context.Background()
			if tc.ctxTimeout > 0 {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, tc.ctxTimeout)
				defer cancel()
			}

			_, err := s.Latest(ctx)

			var timeoutErr *TimeoutError
			if !errors.As(err, &timeoutErr) {
				t.Fatalf("Latest() error = %v, want a *TimeoutError", err)
			}
			if want := f.server.URL + listPath + "?" + listQuery; timeoutErr.URL != want {
				t.Errorf("TimeoutError.URL = %q, want %q", timeoutErr.URL, want)
			}
			if tc.wantDeadlineExceeded && !errors.Is(err, context.DeadlineExceeded) {
				t.Errorf("errors.Is(err, context.DeadlineExceeded) = false, want true")
			}
		})
	}
}

func TestGitHubSource_ListCallerCancellation(t *testing.T) {
	t.Parallel()

	arrived := make(chan struct{})
	f := newFakeGitHubList(t, func(w http.ResponseWriter, r *http.Request) {
		close(arrived)
		blockUntilDone(w, r)
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		<-arrived
		cancel()
	}()

	_, err := f.listSource().Latest(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Latest() error = %v, want context.Canceled", err)
	}
	var timeoutErr *TimeoutError
	if errors.As(err, &timeoutErr) {
		t.Errorf("Latest() error = %v, want no *TimeoutError", err)
	}
}
