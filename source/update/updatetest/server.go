package updatetest

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"runtime"
	"slices"
	"sync"
	"testing"
	"time"
)

// Release is the one release a [Server] publishes.
type Release struct {
	// Owner is the GitHub user or organization that owns the repository.
	Owner string
	// Repo is the name of the repository.
	Repo string
	// Tag is the release's tag, such as "v1.2.3".
	Tag string
	// ArchiveName is the name of the release's archive.
	ArchiveName string
	// Archive is the archive's content, such as [BuildArchive] returns.
	Archive []byte
	// ChecksumsName is the name of the release's checksums file. Empty means
	// [DefaultChecksumsName].
	ChecksumsName string
}

// ArchiveName returns the name the updater looks for when its AssetTemplate is left at its
// default: binary, goos, goarch and ext rendered into
// [go.vipengele.org/update/asset.DefaultTemplate], such as "tool_linux_amd64.tar.gz". The template
// holds no {tag} or {version}, so the name does not depend on the release's tag.
func ArchiveName(binary, goos, goarch, ext string) string {
	return binary + "_" + goos + "_" + goarch + "." + ext
}

// NewRelease returns the release tagged tag of the repository owner/repo for the platform the
// test runs on, as the updater finds it with every template and the extension at their defaults:
// an [ExtTarGz] archive named by [ArchiveName] for [runtime.GOOS] and [runtime.GOARCH], holding
// one executable entry called binary whose content is a shell script printing tag, and the
// checksums file [DefaultChecksumsName]. It fails t when the archive cannot be built.
func NewRelease(t testing.TB, owner, repo, tag, binary string) Release {
	t.Helper()
	archive, err := BuildArchive(ExtTarGz, []Entry{
		{Name: binary, Mode: 0o755, Body: []byte("#!/bin/sh\necho " + tag + "\n")},
	})
	if err != nil {
		t.Fatalf("updatetest: building the archive of %s: %v", tag, err)
	}
	return Release{
		Owner:         owner,
		Repo:          repo,
		Tag:           tag,
		ArchiveName:   ArchiveName(binary, runtime.GOOS, runtime.GOARCH, ExtTarGz),
		Archive:       archive,
		ChecksumsName: DefaultChecksumsName,
	}
}

// Server is a fake GitHub serving one [Release] over HTTP, on these routes, each a [Target]:
//
//   - [TargetLatest], GET /<owner>/<repo>/releases/latest: a 302 Found whose Location is the
//     release's tag page, <server>/<owner>/<repo>/releases/tag/<tag>, as github.com answers. With
//     [APIOnly] it answers 404 Not Found.
//   - [TargetLatestAPI], GET /repos/<owner>/<repo>/releases/latest: the REST API release object,
//     with tag_name, html_url, draft and an assets list giving each asset's name,
//     browser_download_url and size.
//   - [TargetArchive] and [TargetChecksums], GET /<owner>/<repo>/releases/download/<tag>/<name>:
//     the archive's bytes, and the checksums file, one `<sha256 hex>  <name>` line listing the
//     archive.
//
// Owner, repository, tag and asset name each appear in a path escaped as a path segment, as a
// GitHubSource writes them. Both web and API routes are served from the one address, so a
// GitHubSource whose WebURL and APIURL are both the server's reads the release by the redirect,
// with no asset list, and with [APIOnly] by the REST API, with every asset listed.
//
// Any other request answers 404 Not Found and fails the test with [testing.TB.Errorf]. A Server is
// safe for concurrent use.
type Server struct {
	t      testing.TB
	srv    *httptest.Server
	rel    Release
	opts   options
	sums   []byte
	routes map[string]Target

	// blocked and blockedOnce hold one entry per Target from NewServer on and are never
	// written after it, so they are read without mu.
	blocked     map[Target]chan struct{}
	blockedOnce map[Target]*sync.Once

	mu     sync.Mutex
	closed bool
	done   chan struct{}
	counts map[Target]int
}

// NewServer starts a Server publishing rel with the faults and the serving mode opts set, and
// closes it when the test ends. It fails t when rel has no Owner, Repo, Tag or ArchiveName.
func NewServer(t testing.TB, rel Release, opts ...Option) *Server {
	t.Helper()
	if rel.Owner == "" || rel.Repo == "" || rel.Tag == "" || rel.ArchiveName == "" {
		t.Fatalf("updatetest: release %+v needs an Owner, a Repo, a Tag and an ArchiveName", rel)
	}
	if rel.ChecksumsName == "" {
		rel.ChecksumsName = DefaultChecksumsName
	}

	s := &Server{
		t:           t,
		rel:         rel,
		done:        make(chan struct{}),
		counts:      map[Target]int{},
		blocked:     map[Target]chan struct{}{},
		blockedOnce: map[Target]*sync.Once{},
	}
	for _, opt := range opts {
		opt(&s.opts)
	}
	for _, target := range []Target{TargetLatest, TargetLatestAPI, TargetArchive, TargetChecksums} {
		s.blocked[target] = make(chan struct{})
		s.blockedOnce[target] = &sync.Once{}
	}

	listed := rel.Archive
	if s.opts.badChecksum {
		// A longer body has another digest.
		listed = append(slices.Clone(rel.Archive), 0)
	}
	s.sums = []byte(ChecksumLine(rel.ArchiveName, listed) + "\n")

	repo := "/" + url.PathEscape(rel.Owner) + "/" + url.PathEscape(rel.Repo)
	s.routes = map[string]Target{
		repo + "/releases/latest":            TargetLatest,
		"/repos" + repo + "/releases/latest": TargetLatestAPI,
		s.downloadPath(rel.ArchiveName):      TargetArchive,
		s.downloadPath(rel.ChecksumsName):    TargetChecksums,
	}

	s.srv = httptest.NewServer(http.HandlerFunc(s.serve))
	t.Cleanup(s.close)
	return s
}

// WebURL is the base address of the server's web pages, for a GitHubSource's WebURL.
func (s *Server) WebURL() string {
	return s.srv.URL
}

// APIURL is the base address of the server's REST API, for a GitHubSource's APIURL.
func (s *Server) APIURL() string {
	return s.srv.URL
}

// Client returns an HTTP client that reaches the server.
func (s *Server) Client() *http.Client {
	return s.srv.Client()
}

// AssetURL returns the download address of the release's asset called name, as a GitHubSource's
// AssetURL builds it and as the REST API lists it.
func (s *Server) AssetURL(name string) string {
	return s.srv.URL + s.downloadPath(name)
}

// TagURL returns the address of the release's tag page, which the latest-release web page
// redirects to and the REST API gives as html_url.
func (s *Server) TagURL() string {
	return s.srv.URL + (&url.URL{Path: "/" + s.rel.Owner + "/" + s.rel.Repo + "/releases/tag/" + s.rel.Tag}).EscapedPath()
}

// ChecksumsFile returns the checksums file the server serves.
func (s *Server) ChecksumsFile() []byte {
	return slices.Clone(s.sums)
}

// Requests returns how many requests target has received, faulted ones included.
func (s *Server) Requests(target Target) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.counts[target]
}

// Blocked returns a channel closed once a request to target has started waiting on the gate
// [Slow] gave for it. It is never closed for a target with no Slow option, and it is nil, never
// ready, for a value that is not one of the Target constants.
func (s *Server) Blocked(target Target) <-chan struct{} {
	return s.blocked[target]
}

// downloadPath returns the escaped request path of the release's asset called name.
func (s *Server) downloadPath(name string) string {
	return "/" + url.PathEscape(s.rel.Owner) + "/" + url.PathEscape(s.rel.Repo) + "/releases/download/" +
		url.PathEscape(s.rel.Tag) + "/" + url.PathEscape(name)
}

// close releases every request waiting on a gate and shuts the server down. A handler that runs
// after it has begun reports nothing to the test.
func (s *Server) close() {
	s.mu.Lock()
	s.closed = true
	close(s.done)
	s.mu.Unlock()
	s.srv.Close()
}

// serve answers one request on the route its escaped path names.
func (s *Server) serve(w http.ResponseWriter, r *http.Request) {
	target, ok := s.routes[r.URL.EscapedPath()]
	if !ok || r.Method != http.MethodGet {
		s.unexpected(w, r)
		return
	}

	s.mu.Lock()
	s.counts[target]++
	s.mu.Unlock()

	if !s.wait(r, target) {
		// Aborting closes the connection with no response; returning would answer 200 OK.
		panic(http.ErrAbortHandler)
	}

	switch target {
	case TargetLatest:
		s.serveLatest(w)
	case TargetLatestAPI:
		s.serveAPI(w)
	case TargetArchive:
		s.serveAsset(w, r, s.rel.ArchiveName, s.rel.Archive)
	case TargetChecksums:
		s.serveAsset(w, r, s.rel.ChecksumsName, s.sums)
	}
}

// unexpected answers a request on no route with 404 Not Found and fails the test, unless the
// server is closing.
func (s *Server) unexpected(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	if !s.closed {
		s.t.Errorf("updatetest: unexpected request %s %s", r.Method, r.URL.RequestURI())
	}
	s.mu.Unlock()
	http.NotFound(w, r)
}

// wait blocks a request to a [Slow] target until its gate closes, and reports whether the request
// is to be answered: false when its context ended or the server closed first.
func (s *Server) wait(r *http.Request, target Target) bool {
	gate, ok := s.opts.slow[target]
	if !ok {
		return true
	}

	s.blockedOnce[target].Do(func() { close(s.blocked[target]) })

	select {
	case <-gate:
		return true
	case <-r.Context().Done():
		return false
	case <-s.done:
		return false
	}
}

// serveLatest answers the latest-release web page.
func (s *Server) serveLatest(w http.ResponseWriter) {
	if s.opts.apiOnly {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	w.Header().Set("Location", s.TagURL())
	w.WriteHeader(http.StatusFound)
}

// apiRelease is the part of a REST API release object the server writes.
type apiRelease struct {
	TagName string     `json:"tag_name"`
	HTMLURL string     `json:"html_url"`
	Draft   bool       `json:"draft"`
	Assets  []apiAsset `json:"assets"`
}

// apiAsset is the part of a REST API release asset object the server writes.
type apiAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
}

// serveAPI answers the REST API latest release with every asset not [MissingAsset].
func (s *Server) serveAPI(w http.ResponseWriter) {
	rel := apiRelease{TagName: s.rel.Tag, HTMLURL: s.TagURL(), Assets: []apiAsset{}}
	for _, a := range []struct {
		name string
		body []byte
	}{{s.rel.ArchiveName, s.rel.Archive}, {s.rel.ChecksumsName, s.sums}} {
		if s.opts.missing[a.name] {
			continue
		}
		rel.Assets = append(rel.Assets, apiAsset{Name: a.name, BrowserDownloadURL: s.AssetURL(a.name), Size: int64(len(a.body))})
	}

	// The object is a few hundred bytes, under the response buffer, so net/http sets its
	// Content-Length. Encode fails only on a write error, which leaves the client nothing to read.
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(rel)
}

// serveAsset answers the download of the asset called name with body, or 404 Not Found when it is
// a [MissingAsset]. The answer carries the body's Content-Length and no Last-Modified.
func (s *Server) serveAsset(w http.ResponseWriter, r *http.Request, name string, body []byte) {
	if s.opts.missing[name] {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	http.ServeContent(w, r, name, time.Time{}, bytes.NewReader(body))
}
