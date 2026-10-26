package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"go.vipengele.org/commons/validate"
)

const (
	dlTag           = "v1.2.3"
	dlArchiveName   = "tool_linux_amd64.tar.gz"
	dlChecksumsName = "checksums.txt"
	dlArchivePath   = "/download/" + dlTag + "/" + dlArchiveName
	dlChecksumsPath = "/download/" + dlTag + "/" + dlChecksumsName
	dlContent       = "the bytes of the release archive"
)

// sha256Hex is the lower-case hex SHA-256 digest of s.
func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// dlChecksums is a checksums file listing dlContent for the archive and another asset besides.
func dlChecksums() string {
	return sha256Hex("other") + "  tool_darwin_arm64.tar.gz\n" + sha256Hex(dlContent) + "  " + dlArchiveName + "\n"
}

// fakeAssets is an httptest server answering each of its paths with a handler of its own and
// counting the requests each path receives. A path it has no handler for answers 418.
type fakeAssets struct {
	server *httptest.Server
	mu     sync.Mutex
	calls  map[string]int
}

// newFakeAssets starts a fakeAssets serving routes, a handler per request path, and closes it when
// the test ends.
func newFakeAssets(t *testing.T, routes map[string]http.HandlerFunc) *fakeAssets {
	t.Helper()

	f := &fakeAssets{calls: map[string]int{}}
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.calls[r.URL.Path]++
		f.mu.Unlock()

		h, ok := routes[r.URL.Path]
		if !ok {
			w.WriteHeader(http.StatusTeapot)
			return
		}
		h(w, r)
	}))
	t.Cleanup(f.server.Close)

	return f
}

// newFakeRelease starts a fakeAssets serving the archive and the checksums file at dlArchivePath
// and dlChecksumsPath. A nil handler serves the matching file in full, with a Content-Length.
func newFakeRelease(t *testing.T, checksums, archive http.HandlerFunc) *fakeAssets {
	t.Helper()

	if checksums == nil {
		checksums = serveBody(dlChecksums())
	}
	if archive == nil {
		archive = serveBody(dlContent)
	}

	return newFakeAssets(t, map[string]http.HandlerFunc{dlChecksumsPath: checksums, dlArchivePath: archive})
}

// url is the address of path on the server.
func (f *fakeAssets) url(path string) string {
	return f.server.URL + path
}

// count is the number of requests path has received.
func (f *fakeAssets) count(path string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[path]
}

// selection is a listed Selection of the archive and the checksums file at their default paths,
// with no Size.
func (f *fakeAssets) selection() Selection {
	return Selection{
		Tag:       dlTag,
		Archive:   Asset{Name: dlArchiveName, URL: f.url(dlArchivePath)},
		Checksums: Asset{Name: dlChecksumsName, URL: f.url(dlChecksumsPath)},
	}
}

// config is a valid Config whose temporary files go to dir and whose requests go through the
// server's client.
func (f *fakeAssets) config(t *testing.T, dir string) Config {
	t.Helper()

	c := validConfig(t.TempDir())
	c.TempDir = dir
	c.HTTPClient = f.server.Client()

	return c
}

// serveBody answers 200 with body and its Content-Length.
func serveBody(body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		_, _ = io.WriteString(w, body)
	}
}

// serveChunked answers 200 with no Content-Length, flushing after each chunk.
func serveChunked(chunks ...string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		for _, c := range chunks {
			_, _ = io.WriteString(w, c)
			w.(http.Flusher).Flush()
		}
	}
}

// serveChunkedWithLength answers 200 with the Content-Length of every chunk together, flushing
// after each chunk.
func serveChunkedWithLength(chunks ...string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(len(strings.Join(chunks, ""))))
		serveChunked(chunks...)(w, r)
	}
}

// serveTruncated answers 200 announcing a Content-Length of length but sends only body, so the
// client reads an unexpected EOF.
func serveTruncated(length int, body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(length))
		_, _ = io.WriteString(w, body)
	}
}

// redirectHandler answers a 302 to location.
func redirectHandler(location string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, location, http.StatusFound)
	}
}

// progressCall is one call to a WithProgress callback.
type progressCall struct {
	done, total int64
}

// recordProgress returns a WithProgress option recording every call into calls.
func recordProgress(calls *[]progressCall) DownloadOption {
	return WithProgress(func(done, total int64) {
		*calls = append(*calls, progressCall{done, total})
	})
}

// assertMonotonic fails when done decreases between two calls.
func assertMonotonic(t *testing.T, calls []progressCall) {
	t.Helper()
	for i := 1; i < len(calls); i++ {
		if calls[i].done < calls[i-1].done {
			t.Errorf("progress calls %v: done decreases at %d", calls, i)
		}
	}
}

func TestDownloadSucceeds(t *testing.T) {
	t.Parallel()

	n := int64(len(dlContent))

	tests := []struct {
		name    string
		archive http.HandlerFunc
		sel     func(*Selection)
		cfg     func(*Config)
	}{
		{name: "content length", archive: serveBody(dlContent)},
		{name: "chunked", archive: serveChunked(dlContent[:5], dlContent[5:])},
		{
			name:    "chunked with a larger size hint",
			archive: serveChunked(dlContent),
			sel:     func(s *Selection) { s.Archive.Size = 1000 },
		},
		{
			name:    "content length with a smaller size hint",
			archive: serveBody(dlContent),
			sel:     func(s *Selection) { s.Archive.Size = 3 },
		},
		{
			name:    "content length within the cap and a size hint above it",
			archive: serveBody(dlContent),
			sel:     func(s *Selection) { s.Archive.Size = n + 1 },
			cfg:     func(c *Config) { c.MaxDownloadBytes = n },
		},
		{
			name:    "content length at the cap",
			archive: serveBody(dlContent),
			cfg:     func(c *Config) { c.MaxDownloadBytes = n },
		},
		{
			name:    "size hint at the cap",
			archive: serveChunked(dlContent),
			sel:     func(s *Selection) { s.Archive.Size = n },
			cfg:     func(c *Config) { c.MaxDownloadBytes = n },
		},
		{
			name:    "streamed at the cap",
			archive: serveChunked(dlContent),
			cfg:     func(c *Config) { c.MaxDownloadBytes = n },
		},
		{
			name: "checksums size hint at the cap",
			sel:  func(s *Selection) { s.Checksums.Size = maxChecksumsBytes },
		},
		{
			name: "located",
			sel:  func(s *Selection) { s.Located = true },
		},
		{
			name: "default temporary file prefix",
			cfg:  func(c *Config) { c.TempPrefix = "" },
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := newFakeRelease(t, nil, tc.archive)
			dir := t.TempDir()
			cfg := f.config(t, dir)
			cfg.TempPrefix = "tool-update-"
			if tc.cfg != nil {
				tc.cfg(&cfg)
			}
			sel := f.selection()
			if tc.sel != nil {
				tc.sel(&sel)
			}

			a, err := cfg.Download(context.Background(), sel)
			if err != nil {
				t.Fatalf("Download() error = %v", err)
			}
			assertVerifiedArchive(t, a, dir)
			wantPrefix := cfg.TempPrefix
			if wantPrefix == "" {
				wantPrefix = "atk-update-"
			}
			if base := filepath.Base(a.Name()); !strings.HasPrefix(base, wantPrefix) {
				t.Errorf("Name base = %q, want the prefix %q", base, wantPrefix)
			}

			if err := a.Close(); err != nil {
				t.Fatalf("Close: %v", err)
			}
			assertDirEmpty(t, dir)
		})
	}
}

// assertVerifiedArchive checks that a holds dlContent at offset 0 in dir.
func assertVerifiedArchive(t *testing.T, a *Archive, dir string) {
	t.Helper()

	if got := filepath.Dir(a.Name()); got != dir {
		t.Errorf("Name dir = %q, want %q", got, dir)
	}
	if got, want := a.Size(), int64(len(dlContent)); got != want {
		t.Errorf("Size = %d, want %d", got, want)
	}
	off, err := a.Seek(0, io.SeekCurrent)
	if err != nil || off != 0 {
		t.Errorf("offset = %d, %v; want 0", off, err)
	}
	got, err := io.ReadAll(a)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if string(got) != dlContent {
		t.Errorf("content = %q, want %q", got, dlContent)
	}
}

func TestDownloadFollowsRedirects(t *testing.T) {
	t.Parallel()

	var f *fakeAssets
	f = newFakeAssets(t, map[string]http.HandlerFunc{
		"/redirect/archive":   func(w http.ResponseWriter, r *http.Request) { redirectHandler(f.url(dlArchivePath))(w, r) },
		"/redirect/checksums": func(w http.ResponseWriter, r *http.Request) { redirectHandler(f.url(dlChecksumsPath))(w, r) },
		dlArchivePath:         serveBody(dlContent),
		dlChecksumsPath:       serveBody(dlChecksums()),
	})
	dir := t.TempDir()
	sel := f.selection()
	sel.Archive.URL = f.url("/redirect/archive")
	sel.Checksums.URL = f.url("/redirect/checksums")

	a, err := f.config(t, dir).Download(context.Background(), sel)
	if err != nil {
		t.Fatalf("Download() error = %v", err)
	}
	assertVerifiedArchive(t, a, dir)
	if err := a.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	assertDirEmpty(t, dir)
	for _, p := range []string{"/redirect/archive", "/redirect/checksums", dlArchivePath, dlChecksumsPath} {
		if got := f.count(p); got != 1 {
			t.Errorf("requests to %s = %d, want 1", p, got)
		}
	}
}

func TestDownloadFails(t *testing.T) {
	t.Parallel()

	n := int64(len(dlContent))
	wrong := strings.Repeat("x", len(dlContent))

	tests := []struct {
		name      string
		checksums http.HandlerFunc
		archive   http.HandlerFunc
		sel       func(*Selection)
		cfg       func(c *Config, dir string)
		// wantArchiveCalls is the number of archive requests expected.
		wantArchiveCalls int
		// wantChecksumsCalls is the number of checksums requests expected.
		wantChecksumsCalls int
		check              func(t *testing.T, f *fakeAssets, err error)
	}{
		{
			name:               "invalid config",
			cfg:                func(c *Config, _ string) { c.MaxDownloadBytes = -1 },
			wantChecksumsCalls: 0,
			check: func(t *testing.T, _ *fakeAssets, err error) {
				var fe *validate.FieldError
				if !errors.Is(err, validate.ErrInvalid) || !errors.As(err, &fe) || fe.Field != "MaxDownloadBytes" {
					t.Errorf("err = %v, want a MaxDownloadBytes FieldError wrapping validate.ErrInvalid", err)
				}
			},
		},
		{
			name: "invalid config before an empty URL",
			cfg:  func(c *Config, _ string) { c.DownloadTimeout = -1 },
			sel:  func(s *Selection) { s.Archive.URL = "" },
			check: func(t *testing.T, _ *fakeAssets, err error) {
				if !errors.Is(err, validate.ErrInvalid) || errors.Is(err, errNoURL) {
					t.Errorf("err = %v, want only the Config error", err)
				}
			},
		},
		{
			name: "empty archive URL",
			sel:  func(s *Selection) { s.Archive.URL = "" },
			check: func(t *testing.T, _ *fakeAssets, err error) {
				if !errors.Is(err, errNoURL) || !strings.Contains(err.Error(), dlArchiveName) {
					t.Errorf("err = %v, want errNoURL naming the archive", err)
				}
			},
		},
		{
			name: "empty checksums URL",
			sel:  func(s *Selection) { s.Checksums.URL = "" },
			check: func(t *testing.T, _ *fakeAssets, err error) {
				if !errors.Is(err, errNoURL) || !strings.Contains(err.Error(), dlChecksumsName) {
					t.Errorf("err = %v, want errNoURL naming the checksums file", err)
				}
			},
		},
		{
			name:               "checksums server error",
			checksums:          respond(http.StatusInternalServerError, "boom"),
			wantChecksumsCalls: 1,
			check:              wantStatus(dlChecksumsPath, http.StatusInternalServerError, "boom"),
		},
		{
			name:               "archive server error",
			archive:            respond(http.StatusInternalServerError, "boom"),
			wantChecksumsCalls: 1,
			wantArchiveCalls:   1,
			check:              wantStatus(dlArchivePath, http.StatusInternalServerError, "boom"),
		},
		{
			name:               "archive success status other than 200",
			archive:            respond(http.StatusCreated, dlContent),
			wantChecksumsCalls: 1,
			wantArchiveCalls:   1,
			check:              wantStatus(dlArchivePath, http.StatusCreated, dlContent),
		},
		{
			name:               "checksums located 404",
			checksums:          respond(http.StatusNotFound, "Not Found"),
			sel:                func(s *Selection) { s.Located = true },
			wantChecksumsCalls: 1,
			check:              wantAssetNotFound(dlChecksumsName),
		},
		{
			name:               "archive located 404",
			archive:            respond(http.StatusNotFound, "Not Found"),
			sel:                func(s *Selection) { s.Located = true },
			wantChecksumsCalls: 1,
			wantArchiveCalls:   1,
			check:              wantAssetNotFound(dlArchiveName),
		},
		{
			name:               "checksums unlocated 404",
			checksums:          respond(http.StatusNotFound, "Not Found"),
			wantChecksumsCalls: 1,
			check:              wantStatus(dlChecksumsPath, http.StatusNotFound, "Not Found"),
		},
		{
			name:               "archive unlocated 404",
			archive:            respond(http.StatusNotFound, "Not Found"),
			wantChecksumsCalls: 1,
			wantArchiveCalls:   1,
			check:              wantStatus(dlArchivePath, http.StatusNotFound, "Not Found"),
		},
		{
			name:               "located server error",
			archive:            respond(http.StatusInternalServerError, "boom"),
			sel:                func(s *Selection) { s.Located = true },
			wantChecksumsCalls: 1,
			wantArchiveCalls:   1,
			check:              wantStatus(dlArchivePath, http.StatusInternalServerError, "boom"),
		},
		{
			name:               "checksum mismatch",
			archive:            serveBody(wrong),
			wantChecksumsCalls: 1,
			wantArchiveCalls:   1,
			check: func(t *testing.T, _ *fakeAssets, err error) {
				var mismatch *ChecksumMismatchError
				if !errors.As(err, &mismatch) {
					t.Fatalf("err = %v, want a *ChecksumMismatchError", err)
				}
				want := ChecksumMismatchError{Name: dlArchiveName, Want: sha256Hex(dlContent), Got: sha256Hex(wrong)}
				if *mismatch != want {
					t.Errorf("ChecksumMismatchError = %+v, want %+v", *mismatch, want)
				}
			},
		},
		{
			name:               "missing checksum entry",
			checksums:          serveBody(sha256Hex(dlContent) + "  other.tar.gz\n"),
			wantChecksumsCalls: 1,
			check: func(t *testing.T, _ *fakeAssets, err error) {
				var notFound *ChecksumNotFoundError
				if !errors.As(err, &notFound) || notFound.Name != dlArchiveName {
					t.Fatalf("err = %v, want a *ChecksumNotFoundError for %q", err, dlArchiveName)
				}
				if err.Error() != notFound.Error() {
					t.Errorf("err = %q, want it unwrapped: %q", err, notFound)
				}
			},
		},
		{
			name: "conflicting checksum entries",
			checksums: serveBody(sha256Hex(dlContent) + "  " + dlArchiveName + "\n" +
				sha256Hex("other") + "  " + dlArchiveName + "\n"),
			wantChecksumsCalls: 1,
			check: func(t *testing.T, _ *fakeAssets, err error) {
				var conflict *ChecksumConflictError
				if !errors.As(err, &conflict) || conflict.Name != dlArchiveName {
					t.Fatalf("err = %v, want a *ChecksumConflictError for %q", err, dlArchiveName)
				}
				if err.Error() != conflict.Error() {
					t.Errorf("err = %q, want it unwrapped: %q", err, conflict)
				}
			},
		},
		{
			name:               "checksums over the cap while reading",
			checksums:          serveChunked(dlChecksums(), strings.Repeat("\n", maxChecksumsBytes)),
			wantChecksumsCalls: 1,
			check:              wantSizeLimit(dlChecksumsPath, maxChecksumsBytes),
		},
		{
			// The body is cut short, so only the announced length can fail it with a size error.
			name:               "checksums content length over the cap",
			checksums:          serveTruncated(maxChecksumsBytes+1, dlChecksums()),
			wantChecksumsCalls: 1,
			check:              wantSizeLimit(dlChecksumsPath, maxChecksumsBytes),
		},
		{
			name:               "checksums size hint over the cap",
			checksums:          serveChunked(dlChecksums()),
			sel:                func(s *Selection) { s.Checksums.Size = maxChecksumsBytes + 1 },
			wantChecksumsCalls: 1,
			check:              wantSizeLimit(dlChecksumsPath, maxChecksumsBytes),
		},
		{
			name:               "checksums body cut short",
			checksums:          serveTruncated(1000, sha256Hex(dlContent)),
			wantChecksumsCalls: 1,
			check: func(t *testing.T, f *fakeAssets, err error) {
				if !errors.Is(err, io.ErrUnexpectedEOF) {
					t.Errorf("err = %v, want io.ErrUnexpectedEOF", err)
				}
				if want := "update: GET " + f.url(dlChecksumsPath) + ": reading checksums: "; !strings.HasPrefix(err.Error(), want) {
					t.Errorf("err = %q, want the prefix %q", err, want)
				}
			},
		},
		{
			// TempDir does not exist, so a size error proves no temporary file was attempted.
			name:               "archive content length over the cap",
			archive:            serveBody(dlContent),
			cfg:                func(c *Config, dir string) { c.MaxDownloadBytes = n - 1; c.TempDir = filepath.Join(dir, "missing") },
			wantChecksumsCalls: 1,
			wantArchiveCalls:   1,
			check:              wantSizeLimit(dlArchivePath, n-1),
		},
		{
			name:               "archive size hint over the cap",
			archive:            serveChunked(dlContent),
			sel:                func(s *Selection) { s.Archive.Size = n },
			cfg:                func(c *Config, dir string) { c.MaxDownloadBytes = n - 1; c.TempDir = filepath.Join(dir, "missing") },
			wantChecksumsCalls: 1,
			wantArchiveCalls:   1,
			check:              wantSizeLimit(dlArchivePath, n-1),
		},
		{
			name:               "archive over the cap while streaming",
			archive:            serveChunked(dlContent[:5], dlContent[5:]),
			cfg:                func(c *Config, _ string) { c.MaxDownloadBytes = n - 1 },
			wantChecksumsCalls: 1,
			wantArchiveCalls:   1,
			check:              wantSizeLimit(dlArchivePath, n-1),
		},
		{
			name:               "archive body cut short",
			archive:            serveTruncated(len(dlContent)+10, dlContent),
			wantChecksumsCalls: 1,
			wantArchiveCalls:   1,
			check: func(t *testing.T, f *fakeAssets, err error) {
				if !errors.Is(err, io.ErrUnexpectedEOF) {
					t.Errorf("err = %v, want io.ErrUnexpectedEOF", err)
				}
				if want := "update: GET " + f.url(dlArchivePath) + ": "; !strings.HasPrefix(err.Error(), want) {
					t.Errorf("err = %q, want the prefix %q", err, want)
				}
			},
		},
		{
			name:               "missing temporary directory",
			cfg:                func(c *Config, dir string) { c.TempDir = filepath.Join(dir, "missing") },
			wantChecksumsCalls: 1,
			wantArchiveCalls:   1,
			check: func(t *testing.T, _ *fakeAssets, err error) {
				if !errors.Is(err, os.ErrNotExist) {
					t.Errorf("err = %v, want os.ErrNotExist", err)
				}
			},
		},
		{
			name: "unparseable URL",
			sel:  func(s *Selection) { s.Checksums.URL = "http://[::1" },
			check: func(t *testing.T, _ *fakeAssets, err error) {
				if err == nil || !strings.HasPrefix(err.Error(), "update: GET http://[::1: ") {
					t.Errorf("err = %v, want a request error naming the URL", err)
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := newFakeRelease(t, tc.checksums, tc.archive)
			dir := t.TempDir()
			cfg := f.config(t, dir)
			if tc.cfg != nil {
				tc.cfg(&cfg, dir)
			}
			sel := f.selection()
			if tc.sel != nil {
				tc.sel(&sel)
			}

			var calls []progressCall
			a, err := cfg.Download(context.Background(), sel, recordProgress(&calls))
			if err == nil {
				_ = a.Close()
				t.Fatalf("Download() = %v, nil; want an error", a)
			}
			if a != nil {
				t.Errorf("Download() Archive = %v, want nil", a)
			}
			tc.check(t, f, err)
			assertDirEmpty(t, dir)
			if got := f.count(dlChecksumsPath); got != tc.wantChecksumsCalls {
				t.Errorf("checksums requests = %d, want %d", got, tc.wantChecksumsCalls)
			}
			if got := f.count(dlArchivePath); got != tc.wantArchiveCalls {
				t.Errorf("archive requests = %d, want %d", got, tc.wantArchiveCalls)
			}
			assertMonotonic(t, calls)
		})
	}
}

// wantStatus checks for a *StatusError for path with the status and body given.
func wantStatus(path string, status int, body string) func(*testing.T, *fakeAssets, error) {
	return func(t *testing.T, f *fakeAssets, err error) {
		t.Helper()
		var se *StatusError
		if !errors.As(err, &se) {
			t.Fatalf("err = %v, want a *StatusError", err)
		}
		want := StatusError{URL: f.url(path), StatusCode: status, Body: body}
		if *se != want {
			t.Errorf("StatusError = %+v, want %+v", *se, want)
		}
	}
}

// wantAssetNotFound checks for a *AssetNotFoundError for name in the release dlTag.
func wantAssetNotFound(name string) func(*testing.T, *fakeAssets, error) {
	return func(t *testing.T, _ *fakeAssets, err error) {
		t.Helper()
		var nf *AssetNotFoundError
		if !errors.As(err, &nf) {
			t.Fatalf("err = %v, want a *AssetNotFoundError", err)
		}
		if want := (AssetNotFoundError{Tag: dlTag, Name: name}); *nf != want {
			t.Errorf("AssetNotFoundError = %+v, want %+v", *nf, want)
		}
	}
}

// wantSizeLimit checks for a *SizeLimitError for path with the limit given.
func wantSizeLimit(path string, limit int64) func(*testing.T, *fakeAssets, error) {
	return func(t *testing.T, f *fakeAssets, err error) {
		t.Helper()
		var sl *SizeLimitError
		if !errors.As(err, &sl) {
			t.Fatalf("err = %v, want a *SizeLimitError", err)
		}
		if want := (SizeLimitError{URL: f.url(path), Limit: limit}); *sl != want {
			t.Errorf("SizeLimitError = %+v, want %+v", *sl, want)
		}
	}
}

// blockAfter answers 200, sends prefix and then answers nothing more until the request's context
// ends.
func blockAfter(prefix string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, prefix)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}
}

// expiringContext is a context with no deadline of its own whose deadline passes when expire is
// called: Done closes and Err is context.DeadlineExceeded, as for a context.WithDeadline whose
// time has come. It lets a test place a caller's deadline at a point of the exchange rather than at
// a wall-clock offset that a slow run can overtake.
type expiringContext struct {
	done chan struct{}
	once sync.Once
}

func newExpiringContext() *expiringContext {
	return &expiringContext{done: make(chan struct{})}
}

// expire makes the deadline pass. Calls after the first do nothing.
func (c *expiringContext) expire() {
	c.once.Do(func() { close(c.done) })
}

// Deadline implements context.Context: the deadline is not a point in time.
func (*expiringContext) Deadline() (time.Time, bool) {
	return time.Time{}, false
}

// Done implements context.Context.
func (c *expiringContext) Done() <-chan struct{} {
	return c.done
}

// Err implements context.Context.
func (c *expiringContext) Err() error {
	select {
	case <-c.done:
		return context.DeadlineExceeded
	default:
		return nil
	}
}

// Value implements context.Context. The context carries no values.
func (*expiringContext) Value(any) any {
	return nil
}

func TestDownloadTimeout(t *testing.T) {
	t.Parallel()

	// A stall on the archive is reached only after the checksums request completes, and each
	// request has a deadline of its own, so the archive cases' DownloadTimeout must outlast a fast
	// checksums request under any load. A stalled handler returns as soon as the deadline passes,
	// so the budget costs wall time only, which the parallel cases share.
	const archiveBudget = 2 * time.Second

	tests := []struct {
		name      string
		checksums http.HandlerFunc
		archive   http.HandlerFunc
		timeout   time.Duration
		// callerDeadline has the caller's context expire once the archive handler has sent its
		// first bytes, with DownloadTimeout left at its default.
		callerDeadline bool
		wantPath       string
	}{
		{name: "checksums headers", checksums: blockUntilDone, timeout: 50 * time.Millisecond, wantPath: dlChecksumsPath},
		{name: "checksums body", checksums: blockAfter("abc"), timeout: 50 * time.Millisecond, wantPath: dlChecksumsPath},
		{name: "archive headers", archive: blockUntilDone, timeout: archiveBudget, wantPath: dlArchivePath},
		{name: "archive body", archive: blockAfter(dlContent[:5]), timeout: archiveBudget, wantPath: dlArchivePath},
		{name: "shorter caller deadline", callerDeadline: true, wantPath: dlArchivePath},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			archive := tc.archive
			if tc.callerDeadline {
				expiring := newExpiringContext()
				t.Cleanup(expiring.expire)
				ctx = expiring
				archive = func(w http.ResponseWriter, r *http.Request) {
					_, _ = io.WriteString(w, dlContent[:5])
					w.(http.Flusher).Flush()
					expiring.expire()
					<-r.Context().Done()
				}
			}

			f := newFakeRelease(t, tc.checksums, archive)
			dir := t.TempDir()
			cfg := f.config(t, dir)
			cfg.DownloadTimeout = tc.timeout

			var calls []progressCall
			a, err := cfg.Download(ctx, f.selection(), recordProgress(&calls))
			if a != nil {
				t.Errorf("Download() Archive = %v, want nil", a)
			}
			var timeoutErr *TimeoutError
			if !errors.As(err, &timeoutErr) {
				t.Fatalf("Download() error = %v, want a *TimeoutError", err)
			}
			if want := f.url(tc.wantPath); timeoutErr.URL != want {
				t.Errorf("TimeoutError.URL = %q, want %q", timeoutErr.URL, want)
			}
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Errorf("errors.Is(err, context.DeadlineExceeded) = false, want true")
			}
			assertDirEmpty(t, dir)
			for _, c := range calls {
				if c.done != 5 {
					t.Errorf("progress calls %v, want only the bytes before the stall", calls)
				}
			}
		})
	}
}

func TestDownloadTimeoutIsPerRequest(t *testing.T) {
	t.Parallel()

	// The checksums handler takes half the timeout, and the archive handler answers only once the
	// timeout has passed since the checksums request arrived. The two requests together therefore
	// always outlast the timeout, while each leaves about half of it as headroom.
	const timeout = 2 * time.Second

	started := make(chan time.Time, 1)
	checksums := func(w http.ResponseWriter, r *http.Request) {
		started <- time.Now()
		time.Sleep(timeout / 2)
		serveBody(dlChecksums())(w, r)
	}
	archive := func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(time.Until((<-started).Add(timeout + 50*time.Millisecond)))
		serveBody(dlContent)(w, r)
	}
	f := newFakeRelease(t, checksums, archive)
	dir := t.TempDir()
	cfg := f.config(t, dir)
	cfg.DownloadTimeout = timeout

	a, err := cfg.Download(context.Background(), f.selection())
	if err != nil {
		t.Fatalf("Download() error = %v, want the two requests bounded separately", err)
	}
	assertVerifiedArchive(t, a, dir)
	if err := a.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestDownloadCancellation(t *testing.T) {
	t.Parallel()

	arriveThen := func(next http.HandlerFunc) func(chan<- struct{}) http.HandlerFunc {
		return func(arrived chan<- struct{}) http.HandlerFunc {
			return func(w http.ResponseWriter, r *http.Request) {
				close(arrived)
				next(w, r)
			}
		}
	}

	tests := []struct {
		name      string
		checksums func(chan<- struct{}) http.HandlerFunc
		archive   func(chan<- struct{}) http.HandlerFunc
	}{
		{name: "checksums", checksums: arriveThen(blockUntilDone)},
		{name: "archive headers", archive: arriveThen(blockUntilDone)},
		{
			name: "archive body",
			archive: func(arrived chan<- struct{}) http.HandlerFunc {
				return func(w http.ResponseWriter, r *http.Request) {
					_, _ = io.WriteString(w, dlContent[:5])
					w.(http.Flusher).Flush()
					close(arrived)
					<-r.Context().Done()
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			arrived := make(chan struct{})
			var checksums, archive http.HandlerFunc
			if tc.checksums != nil {
				checksums = tc.checksums(arrived)
			}
			if tc.archive != nil {
				archive = tc.archive(arrived)
			}
			f := newFakeRelease(t, checksums, archive)
			dir := t.TempDir()

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			go func() {
				<-arrived
				cancel()
			}()

			a, err := f.config(t, dir).Download(ctx, f.selection())
			if a != nil {
				t.Errorf("Download() Archive = %v, want nil", a)
			}
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("Download() error = %v, want context.Canceled", err)
			}
			var timeoutErr *TimeoutError
			if errors.As(err, &timeoutErr) {
				t.Errorf("Download() error = %v, want no *TimeoutError", err)
			}
			assertDirEmpty(t, dir)
		})
	}
}

func TestDownloadJoinsRemoveFailure(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("an open file cannot be removed on Windows")
	}

	dir := t.TempDir()
	wrong := strings.Repeat("x", len(dlContent))

	// The handler removes the temporary file itself once it exists, so the removal on the failure
	// path finds nothing to remove.
	archive := func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, wrong[:5])
		w.(http.Flusher).Flush()
		deadline := time.Now().Add(5 * time.Second)
		for {
			entries, err := os.ReadDir(dir)
			if err == nil && len(entries) == 1 {
				_ = os.Remove(filepath.Join(dir, entries[0].Name()))
				break
			}
			if time.Now().After(deadline) {
				break
			}
			time.Sleep(time.Millisecond)
		}
		_, _ = io.WriteString(w, wrong[5:])
	}
	f := newFakeRelease(t, nil, archive)

	a, err := f.config(t, dir).Download(context.Background(), f.selection())
	if a != nil {
		t.Errorf("Download() Archive = %v, want nil", a)
	}
	var mismatch *ChecksumMismatchError
	if !errors.As(err, &mismatch) {
		t.Errorf("Download() error = %v, want a *ChecksumMismatchError", err)
	}
	if !errors.Is(err, os.ErrNotExist) {
		t.Errorf("Download() error = %v, want the removal's os.ErrNotExist joined", err)
	}
	assertDirEmpty(t, dir)
}

func TestDownloadProgress(t *testing.T) {
	t.Parallel()

	n := int64(len(dlContent))

	tests := []struct {
		name    string
		archive http.HandlerFunc
		size    int64
		// wantTotal is the total of every call before the final one.
		wantTotal int64
	}{
		{name: "content length", archive: serveChunkedWithLength(dlContent[:5], dlContent[5:]), wantTotal: n},
		{name: "content length over a size hint", archive: serveChunkedWithLength(dlContent[:5], dlContent[5:]), size: 1000, wantTotal: n},
		{name: "size hint", archive: serveChunked(dlContent[:5], dlContent[5:]), size: n, wantTotal: n},
		{name: "wrong size hint", archive: serveChunked(dlContent[:5], dlContent[5:]), size: 1000, wantTotal: 1000},
		{name: "unknown", archive: serveChunked(dlContent[:5], dlContent[5:]), wantTotal: -1},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := newFakeRelease(t, nil, tc.archive)
			sel := f.selection()
			sel.Archive.Size = tc.size

			var calls []progressCall
			a, err := f.config(t, t.TempDir()).Download(context.Background(), sel, recordProgress(&calls))
			if err != nil {
				t.Fatalf("Download() error = %v", err)
			}
			if err := a.Close(); err != nil {
				t.Fatalf("Close: %v", err)
			}

			if len(calls) < 2 {
				t.Fatalf("progress calls %v, want a streaming call and a final call", calls)
			}
			assertMonotonic(t, calls)
			last := calls[len(calls)-1]
			if last != (progressCall{n, n}) {
				t.Errorf("final progress call = %v, want {%d %d}", last, n, n)
			}
			for _, c := range calls[:len(calls)-1] {
				if c.total != tc.wantTotal {
					t.Errorf("progress calls %v, want total %d before the final call", calls, tc.wantTotal)
					break
				}
			}
			if calls[len(calls)-2].done != n {
				t.Errorf("progress calls %v, want the last streaming call to report every byte", calls)
			}
		})
	}
}

func TestDownloadProgressStopsAtFailure(t *testing.T) {
	t.Parallel()

	wrong := strings.Repeat("x", len(dlContent))

	tests := []struct {
		name    string
		archive http.HandlerFunc
	}{
		{name: "unknown total", archive: serveChunked(wrong[:5], wrong[5:])},
		{name: "content length", archive: serveChunkedWithLength(wrong[:5], wrong[5:])},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := newFakeRelease(t, nil, tc.archive)

			var calls []progressCall
			_, err := f.config(t, t.TempDir()).Download(context.Background(), f.selection(), recordProgress(&calls))
			var mismatch *ChecksumMismatchError
			if !errors.As(err, &mismatch) {
				t.Fatalf("Download() error = %v, want a *ChecksumMismatchError", err)
			}
			if len(calls) == 0 {
				t.Fatalf("no progress calls, want the streamed bytes reported")
			}
			for i := 1; i < len(calls); i++ {
				if calls[i].done <= calls[i-1].done {
					t.Errorf("progress calls %v: a call repeats the count, as the final call would", calls)
				}
			}
			for _, c := range calls {
				if c.total == c.done && c.total != int64(len(wrong)) {
					t.Errorf("progress calls %v: a final call after a failure", calls)
				}
			}
			if last := calls[len(calls)-1]; last.done != int64(len(wrong)) {
				t.Errorf("last progress call = %v, want every streamed byte", last)
			}
		})
	}
}

func TestDownloadLastProgressOptionWins(t *testing.T) {
	t.Parallel()

	f := newFakeRelease(t, nil, nil)

	var first, second []progressCall
	a, err := f.config(t, t.TempDir()).Download(context.Background(), f.selection(),
		recordProgress(&first), recordProgress(&second))
	if err != nil {
		t.Fatalf("Download() error = %v", err)
	}
	if err := a.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if len(first) != 0 || len(second) == 0 {
		t.Errorf("progress calls = %v and %v, want only the last option's callback called", first, second)
	}
}

func TestDownloadEmptyArchiveWithContentLengthZero(t *testing.T) {
	t.Parallel()

	f := newFakeRelease(t, serveBody(sha256Hex("")+"  "+dlArchiveName+"\n"), serveBody(""))
	dir := t.TempDir()
	cfg := f.config(t, dir)
	cfg.MaxDownloadBytes = 10
	sel := f.selection()
	sel.Archive.Size = 11

	var calls []progressCall
	a, err := cfg.Download(context.Background(), sel, recordProgress(&calls))
	if err != nil {
		t.Fatalf("Download() error = %v, want a Content-Length of 0 to override the size hint", err)
	}
	if a.Size() != 0 {
		t.Errorf("Size = %d, want 0", a.Size())
	}
	if err := a.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	assertDirEmpty(t, dir)
	if want := []progressCall{{0, 0}}; len(calls) != 1 || calls[0] != want[0] {
		t.Errorf("progress calls = %v, want %v", calls, want)
	}
}
