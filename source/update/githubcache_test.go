package update

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

// memCache is a ResponseCache in memory that counts its Puts.
type memCache struct {
	mu      sync.Mutex
	entries map[string]CachedResponse
	puts    int
}

func newMemCache() *memCache {
	return &memCache{entries: map[string]CachedResponse{}}
}

func (m *memCache) Get(_ context.Context, url string) (CachedResponse, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	r, ok := m.entries[url]

	return r, ok
}

func (m *memCache) Put(_ context.Context, url string, r CachedResponse) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.entries[url] = r
	m.puts++

	return nil
}

func (m *memCache) entry(url string) (CachedResponse, bool) {
	return m.Get(context.Background(), url)
}

func (m *memCache) putCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.puts
}

// etagHandler answers a request whose If-None-Match is etag with 304 Not Modified and any other
// with 200, body and the ETag header etag. It records each request's If-None-Match.
type etagHandler struct {
	etag string
	body string

	mu   sync.Mutex
	seen []string
}

func (h *etagHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	inm := r.Header.Get("If-None-Match")

	h.mu.Lock()
	h.seen = append(h.seen, inm)
	h.mu.Unlock()

	if inm != "" && inm == h.etag {
		w.WriteHeader(http.StatusNotModified)

		return
	}

	w.Header().Set("ETag", h.etag)
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, h.body)
}

func (h *etagHandler) ifNoneMatch() []string {
	h.mu.Lock()
	defer h.mu.Unlock()

	return append([]string(nil), h.seen...)
}

// cachedEndpoint is one REST API path a GitHubSource caches: a source pointed at a fake server
// answering it with h, and the URL its responses are cached under.
type cachedEndpoint struct {
	name    string
	channel Channel
	source  func(t *testing.T, h http.Handler) (s *GitHubSource, url string)
	body    string
	want    Release
}

func cachedEndpoints() []cachedEndpoint {
	return []cachedEndpoint{
		{
			name:    "latest",
			channel: Stable,
			source: func(t *testing.T, h http.Handler) (*GitHubSource, string) {
				t.Helper()
				f := newFakeGitHub(t, respond(http.StatusOK, ""), h.ServeHTTP)

				return f.source(), f.server.URL + apiPath
			},
			body: apiReleaseJSON("v1.4.0"),
			want: Release{Tag: "v1.4.0", URL: "https://example.test/octo/tool/releases/tag/v1.4.0", Assets: apiAssets()},
		},
		{
			name:    "list",
			channel: Edge,
			source: func(t *testing.T, h http.Handler) (*GitHubSource, string) {
				t.Helper()
				f := newFakeGitHubList(t, h.ServeHTTP)

				return f.source(), f.server.URL + listPath + "?" + page1Query
			},
			body: apiListJSON(listedRelease{tag: "v1.0.0"}, listedRelease{tag: "v2.0.0-rc.1"}),
			want: listedReleaseOf("v2.0.0-rc.1"),
		},
	}
}

func TestGitHubSource_CacheSendsIfNoneMatchOnlyForAnEntry(t *testing.T) {
	t.Parallel()

	for _, ep := range cachedEndpoints() {
		t.Run(ep.name, func(t *testing.T) {
			t.Parallel()

			h := &etagHandler{etag: `"one"`, body: ep.body}
			s, url := ep.source(t, h)
			cache := newMemCache()
			cache.entries["https://example.test/another-url"] = CachedResponse{ETag: `"other"`, Body: []byte(ep.body)}
			s.Cache = cache

			if _, err := s.Latest(context.Background(), ep.channel); err != nil {
				t.Fatalf("first Latest() error = %v", err)
			}
			if _, ok := cache.entry(url); !ok {
				t.Fatalf("no entry for %s after a 200 with an ETag", url)
			}
			if _, err := s.Latest(context.Background(), ep.channel); err != nil {
				t.Fatalf("second Latest() error = %v", err)
			}

			if got := h.ifNoneMatch(); len(got) != 2 || got[0] != "" || got[1] != `"one"` {
				t.Errorf("If-None-Match = %q, want none and then %q", got, `"one"`)
			}
		})
	}
}

func TestGitHubSource_CacheWithoutCacheSendsNoIfNoneMatch(t *testing.T) {
	t.Parallel()

	h := &etagHandler{etag: `"one"`, body: apiReleaseJSON("v1.4.0")}
	f := newFakeGitHub(t, respond(http.StatusOK, ""), h.ServeHTTP)
	s := f.source()

	for range 2 {
		if _, err := s.Latest(context.Background(), Stable); err != nil {
			t.Fatalf("Latest() error = %v", err)
		}
	}
	if got := h.ifNoneMatch(); len(got) != 2 || got[0] != "" || got[1] != "" {
		t.Errorf("If-None-Match = %q, want none on either request", got)
	}
}

func TestGitHubSource_CacheNotModifiedReturnsTheCachedRelease(t *testing.T) {
	t.Parallel()

	for _, ep := range cachedEndpoints() {
		t.Run(ep.name, func(t *testing.T) {
			t.Parallel()

			// The server's body differs from the cached one, so the release can only come from the
			// cache when the server answers 304.
			h := &etagHandler{etag: `"cached"`, body: "not the cached body"}
			s, url := ep.source(t, h)
			cache := newMemCache()
			cache.entries[url] = CachedResponse{ETag: `"cached"`, Body: []byte(ep.body)}
			s.Cache = cache

			got, err := s.Latest(context.Background(), ep.channel)
			if err != nil {
				t.Fatalf("Latest() error = %v", err)
			}
			if !reflect.DeepEqual(got, ep.want) {
				t.Errorf("Latest() = %+v, want %+v", got, ep.want)
			}
			if n := cache.putCount(); n != 0 {
				t.Errorf("Put called %d times on a 304, want 0", n)
			}
		})
	}
}

func TestGitHubSource_CacheOKWithAnETagReplacesTheEntry(t *testing.T) {
	t.Parallel()

	for _, ep := range cachedEndpoints() {
		t.Run(ep.name, func(t *testing.T) {
			t.Parallel()

			h := &etagHandler{etag: `"new"`, body: ep.body}
			s, url := ep.source(t, h)
			cache := newMemCache()
			cache.entries[url] = CachedResponse{ETag: `"old"`, Body: []byte("stale")}
			s.Cache = cache

			got, err := s.Latest(context.Background(), ep.channel)
			if err != nil {
				t.Fatalf("Latest() error = %v", err)
			}
			if !reflect.DeepEqual(got, ep.want) {
				t.Errorf("Latest() = %+v, want %+v", got, ep.want)
			}
			if seen := h.ifNoneMatch(); len(seen) != 1 || seen[0] != `"old"` {
				t.Errorf("If-None-Match = %q, want %q", seen, `"old"`)
			}
			entry, _ := cache.entry(url)
			if entry.ETag != `"new"` || string(entry.Body) != ep.body {
				t.Errorf("entry = %q %q, want the 200's ETag and body", entry.ETag, entry.Body)
			}
		})
	}
}

func TestGitHubSource_CacheOKWithoutAnETagWritesNothing(t *testing.T) {
	t.Parallel()

	f := newFakeGitHub(t, respond(http.StatusOK, ""), respond(http.StatusOK, apiReleaseJSON("v1.4.0")))
	s := f.source()
	cache := newMemCache()
	s.Cache = cache

	if _, err := s.Latest(context.Background(), Stable); err != nil {
		t.Fatalf("Latest() error = %v", err)
	}
	if n := cache.putCount(); n != 0 {
		t.Errorf("Put called %d times, want 0", n)
	}
}

func TestGitHubSource_CacheErrorResponsesIgnoreTheEntry(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		handler http.HandlerFunc
		token   bool
		check   func(error) bool
	}{
		{
			name:    "rate limited",
			handler: withHeaders(http.StatusForbidden, "", "X-RateLimit-Limit", "60", "X-RateLimit-Remaining", "0"),
			check:   func(err error) bool { var rl *RateLimitError; return errors.As(err, &rl) },
		},
		{
			name:    "401 with a token",
			handler: respond(http.StatusUnauthorized, "bad credentials"),
			token:   true,
			check:   func(err error) bool { var tr *TokenRejectedError; return errors.As(err, &tr) },
		},
		{
			name:    "500",
			handler: respond(http.StatusInternalServerError, ""),
			check: func(err error) bool {
				var se *StatusError
				return errors.As(err, &se) && se.StatusCode == http.StatusInternalServerError
			},
		},
	} {
		for _, endpoint := range []string{"latest", "list"} {
			t.Run(tc.name+"/"+endpoint, func(t *testing.T) {
				t.Parallel()

				var (
					s   *GitHubSource
					url string
				)
				ch := Stable
				if endpoint == "latest" {
					f := newFakeGitHub(t, respond(http.StatusOK, ""), tc.handler)
					s, url = f.source(), f.server.URL+apiPath
				} else {
					f := newFakeGitHubList(t, tc.handler)
					s, url = f.source(), f.server.URL+listPath+"?"+page1Query
					ch = Edge
				}
				if tc.token {
					s = withToken(s)
				}
				cache := newMemCache()
				cache.entries[url] = CachedResponse{ETag: `"cached"`, Body: []byte(apiReleaseJSON("v1.4.0"))}
				s.Cache = cache

				got, err := s.Latest(context.Background(), ch)
				if !tc.check(err) {
					t.Fatalf("Latest() error = %v, want the response's error", err)
				}
				if got.Tag != "" {
					t.Errorf("Latest() = %+v, want the zero Release", got)
				}
			})
		}
	}
}

func TestGitHubSource_CacheNotModifiedWithoutAnEntryIsAStatusError(t *testing.T) {
	t.Parallel()

	f := newFakeGitHub(t, respond(http.StatusOK, ""), respond(http.StatusNotModified, ""))
	s := f.source()
	s.Cache = newMemCache()

	_, err := s.Latest(context.Background(), Stable)

	var se *StatusError
	if !errors.As(err, &se) || se.StatusCode != http.StatusNotModified {
		t.Fatalf("Latest() error = %v, want a *StatusError with status 304", err)
	}
}

func TestGitHubSource_CacheMissingOrCorruptFileIsAMiss(t *testing.T) {
	t.Parallel()

	for name, content := range map[string]*string{
		"missing": nil,
		"corrupt": ptr("{not json"),
		"empty":   ptr(""),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			if content != nil {
				if err := os.WriteFile(filepath.Join(dir, cacheFileName), []byte(*content), 0o600); err != nil {
					t.Fatal(err)
				}
			}

			h := &etagHandler{etag: `"one"`, body: apiReleaseJSON("v1.4.0")}
			f := newFakeGitHub(t, respond(http.StatusOK, ""), h.ServeHTTP)
			s := f.source()
			s.Cache = NewFileResponseCache(dir)

			if _, err := s.Latest(context.Background(), Stable); err != nil {
				t.Fatalf("Latest() error = %v", err)
			}
			if got := h.ifNoneMatch(); len(got) != 1 || got[0] != "" {
				t.Errorf("If-None-Match = %q, want none", got)
			}

			entry, ok := s.Cache.Get(context.Background(), f.server.URL+apiPath)
			if !ok || entry.ETag != `"one"` {
				t.Errorf("entry after the 200 = %+v, %v, want the response's ETag", entry, ok)
			}
		})
	}
}

// cancelAfterResponse is a transport that reads each response body in full and then cancels the
// request's context, so the response has arrived whole before the context is done.
type cancelAfterResponse struct {
	base   http.RoundTripper
	cancel context.CancelFunc
}

func (c cancelAfterResponse) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := c.base.RoundTrip(req)
	if err != nil {
		return nil, err
	}

	body, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		return nil, err
	}

	resp.Body = io.NopCloser(strings.NewReader(string(body)))
	c.cancel()

	return resp, nil
}

func TestGitHubSource_CacheCancelledContextWritesNothing(t *testing.T) {
	t.Parallel()

	dir := filepath.Join(t.TempDir(), "state")
	h := &etagHandler{etag: `"one"`, body: apiListJSON(listedRelease{tag: "v1.0.0"})}
	f := newFakeGitHubList(t, h.ServeHTTP)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	s := f.source()
	s.HTTPClient = &http.Client{Transport: cancelAfterResponse{base: f.server.Client().Transport, cancel: cancel}}
	cache := newMemCache()
	s.Cache = cache

	_, _ = s.Latest(ctx, Edge)

	if n := cache.putCount(); n != 0 {
		t.Errorf("Put called %d times once ctx was done, want 0", n)
	}

	fc := NewFileResponseCache(dir)
	if err := fc.Put(ctx, "https://example.test/x", CachedResponse{ETag: `"x"`}); !errors.Is(err, context.Canceled) {
		t.Errorf("Put error = %v, want context.Canceled", err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("Stat(%s) = %v, want not exist", dir, err)
	}
}

func TestGitHubSource_CacheStalledIOReturnsWhenContextIsCancelled(t *testing.T) {
	t.Parallel()

	started := make(chan struct{}, 1)
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })

	f := newFakeGitHub(t, respond(http.StatusOK, ""), respond(http.StatusOK, apiReleaseJSON("v1.4.0")))
	s := f.source()
	s.Cache = &fileCache{
		dir: t.TempDir(),
		readFile: func() (cacheFile, error) {
			started <- struct{}{}
			<-release

			return cacheFile{}, nil
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() {
		_, err := s.Latest(ctx, Stable)
		done <- err
	}()

	<-started
	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("Latest() error = %v, want context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Latest did not return after cancellation")
	}
}

func TestFileResponseCacheRoundTripKeepsOtherEntries(t *testing.T) {
	t.Parallel()

	c := NewFileResponseCache(filepath.Join(t.TempDir(), "nested", "state"))
	ctx := context.Background()

	if err := c.Put(ctx, "https://a.test", CachedResponse{ETag: `"a"`, Body: []byte("body a")}); err != nil {
		t.Fatalf("Put a: %v", err)
	}
	if err := c.Put(ctx, "https://b.test", CachedResponse{ETag: `"b"`, Body: []byte("body b")}); err != nil {
		t.Fatalf("Put b: %v", err)
	}
	if err := c.Put(ctx, "https://a.test", CachedResponse{ETag: `"a2"`, Body: []byte("body a2")}); err != nil {
		t.Fatalf("Put a2: %v", err)
	}

	for url, want := range map[string]CachedResponse{
		"https://a.test": {ETag: `"a2"`, Body: []byte("body a2")},
		"https://b.test": {ETag: `"b"`, Body: []byte("body b")},
	} {
		got, ok := c.Get(ctx, url)
		if !ok || got.ETag != want.ETag || string(got.Body) != string(want.Body) {
			t.Errorf("Get(%s) = %q %q, %v, want %q %q", url, got.ETag, got.Body, ok, want.ETag, want.Body)
		}
	}
	if _, ok := c.Get(ctx, "https://c.test"); ok {
		t.Error("Get of a URL never put = hit, want a miss")
	}
}

func TestFileResponseCacheUnreadableFileIsAMiss(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	// A directory in place of the file cannot be read as one.
	if err := os.Mkdir(filepath.Join(dir, cacheFileName), 0o700); err != nil {
		t.Fatal(err)
	}

	if _, ok := NewFileResponseCache(dir).Get(context.Background(), "https://a.test"); ok {
		t.Error("Get = hit, want a miss")
	}
}

func TestFileResponseCachePutFailsWhenTheDirectoryCannotBeCreated(t *testing.T) {
	t.Parallel()

	parent := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(parent, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	err := NewFileResponseCache(filepath.Join(parent, "state")).Put(context.Background(), "https://a.test", CachedResponse{ETag: `"a"`})
	if err == nil {
		t.Fatal("Put = nil, want an error")
	}
}

func TestFileResponseCachePutFailsWhenADirectoryOccupiesTheFilePath(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, cacheFileName), 0o700); err != nil {
		t.Fatal(err)
	}

	err := NewFileResponseCache(dir).Put(context.Background(), "https://a.test", CachedResponse{ETag: `"a"`})
	if err == nil {
		t.Fatal("Put = nil, want an error")
	}
}

func TestFileResponseCacheDoneContextTouchesNothing(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	dir := filepath.Join(t.TempDir(), "state")
	c := &fileCache{
		dir: dir,
		readFile: func() (cacheFile, error) {
			t.Error("read on a done ctx")

			return cacheFile{}, nil
		},
	}

	if _, ok := c.Get(ctx, "https://a.test"); ok {
		t.Error("Get = hit, want a miss")
	}
	if err := c.Put(ctx, "https://a.test", CachedResponse{ETag: `"a"`}); !errors.Is(err, context.Canceled) {
		t.Errorf("Put error = %v, want context.Canceled", err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("Stat(%s) = %v, want not exist", dir, err)
	}
}

func TestFileResponseCacheReturnsWhenContextIsCancelledDuringIO(t *testing.T) {
	t.Parallel()

	started := make(chan struct{}, 2)
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })

	c := &fileCache{
		dir: t.TempDir(),
		readFile: func() (cacheFile, error) {
			started <- struct{}{}
			<-release

			return cacheFile{}, nil
		},
		writeFile: func(cacheFile) error {
			<-release

			return nil
		},
	}

	calls := map[string]func(context.Context) error{
		"Get": func(ctx context.Context) error {
			if _, ok := c.Get(ctx, "https://a.test"); ok {
				return errors.New("hit")
			}

			return ctx.Err()
		},
		"Put": func(ctx context.Context) error { return c.Put(ctx, "https://a.test", CachedResponse{}) },
	}

	for name, call := range calls {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			done := make(chan error, 1)
			go func() { done <- call(ctx) }()

			<-started
			cancel()

			select {
			case err := <-done:
				if !errors.Is(err, context.Canceled) {
					t.Errorf("error = %v, want context.Canceled", err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("call did not return after cancellation")
			}
		})
	}
}
