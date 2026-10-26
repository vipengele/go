package updatetest_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"runtime"
	"strings"
	"sync"
	"testing"

	"go.vipengele.org/update/asset"
	"go.vipengele.org/update/updatetest"
)

const (
	testOwner  = "vipengele"
	testRepo   = "tool"
	testTag    = "v1.2.3"
	testBinary = "tool"
)

// recorderTB passes every call to the test it embeds except Errorf, which it records instead of
// failing that test.
type recorderTB struct {
	testing.TB
	mu     sync.Mutex
	errors []string
}

func (r *recorderTB) Errorf(format string, args ...any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.errors = append(r.errors, fmt.Sprintf(format, args...))
}

func (r *recorderTB) recorded() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.errors...)
}

// response is what a request to a Server answered.
type response struct {
	status int
	header http.Header
	body   []byte
}

// get requests rawURL through the server's client without following a redirect.
func get(ctx context.Context, s *updatetest.Server, rawURL string) (response, error) {
	client := *s.Client()
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return response{}, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return response{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return response{}, err
	}
	return response{status: resp.StatusCode, header: resp.Header, body: body}, nil
}

// mustGet is get with the test's context, failing t on an error.
func mustGet(t *testing.T, s *updatetest.Server, rawURL string) response {
	t.Helper()
	resp, err := get(t.Context(), s, rawURL)
	if err != nil {
		t.Fatalf("GET %s: %v", rawURL, err)
	}
	return resp
}

// apiRelease is the REST API release object as GitHubSource decodes it.
type apiRelease struct {
	TagName string `json:"tag_name"`
	HTMLURL string `json:"html_url"`
	Draft   bool   `json:"draft"`
	Assets  []struct {
		Name               string `json:"name"`
		BrowserDownloadURL string `json:"browser_download_url"`
		Size               int64  `json:"size"`
	} `json:"assets"`
}

// latestAPI requests and decodes the server's REST API latest release.
func latestAPI(t *testing.T, s *updatetest.Server) apiRelease {
	t.Helper()
	resp := mustGet(t, s, s.APIURL()+"/repos/"+testOwner+"/"+testRepo+"/releases/latest")
	if resp.status != http.StatusOK {
		t.Fatalf("API latest status = %d, want 200", resp.status)
	}
	if ct := resp.header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("API latest Content-Type = %q, want application/json", ct)
	}
	var rel apiRelease
	if err := json.Unmarshal(resp.body, &rel); err != nil {
		t.Fatalf("decoding API latest %s: %v", resp.body, err)
	}
	return rel
}

func newRelease(t *testing.T) updatetest.Release {
	t.Helper()
	return updatetest.NewRelease(t, testOwner, testRepo, testTag, testBinary)
}

func TestNewReleaseIsTheDefaultRelease(t *testing.T) {
	t.Parallel()
	rel := newRelease(t)
	want, err := asset.Render(asset.DefaultTemplate, asset.Fields{
		Name: testBinary, Tag: testTag, OS: runtime.GOOS, Arch: runtime.GOARCH, Ext: asset.DefaultExt,
	})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if rel.Owner != testOwner || rel.Repo != testRepo || rel.Tag != testTag {
		t.Errorf("release = %s/%s@%s, want %s/%s@%s", rel.Owner, rel.Repo, rel.Tag, testOwner, testRepo, testTag)
	}
	if rel.ArchiveName != want {
		t.Errorf("ArchiveName = %q, want %q", rel.ArchiveName, want)
	}
	if rel.ChecksumsName != updatetest.DefaultChecksumsName {
		t.Errorf("ChecksumsName = %q, want %q", rel.ChecksumsName, updatetest.DefaultChecksumsName)
	}
	got := readTarGz(t, rel.Archive)
	if len(got) != 1 || got[0].Name != testBinary || got[0].Mode != 0o755 || !strings.Contains(string(got[0].Body), testTag) {
		t.Errorf("archive entries = %+v, want one executable %q printing %s", got, testBinary, testTag)
	}
}

func TestArchiveNameRendersTheDefaultTemplate(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ goos, goarch, ext string }{
		{"linux", "amd64", updatetest.ExtTarGz},
		{"windows", "arm64", updatetest.ExtZip},
	} {
		want, err := asset.Render(asset.DefaultTemplate, asset.Fields{Name: testBinary, Tag: testTag, OS: tc.goos, Arch: tc.goarch, Ext: tc.ext})
		if err != nil {
			t.Fatalf("Render: %v", err)
		}
		if got := updatetest.ArchiveName(testBinary, tc.goos, tc.goarch, tc.ext); got != want {
			t.Errorf("ArchiveName(%s, %s, %s) = %q, want %q", tc.goos, tc.goarch, tc.ext, got, want)
		}
	}
}

func TestServerRedirectsLatestToTheTagPage(t *testing.T) {
	t.Parallel()
	s := updatetest.NewServer(t, newRelease(t))
	resp := mustGet(t, s, s.WebURL()+"/"+testOwner+"/"+testRepo+"/releases/latest")
	if resp.status != http.StatusFound {
		t.Fatalf("latest status = %d, want 302", resp.status)
	}
	loc := resp.header.Get("Location")
	if want := "/" + testOwner + "/" + testRepo + "/releases/tag/" + testTag; !strings.HasSuffix(loc, want) {
		t.Errorf("Location = %q, want it to end in %q", loc, want)
	}
	if loc != s.TagURL() {
		t.Errorf("Location = %q, want TagURL %q", loc, s.TagURL())
	}
	if got := s.Requests(updatetest.TargetLatest); got != 1 {
		t.Errorf("Requests(TargetLatest) = %d, want 1", got)
	}
}

func TestServerListsEveryAssetInTheAPI(t *testing.T) {
	t.Parallel()
	rel := newRelease(t)
	s := updatetest.NewServer(t, rel)
	got := latestAPI(t, s)
	if got.TagName != testTag || got.HTMLURL != s.TagURL() || got.Draft {
		t.Errorf("release = {%q %q draft=%v}, want {%q %q draft=false}", got.TagName, got.HTMLURL, got.Draft, testTag, s.TagURL())
	}
	want := map[string]int64{
		rel.ArchiveName:   int64(len(rel.Archive)),
		rel.ChecksumsName: int64(len(s.ChecksumsFile())),
	}
	if len(got.Assets) != len(want) {
		t.Fatalf("assets = %+v, want %d", got.Assets, len(want))
	}
	for _, a := range got.Assets {
		size, ok := want[a.Name]
		if !ok || a.Size != size || a.BrowserDownloadURL != s.AssetURL(a.Name) {
			t.Errorf("asset %+v, want size %d at %s", a, size, s.AssetURL(a.Name))
		}
	}
	if got := s.Requests(updatetest.TargetLatestAPI); got != 1 {
		t.Errorf("Requests(TargetLatestAPI) = %d, want 1", got)
	}
}

func TestServerServesTheAssets(t *testing.T) {
	t.Parallel()
	rel := newRelease(t)
	s := updatetest.NewServer(t, rel)

	archiveURL := s.WebURL() + "/" + testOwner + "/" + testRepo + "/releases/download/" + testTag + "/" + rel.ArchiveName
	if archiveURL != s.AssetURL(rel.ArchiveName) {
		t.Errorf("AssetURL = %q, want %q", s.AssetURL(rel.ArchiveName), archiveURL)
	}
	archive := mustGet(t, s, archiveURL)
	if archive.status != http.StatusOK || !bytes.Equal(archive.body, rel.Archive) {
		t.Errorf("archive = %d with %d bytes, want 200 with the %d archive bytes", archive.status, len(archive.body), len(rel.Archive))
	}

	sums := mustGet(t, s, s.AssetURL(updatetest.DefaultChecksumsName))
	want := updatetest.ChecksumLine(rel.ArchiveName, rel.Archive) + "\n"
	if sums.status != http.StatusOK || string(sums.body) != want {
		t.Errorf("checksums = %d %q, want 200 %q", sums.status, sums.body, want)
	}

	if a, c := s.Requests(updatetest.TargetArchive), s.Requests(updatetest.TargetChecksums); a != 1 || c != 1 {
		t.Errorf("Requests(TargetArchive, TargetChecksums) = %d, %d, want 1, 1", a, c)
	}
}

func TestServerEscapesEachPathSegment(t *testing.T) {
	t.Parallel()
	rel := newRelease(t)
	rel.Tag = "source/tool/v1.2.3"
	rel.ArchiveName = "tool v1,2.tar.gz"
	s := updatetest.NewServer(t, rel)

	escaped := s.WebURL() + "/" + testOwner + "/" + testRepo + "/releases/download/source%2Ftool%2Fv1.2.3/tool%20v1%2C2.tar.gz"
	if got := s.AssetURL(rel.ArchiveName); got != escaped {
		t.Errorf("AssetURL = %q, want %q", got, escaped)
	}
	if resp := mustGet(t, s, escaped); resp.status != http.StatusOK || !bytes.Equal(resp.body, rel.Archive) {
		t.Errorf("archive status = %d, want 200 with the archive", resp.status)
	}
	if want := "/" + testOwner + "/" + testRepo + "/releases/tag/source/tool/v1.2.3"; !strings.HasSuffix(s.TagURL(), want) {
		t.Errorf("TagURL = %q, want it to end in %q", s.TagURL(), want)
	}
}

func TestServerUsesTheReleaseChecksumsName(t *testing.T) {
	t.Parallel()
	rel := newRelease(t)
	rel.ChecksumsName = "tool_v1.2.3_SHA256SUMS"
	s := updatetest.NewServer(t, rel)
	if resp := mustGet(t, s, s.AssetURL(rel.ChecksumsName)); resp.status != http.StatusOK {
		t.Errorf("checksums status = %d, want 200", resp.status)
	}
}

func TestServerDefaultsTheChecksumsName(t *testing.T) {
	t.Parallel()
	rel := newRelease(t)
	rel.ChecksumsName = ""
	s := updatetest.NewServer(t, rel)
	if resp := mustGet(t, s, s.AssetURL(updatetest.DefaultChecksumsName)); resp.status != http.StatusOK {
		t.Errorf("checksums status = %d, want 200", resp.status)
	}
}

func TestServerFailsTheTestOnAnUnexpectedRoute(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, method, path string
	}{
		{"unknown path", http.MethodGet, "/" + testOwner + "/" + testRepo + "/releases"},
		{"other repository", http.MethodGet, "/" + testOwner + "/other/releases/latest"},
		{"other tag", http.MethodGet, "/" + testOwner + "/" + testRepo + "/releases/download/v9.9.9/" + updatetest.DefaultChecksumsName},
		{"tag page", http.MethodGet, "/" + testOwner + "/" + testRepo + "/releases/tag/" + testTag},
		{"POST to a route", http.MethodPost, "/" + testOwner + "/" + testRepo + "/releases/latest"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			rec := &recorderTB{TB: t}
			s := updatetest.NewServer(rec, newRelease(t))
			req, err := http.NewRequestWithContext(t.Context(), tc.method, s.WebURL()+tc.path, nil)
			if err != nil {
				t.Fatalf("NewRequest: %v", err)
			}
			resp, err := s.Client().Do(req)
			if err != nil {
				t.Fatalf("%s %s: %v", tc.method, tc.path, err)
			}
			_ = resp.Body.Close()
			if resp.StatusCode != http.StatusNotFound {
				t.Errorf("status = %d, want 404", resp.StatusCode)
			}
			errs := rec.recorded()
			if len(errs) != 1 || !strings.Contains(errs[0], tc.method+" "+tc.path) {
				t.Errorf("reported %q, want one error naming %s %s", errs, tc.method, tc.path)
			}
		})
	}
}

func TestServerReportsNothingForExpectedRoutes(t *testing.T) {
	t.Parallel()
	rec := &recorderTB{TB: t}
	rel := newRelease(t)
	s := updatetest.NewServer(rec, rel)
	for _, u := range []string{
		s.WebURL() + "/" + testOwner + "/" + testRepo + "/releases/latest",
		s.APIURL() + "/repos/" + testOwner + "/" + testRepo + "/releases/latest",
		s.AssetURL(rel.ArchiveName),
		s.AssetURL(rel.ChecksumsName),
	} {
		mustGet(t, s, u)
	}
	if errs := rec.recorded(); len(errs) != 0 {
		t.Errorf("reported %q, want nothing", errs)
	}
}

func TestNewReleaseFailsOnAnUnbuildableArchive(t *testing.T) {
	t.Parallel()
	f := &fatalTB{TB: t}
	runFatal(f, func(tb testing.TB) { updatetest.NewRelease(tb, testOwner, testRepo, testTag, "to\x00ol") })
	if !f.failed || !strings.Contains(f.message, testTag) {
		t.Errorf("NewRelease with a NUL in the binary name: failed = %v with %q, want a failure naming %s", f.failed, f.message, testTag)
	}
}

func TestNewServerRejectsAnIncompleteRelease(t *testing.T) {
	t.Parallel()
	f := &fatalTB{TB: t}
	runFatal(f, func(tb testing.TB) { updatetest.NewServer(tb, updatetest.Release{Owner: testOwner, Repo: testRepo}) })
	if !f.failed {
		t.Error("NewServer accepted a release with no Tag or ArchiveName")
	}
}

// fatalTB records a Fatalf and its message, ending its goroutine as testing.T does, instead of
// failing the test it embeds.
type fatalTB struct {
	testing.TB
	failed  bool
	message string
}

func (f *fatalTB) Helper() {}

func (f *fatalTB) Fatalf(format string, args ...any) {
	f.failed = true
	f.message = fmt.Sprintf(format, args...)
	runtime.Goexit()
}

// runFatal calls fn with f on its own goroutine, so a Fatalf stops fn as it stops a test, and
// waits for it to finish.
func runFatal(f *fatalTB, fn func(tb testing.TB)) {
	done := make(chan struct{})
	go func() {
		defer close(done)
		fn(f)
	}()
	<-done
}
