package updatetest_test

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"go.vipengele.org/update/updatetest"
)

func TestAPIOnlyAnswersLatestWithNotFound(t *testing.T) {
	t.Parallel()
	s := updatetest.NewServer(t, newRelease(t), updatetest.APIOnly())
	resp := mustGet(t, s, s.WebURL()+"/"+testOwner+"/"+testRepo+"/releases/latest")
	if resp.status != http.StatusNotFound || resp.header.Get("Location") != "" {
		t.Errorf("latest = %d Location %q, want 404 with no Location", resp.status, resp.header.Get("Location"))
	}
	if rel := latestAPI(t, s); rel.TagName != testTag || len(rel.Assets) != 2 {
		t.Errorf("API release = %+v, want %s with both assets", rel, testTag)
	}
}

func TestBadChecksumListsAnotherDigest(t *testing.T) {
	t.Parallel()
	rel := newRelease(t)
	s := updatetest.NewServer(t, rel, updatetest.BadChecksum())

	sums := mustGet(t, s, s.AssetURL(rel.ChecksumsName))
	line := strings.TrimSuffix(string(sums.body), "\n")
	digest, name, ok := splitLine(line)
	if !ok || name != rel.ArchiveName {
		t.Fatalf("checksums = %q, want one line for %q", sums.body, rel.ArchiveName)
	}
	if digest == updatetest.Digest(rel.Archive) {
		t.Errorf("checksums digest = %s, the archive's digest", digest)
	}
	if string(s.ChecksumsFile()) != string(sums.body) {
		t.Errorf("ChecksumsFile = %q, want the served %q", s.ChecksumsFile(), sums.body)
	}

	archive := mustGet(t, s, s.AssetURL(rel.ArchiveName))
	if archive.status != http.StatusOK || updatetest.Digest(archive.body) != updatetest.Digest(rel.Archive) {
		t.Errorf("archive = %d, want 200 with the release's archive", archive.status)
	}
}

func TestMissingAsset(t *testing.T) {
	t.Parallel()
	rel := newRelease(t)
	for _, name := range []string{rel.ArchiveName, rel.ChecksumsName} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			s := updatetest.NewServer(t, rel, updatetest.MissingAsset(name))

			if resp := mustGet(t, s, s.AssetURL(name)); resp.status != http.StatusNotFound {
				t.Errorf("GET %s status = %d, want 404", name, resp.status)
			}
			api := latestAPI(t, s)
			if len(api.Assets) != 1 || api.Assets[0].Name == name {
				t.Errorf("API assets = %+v, want only the asset other than %q", api.Assets, name)
			}
			if resp := mustGet(t, s, s.WebURL()+"/"+testOwner+"/"+testRepo+"/releases/latest"); resp.status != http.StatusFound {
				t.Errorf("latest status = %d, want 302", resp.status)
			}
		})
	}
}

func TestMissingArchiveStaysInTheChecksums(t *testing.T) {
	t.Parallel()
	rel := newRelease(t)
	s := updatetest.NewServer(t, rel, updatetest.MissingAsset(rel.ArchiveName))
	sums := mustGet(t, s, s.AssetURL(rel.ChecksumsName))
	if want := updatetest.ChecksumLine(rel.ArchiveName, rel.Archive) + "\n"; string(sums.body) != want {
		t.Errorf("checksums = %q, want %q", sums.body, want)
	}
}

// slowURL returns the address of target on s.
func slowURL(s *updatetest.Server, rel updatetest.Release, target updatetest.Target) string {
	switch target {
	case updatetest.TargetLatest:
		return s.WebURL() + "/" + testOwner + "/" + testRepo + "/releases/latest"
	case updatetest.TargetLatestAPI:
		return s.APIURL() + "/repos/" + testOwner + "/" + testRepo + "/releases/latest"
	case updatetest.TargetArchive:
		return s.AssetURL(rel.ArchiveName)
	default:
		return s.AssetURL(rel.ChecksumsName)
	}
}

// result is the outcome of a request made on another goroutine.
type result struct {
	resp response
	err  error
}

// getAsync requests rawURL on its own goroutine and delivers the outcome on the returned channel.
func getAsync(ctx context.Context, s *updatetest.Server, rawURL string) <-chan result {
	ch := make(chan result, 1)
	go func() {
		resp, err := get(ctx, s, rawURL)
		ch <- result{resp, err}
	}()
	return ch
}

// slowTargets returns every Target.
func slowTargets() []updatetest.Target {
	return []updatetest.Target{
		updatetest.TargetLatest, updatetest.TargetLatestAPI, updatetest.TargetArchive, updatetest.TargetChecksums,
	}
}

func TestSlowAnswersOnceTheGateCloses(t *testing.T) {
	t.Parallel()
	for _, target := range slowTargets() {
		t.Run(target.String(), func(t *testing.T) {
			t.Parallel()
			rel := newRelease(t)
			gate := make(chan struct{})
			s := updatetest.NewServer(t, rel, updatetest.Slow(target, gate))

			done := getAsync(t.Context(), s, slowURL(s, rel, target))
			<-s.Blocked(target)
			select {
			case r := <-done:
				t.Fatalf("request answered %+v while its gate is open", r)
			default:
			}
			close(gate)
			r := <-done
			if r.err != nil {
				t.Fatalf("GET: %v", r.err)
			}
			if r.resp.status != http.StatusOK && r.resp.status != http.StatusFound {
				t.Errorf("status = %d, want the route's answer", r.resp.status)
			}
			if got := s.Requests(target); got != 1 {
				t.Errorf("Requests(%v) = %d, want 1", target, got)
			}
		})
	}
}

func TestSlowLeavesOtherTargetsAlone(t *testing.T) {
	t.Parallel()
	rel := newRelease(t)
	s := updatetest.NewServer(t, rel, updatetest.Slow(updatetest.TargetArchive, make(chan struct{})))
	if resp := mustGet(t, s, s.AssetURL(rel.ChecksumsName)); resp.status != http.StatusOK {
		t.Errorf("checksums status = %d, want 200", resp.status)
	}
}

func TestSlowReleasesARequestWhoseContextEnds(t *testing.T) {
	t.Parallel()
	for _, target := range slowTargets() {
		t.Run(target.String(), func(t *testing.T) {
			t.Parallel()
			rel := newRelease(t)
			s := updatetest.NewServer(t, rel, updatetest.Slow(target, make(chan struct{})))

			ctx, cancel := context.WithCancel(t.Context())
			done := getAsync(ctx, s, slowURL(s, rel, target))
			<-s.Blocked(target)
			cancel()
			if r := <-done; !errors.Is(r.err, context.Canceled) {
				t.Errorf("GET error = %v, want context.Canceled", r.err)
			}
		})
	}
}

func TestSlowIsReleasedByTheServerCleanup(t *testing.T) {
	t.Parallel()
	var done <-chan result
	t.Run("server", func(t *testing.T) {
		rel := newRelease(t)
		s := updatetest.NewServer(t, rel, updatetest.Slow(updatetest.TargetArchive, make(chan struct{})))
		// The request outlives the sub-test, so it does not use the sub-test's context.
		done = getAsync(context.Background(), s, s.AssetURL(rel.ArchiveName))
		<-s.Blocked(updatetest.TargetArchive)
	})
	if r := <-done; r.err == nil {
		t.Errorf("request answered %d after the server closed, want no answer", r.resp.status)
	}
}

func TestBlockedIsNeverClosedWithoutSlow(t *testing.T) {
	t.Parallel()
	rel := newRelease(t)
	s := updatetest.NewServer(t, rel)
	mustGet(t, s, s.AssetURL(rel.ArchiveName))
	select {
	case <-s.Blocked(updatetest.TargetArchive):
		t.Error("Blocked(TargetArchive) closed with no Slow option")
	default:
	}
}

func TestTargetString(t *testing.T) {
	t.Parallel()
	for target, want := range map[updatetest.Target]string{
		updatetest.TargetLatest:    "TargetLatest",
		updatetest.TargetLatestAPI: "TargetLatestAPI",
		updatetest.TargetArchive:   "TargetArchive",
		updatetest.TargetChecksums: "TargetChecksums",
		updatetest.Target(0):       "Target(0)",
	} {
		if got := target.String(); got != want {
			t.Errorf("String() = %q, want %q", got, want)
		}
	}
}
