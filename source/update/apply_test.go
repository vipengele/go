package update

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sync/atomic"
	"testing"

	"go.vipengele.org/update/updatetest"
)

// applyFixture is an executable holding "old" in its own resolved directory, an updatetest server
// publishing release dlTag for the Config's binary, a Config whose executable is that file, whose
// requests go to the server and which trusts the server's signing key, and the located Selection
// of the release's assets.
type applyFixture struct {
	dir string
	exe string
	tmp string
	rel updatetest.Release
	srv *updatetest.Server
	cfg Config
	sel Selection
}

func newApplyFixture(t *testing.T, opts ...updatetest.Option) applyFixture {
	t.Helper()

	dir := resolvedDir(t, t.TempDir())
	exe := filepath.Join(dir, "atk")

	if err := os.WriteFile(exe, []byte("old"), 0o755); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cfg := validConfig(t.TempDir())
	rel := updatetest.NewRelease(t, testOwner, testRepo, dlTag, cfg.Name)
	srv := updatetest.NewServer(t, rel, opts...)
	tmp := t.TempDir()

	cfg.TempDir = tmp
	cfg.HTTPClient = srv.Client()
	cfg.Executable = func() (string, error) { return exe, nil }
	cfg.InContainer = func() bool { return false }
	cfg.TrustedKeys = []string{srv.PublicKey()}

	sel := Selection{
		Tag:       dlTag,
		Located:   true,
		Archive:   Asset{Name: rel.ArchiveName, URL: srv.AssetURL(rel.ArchiveName)},
		Checksums: Asset{Name: rel.ChecksumsName, URL: srv.AssetURL(rel.ChecksumsName)},
		Signature: Asset{Name: rel.ChecksumsName + ".minisig", URL: srv.AssetURL(rel.ChecksumsName + ".minisig")},
	}

	return applyFixture{dir: dir, exe: exe, tmp: tmp, rel: rel, srv: srv, cfg: cfg, sel: sel}
}

// requests returns how many requests the server has received on every route.
func (f applyFixture) requests() int {
	n := 0
	for _, target := range []updatetest.Target{
		updatetest.TargetLatest, updatetest.TargetLatestAPI, updatetest.TargetArchive, updatetest.TargetChecksums,
		updatetest.TargetSignature,
	} {
		n += f.srv.Requests(target)
	}

	return n
}

// assertUntouched fails t unless the executable still holds "old", its directory holds nothing
// else and the download directory is empty.
func (f applyFixture) assertUntouched(t *testing.T) {
	t.Helper()

	if got := readFile(t, f.exe); got != "old" {
		t.Errorf("executable holds %q, want %q", got, "old")
	}

	if got := dirEntries(t, f.dir); !slices.Equal(got, []string{"atk"}) {
		t.Errorf("executable's directory holds %v, want [atk]", got)
	}

	assertDirEmpty(t, f.tmp)
}

func TestApplyInstallsTheRelease(t *testing.T) {
	t.Parallel()

	f := newApplyFixture(t)

	var calls []progressCall
	installed, err := f.cfg.Apply(context.Background(), f.sel, recordProgress(&calls))
	if err != nil {
		t.Fatalf("Apply error = %v", err)
	}

	t.Cleanup(func() { _ = installed.Discard() })

	if installed.Path() != f.exe {
		t.Errorf("Path = %q, want %q", installed.Path(), f.exe)
	}

	if want := f.exe + ".old"; installed.Backup() != want {
		t.Errorf("Backup = %q, want %q", installed.Backup(), want)
	}

	// The binary updatetest.NewRelease packs prints the release's tag.
	want := "#!/bin/sh\necho " + dlTag + "\n"
	if got := readFile(t, f.exe); got != want {
		t.Errorf("executable holds %q, want the release's binary %q", got, want)
	}

	if got := readFile(t, installed.Backup()); got != "old" {
		t.Errorf("backup holds %q, want %q", got, "old")
	}

	if got := dirEntries(t, f.dir); !slices.Equal(got, []string{"atk", "atk.old"}) {
		t.Errorf("executable's directory holds %v, want [atk atk.old]", got)
	}

	assertDirEmpty(t, f.tmp)

	n := int64(len(f.rel.Archive))
	if len(calls) == 0 || calls[len(calls)-1] != (progressCall{n, n}) {
		t.Errorf("progress calls %v, want a final call {%d %d}", calls, n, n)
	}
}

func TestApplyRefusesAManagedInstallBeforeAnyRequest(t *testing.T) {
	t.Parallel()

	f := newApplyFixture(t)
	f.cfg.Detectors = []Detector{
		func(string) (Install, bool) { return Install{Manager: "Tool", Instead: "run `tool upgrade`"}, true },
	}

	installed, err := f.cfg.Apply(context.Background(), f.sel)
	if installed != nil {
		t.Errorf("Apply Installed = %v, want nil", installed)
	}

	var managed *ManagedInstallError
	if !errors.As(err, &managed) {
		t.Fatalf("Apply error = %v, want a *ManagedInstallError", err)
	}

	if managed.Manager != "Tool" {
		t.Errorf("Manager = %q, want %q", managed.Manager, "Tool")
	}

	if got := f.requests(); got != 0 {
		t.Errorf("requests = %d, want 0", got)
	}

	f.assertUntouched(t)
}

func TestApplyRefusesAnUnwritableDirectoryBeforeAnyRequest(t *testing.T) {
	t.Parallel()

	f := newApplyFixture(t)

	s := defaultSteps()
	s.createTemp = func(dir, _ string) (*os.File, error) {
		return nil, &fs.PathError{Op: "open", Path: dir, Err: fs.ErrPermission}
	}

	installed, err := f.cfg.apply(context.Background(), f.sel, s)
	if installed != nil {
		t.Errorf("apply Installed = %v, want nil", installed)
	}

	var perr *PermissionError
	if !errors.As(err, &perr) {
		t.Fatalf("apply error = %v, want a *PermissionError", err)
	}

	if perr.Op != "create" || perr.Path != f.dir {
		t.Errorf("Op, Path = %q, %q, want %q, %q", perr.Op, perr.Path, "create", f.dir)
	}

	if got := f.requests(); got != 0 {
		t.Errorf("requests = %d, want 0", got)
	}

	f.assertUntouched(t)
}

func TestApplyBadChecksum(t *testing.T) {
	t.Parallel()

	f := newApplyFixture(t, updatetest.BadChecksum())

	installed, err := f.cfg.Apply(context.Background(), f.sel)
	if installed != nil {
		t.Errorf("Apply Installed = %v, want nil", installed)
	}

	var mismatch *ChecksumMismatchError
	if !errors.As(err, &mismatch) {
		t.Fatalf("Apply error = %v, want a *ChecksumMismatchError", err)
	}

	f.assertUntouched(t)
}

func TestApplyMissingArchive(t *testing.T) {
	t.Parallel()

	name := updatetest.ArchiveName(validIdentity().Name, runtime.GOOS, runtime.GOARCH, updatetest.ExtTarGz)
	f := newApplyFixture(t, updatetest.MissingAsset(name))

	installed, err := f.cfg.Apply(context.Background(), f.sel)
	if installed != nil {
		t.Errorf("Apply Installed = %v, want nil", installed)
	}

	var nf *AssetNotFoundError
	if !errors.As(err, &nf) {
		t.Fatalf("Apply error = %v, want a *AssetNotFoundError", err)
	}

	if want := (AssetNotFoundError{Tag: dlTag, Name: name}); *nf != want {
		t.Errorf("AssetNotFoundError = %+v, want %+v", *nf, want)
	}

	f.assertUntouched(t)
}

// TestApplyClosesTheArchiveWhenTargetFails covers the one failure between Download and Extract,
// where the archive is Apply's to close.
func TestApplyClosesTheArchiveWhenTargetFails(t *testing.T) {
	t.Parallel()

	f := newApplyFixture(t)
	cause := errors.New("executable vanished")

	// CheckWritable resolves the target first; the second resolution is Target's.
	var resolutions atomic.Int32
	s := defaultSteps()
	s.evalSymlinks = func(path string) (string, error) {
		if resolutions.Add(1) == 2 {
			return "", cause
		}
		return filepath.EvalSymlinks(path)
	}

	installed, err := f.cfg.apply(context.Background(), f.sel, s)
	if installed != nil {
		t.Errorf("apply Installed = %v, want nil", installed)
	}

	if !errors.Is(err, cause) {
		t.Fatalf("apply error = %v, want one matching %v", err, cause)
	}

	if got := f.srv.Requests(updatetest.TargetArchive); got != 1 {
		t.Errorf("archive requests = %d, want 1", got)
	}

	f.assertUntouched(t)
}

func TestApplyReturnsTheExtractError(t *testing.T) {
	t.Parallel()

	f := newApplyFixture(t)
	f.cfg.MaxExtractBytes = 1

	installed, err := f.cfg.Apply(context.Background(), f.sel)
	if installed != nil {
		t.Errorf("Apply Installed = %v, want nil", installed)
	}

	var limit *ExtractLimitError
	if !errors.As(err, &limit) {
		t.Fatalf("Apply error = %v, want a *ExtractLimitError", err)
	}

	if limit.Limit != 1 {
		t.Errorf("ExtractLimitError.Limit = %d, want 1", limit.Limit)
	}

	f.assertUntouched(t)
}
