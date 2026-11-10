package update

import (
	"context"
	"crypto/rand"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"go.vipengele.org/commons/validate"
	"go.vipengele.org/update/asset"
	"go.vipengele.org/update/internal/minisigntest"
	"go.vipengele.org/update/updatetest"
)

// applyFixture is an executable holding "old" in its own resolved directory, an updatetest server
// publishing release dlTag for the Config's binary, a Config whose executable is that file, whose
// requests go to the server, which trusts the server's signing key and whose StateDir resolves
// under the home directory home, and the located Selection of the release's assets.
type applyFixture struct {
	dir  string
	home string
	exe  string
	tmp  string
	rel  updatetest.Release
	srv  *updatetest.Server
	cfg  Config
	sel  Selection
}

func newApplyFixture(t *testing.T, opts ...updatetest.Option) applyFixture {
	t.Helper()

	dir := resolvedDir(t, t.TempDir())
	exe := filepath.Join(dir, "atk")

	if err := os.WriteFile(exe, []byte("old"), 0o755); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	home := t.TempDir()
	cfg := validConfig(home)
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

	return applyFixture{dir: dir, home: home, exe: exe, tmp: tmp, rel: rel, srv: srv, cfg: cfg, sel: sel}
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

// stateDir returns the fixture Config's resolved StateDir, failing t unless it lies under home.
func (f applyFixture) stateDir(t *testing.T) string {
	t.Helper()

	r, err := f.cfg.resolve()
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}

	if !strings.HasPrefix(r.StateDir, f.home+string(filepath.Separator)) {
		t.Fatalf("StateDir = %q, want one under the home directory %q", r.StateDir, f.home)
	}

	return r.StateDir
}

// steps returns the real fsSteps with run answering "version --json" as the release's binary does
// for the Config's platform, without running it. A test that runs the staged binary for real does
// not run in parallel (see TestApplyRunsTheStagedBinary), so every parallel test that reaches the
// preflight goes through these steps.
func (f applyFixture) steps(t *testing.T) fsSteps {
	t.Helper()

	r, err := f.cfg.resolve()
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}

	return answering(versionJSON(f.sel.Tag, r.GOOS, r.GOARCH), "", nil, nil)
}

// apply runs Apply with opts through f's steps and fails t unless it installs the release,
// settling the update when t ends.
func (f applyFixture) apply(t *testing.T, opts ...ApplyOption) {
	t.Helper()

	installed, err := f.cfg.apply(context.Background(), f.sel, f.steps(t), opts...)
	if err != nil {
		t.Fatalf("Apply error = %v, want nil", err)
	}

	if installed == nil {
		t.Fatal("Apply Installed = nil, want the installed release")
	}

	t.Cleanup(func() { _ = installed.Discard() })

	if got, want := readFile(t, f.exe), string(updatetest.ReleaseExecutable(t, dlTag)); got != want {
		t.Errorf("executable holds %.64q, want the release's binary %.64q", got, want)
	}
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

	r, err := f.cfg.resolve()
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}

	var runs [][]string
	s := answering(versionJSON(dlTag, r.GOOS, r.GOARCH), "", nil, &runs)

	var calls []progressCall
	installed, err := f.cfg.apply(context.Background(), f.sel, s, WithDownloadOptions(recordProgress(&calls)))
	if err != nil {
		t.Fatalf("apply error = %v", err)
	}

	t.Cleanup(func() { _ = installed.Discard() })

	if len(runs) != 1 || !slices.Equal(runs[0][1:], []string{"version", "--json"}) {
		t.Errorf("runs = %q, want one run with the arguments version --json", runs)
	}

	if installed.Path() != f.exe {
		t.Errorf("Path = %q, want %q", installed.Path(), f.exe)
	}

	if want := f.exe + ".old"; installed.Backup() != want {
		t.Errorf("Backup = %q, want %q", installed.Backup(), want)
	}

	want := string(updatetest.ReleaseExecutable(t, dlTag))
	if got := readFile(t, f.exe); got != want {
		t.Errorf("executable holds %.64q, want the release's binary %.64q", got, want)
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

func TestApplyRefusesAReleaseSignedByAnUntrustedKey(t *testing.T) {
	t.Parallel()

	f := newApplyFixture(t)
	other, err := minisigntest.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	f.cfg.TrustedKeys = []string{other.PublicKey()}

	installed, err := f.cfg.Apply(context.Background(), f.sel)
	if installed != nil {
		t.Errorf("Apply Installed = %v, want nil", installed)
	}

	var unknown *UnknownKeyError
	if !errors.As(err, &unknown) {
		t.Fatalf("Apply error = %v, want a *UnknownKeyError", err)
	}

	if n := f.srv.Requests(updatetest.TargetArchive); n != 0 {
		t.Errorf("archive requests = %d, want 0", n)
	}

	f.assertUntouched(t)
}

func TestApplyMissingArchive(t *testing.T) {
	t.Parallel()

	name := updatetest.ArchiveName(validIdentity().Name, runtime.GOOS, runtime.GOARCH, asset.DefaultExtFor(runtime.GOOS))
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

func TestApplyRefusesAnInvalidConfigBeforeAnyRequest(t *testing.T) {
	t.Parallel()

	f := newApplyFixture(t)
	f.cfg.TrustedKeys = nil

	installed, err := f.cfg.Apply(context.Background(), f.sel)
	if installed != nil {
		t.Errorf("Apply Installed = %v, want nil", installed)
	}

	if !errors.Is(err, validate.ErrInvalid) {
		t.Fatalf("Apply error = %v, want one matching validate.ErrInvalid", err)
	}

	if got := f.requests(); got != 0 {
		t.Errorf("requests = %d, want 0", got)
	}

	f.assertUntouched(t)
}

func TestApplyReturnsTheInstallErrorAndRecordsNothing(t *testing.T) {
	t.Parallel()

	f := newApplyFixture(t)
	store := &fakeStore{loads: []loadResult{{}}}
	f.cfg.StateStore = store
	cause := errors.New("rename refused")

	s := f.steps(t)
	s.rename = func(string, string) error { return cause }

	installed, err := f.cfg.apply(context.Background(), f.sel, s)
	if installed != nil {
		t.Errorf("apply Installed = %v, want nil", installed)
	}

	if !errors.Is(err, cause) {
		t.Fatalf("apply error = %v, want one matching %v", err, cause)
	}

	if saves := store.recordedSaves(); len(saves) != 0 {
		t.Errorf("saves = %+v, want none", saves)
	}

	f.assertUntouched(t)
}

// saveFailingStore is a StateStore that loads the zero State and fails every Save, counting them.
type saveFailingStore struct {
	saves atomic.Int32
}

func (*saveFailingStore) Load(context.Context) (State, error) { return State{}, nil }

func (s *saveFailingStore) Save(context.Context, State) error {
	s.saves.Add(1)
	return errors.New("saveFailingStore: cannot save")
}

func TestApplyRefusesADowngradeBeforeAnyRequest(t *testing.T) {
	t.Parallel()

	f := newApplyFixture(t)
	f.cfg.Version = "v2.0.0"

	installed, err := f.cfg.Apply(context.Background(), f.sel)
	if installed != nil {
		t.Errorf("Apply Installed = %v, want nil", installed)
	}

	var down *DowngradeError
	if !errors.As(err, &down) {
		t.Fatalf("Apply error = %v, want a *DowngradeError", err)
	}

	if want := (DowngradeError{Name: "atk", Version: dlTag, Floor: "v2.0.0"}); *down != want {
		t.Errorf("DowngradeError = %+v, want %+v", *down, want)
	}

	if got := f.requests(); got != 0 {
		t.Errorf("requests = %d, want 0", got)
	}

	f.assertUntouched(t)

	// Neither the state file nor the lock file is written, so the StateDir is never created.
	if _, err := os.Stat(f.stateDir(t)); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("Stat(StateDir) error = %v, want one matching fs.ErrNotExist", err)
	}
}

func TestApplyRefusesAnUnreadableStoreBeforeAnyRequest(t *testing.T) {
	t.Parallel()

	f := newApplyFixture(t)
	loadErr := errors.New("unreadable")
	f.cfg.StateStore = &fakeStore{loads: []loadResult{{err: loadErr}}}

	installed, err := f.cfg.Apply(context.Background(), f.sel)
	if installed != nil {
		t.Errorf("Apply Installed = %v, want nil", installed)
	}

	if !errors.Is(err, loadErr) {
		t.Fatalf("Apply error = %v, want the store's error", err)
	}

	if got := f.requests(); got != 0 {
		t.Errorf("requests = %d, want 0", got)
	}

	f.assertUntouched(t)
}

func TestApplyAllowDowngradeInstallsAndKeepsTheWatermark(t *testing.T) {
	t.Parallel()

	f := newApplyFixture(t)
	f.cfg.Version = "v1.5.0"
	store := &fileStore{dir: f.stateDir(t)}

	if err := store.Save(context.Background(), State{HighestApplied: "v2.0.0"}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	f.apply(t, AllowDowngrade())

	st, err := store.Load(context.Background())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if st.HighestApplied != "v2.0.0" {
		t.Errorf("HighestApplied = %q, want %q", st.HighestApplied, "v2.0.0")
	}
}

func TestApplyAllowDowngradeSkipsTheGuardsLoad(t *testing.T) {
	t.Parallel()

	f := newApplyFixture(t)
	f.cfg.Version = "v2.0.0"
	store := &fakeStore{loads: []loadResult{{err: errors.New("unreadable")}}}
	f.cfg.StateStore = store

	f.apply(t, AllowDowngrade())

	// The one load is the record's, whose failure leaves the State unsaved.
	if store.loadCalls != 1 {
		t.Errorf("Load calls = %d, want 1", store.loadCalls)
	}

	if saves := store.recordedSaves(); len(saves) != 0 {
		t.Errorf("saves = %+v, want none", saves)
	}
}

func TestApplyRaisesTheWatermarkOnAnUpgrade(t *testing.T) {
	t.Parallel()

	f := newApplyFixture(t)
	f.cfg.Version = "v1.0.0"
	store := &fileStore{dir: f.stateDir(t)}
	before := State{
		LastAttempt:    time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
		LatestVersion:  "v1.3.0-beta.1",
		LatestChannel:  Beta,
		HighestApplied: "v1.0.0",
	}

	if err := store.Save(context.Background(), before); err != nil {
		t.Fatalf("Save: %v", err)
	}

	f.apply(t)

	st, err := store.Load(context.Background())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	want := before
	want.HighestApplied = dlTag
	if !st.LastAttempt.Equal(want.LastAttempt) || st.LatestVersion != want.LatestVersion ||
		st.LatestChannel != want.LatestChannel || st.HighestApplied != want.HighestApplied {
		t.Errorf("State = %+v, want %+v", st, want)
	}
}

func TestApplyRecordsTheFirstWatermark(t *testing.T) {
	t.Parallel()

	f := newApplyFixture(t)
	store := &fakeStore{}
	f.cfg.StateStore = store

	f.apply(t)

	if saves := store.recordedSaves(); !slices.Equal(saves, []State{{HighestApplied: dlTag}}) {
		t.Errorf("saves = %+v, want one State with HighestApplied %q", saves, dlTag)
	}
}

func TestApplyLeavesAWatermarkAtTheAppliedTag(t *testing.T) {
	t.Parallel()

	f := newApplyFixture(t)
	store := &fakeStore{loads: []loadResult{{st: State{HighestApplied: dlTag}}}}
	f.cfg.StateStore = store

	f.apply(t)

	if saves := store.recordedSaves(); len(saves) != 0 {
		t.Errorf("saves = %+v, want none", saves)
	}
}

func TestApplyIgnoresAFailedSaveOfTheRecord(t *testing.T) {
	t.Parallel()

	f := newApplyFixture(t)
	store := &saveFailingStore{}
	f.cfg.StateStore = store

	f.apply(t)

	if got := store.saves.Load(); got != 1 {
		t.Errorf("Save calls = %d, want 1", got)
	}
}

func TestApplyIgnoresAFailedLoadOfTheRecord(t *testing.T) {
	t.Parallel()

	f := newApplyFixture(t)
	store := &fakeStore{loads: []loadResult{{}, {err: errors.New("unreadable")}}}
	f.cfg.StateStore = store

	f.apply(t)

	if store.loadCalls != 2 {
		t.Errorf("Load calls = %d, want 2", store.loadCalls)
	}

	if saves := store.recordedSaves(); len(saves) != 0 {
		t.Errorf("saves = %+v, want none", saves)
	}
}

func TestApplyIgnoresAFailedLockOfTheRecord(t *testing.T) {
	t.Parallel()

	f := newApplyFixture(t)
	store := &fakeStore{}
	f.cfg.StateStore = store
	f.cfg.Locker = failingLocker{}

	f.apply(t)

	// The one load is the guard's: the record stops at the lock.
	if store.loadCalls != 1 {
		t.Errorf("Load calls = %d, want 1", store.loadCalls)
	}

	if saves := store.recordedSaves(); len(saves) != 0 {
		t.Errorf("saves = %+v, want none", saves)
	}
}

// TestApplyRunsTheStagedBinary runs the release's binary for real in the preflight. It does not run
// in parallel: a process another test forks inherits every descriptor open at that moment until it
// execs, so a staged file some parallel test still holds open for writing would fail this exec
// with ETXTBSY.
func TestApplyRunsTheStagedBinary(t *testing.T) {
	f := newApplyFixture(t)

	var seen []StagedBinary
	f.cfg.Preflight = []Preflight{
		func(_ context.Context, b StagedBinary) error {
			seen = append(seen, b)
			return nil
		},
	}

	installed, err := f.cfg.Apply(context.Background(), f.sel)
	if err != nil {
		t.Fatalf("Apply error = %v", err)
	}

	t.Cleanup(func() { _ = installed.Discard() })

	if len(seen) != 1 {
		t.Fatalf("Preflight hook calls = %d, want 1", len(seen))
	}

	want := StagedBinary{Path: seen[0].Path, Version: dlTag, Schema: 1, OS: runtime.GOOS, Arch: runtime.GOARCH}
	if seen[0] != want {
		t.Errorf("StagedBinary = %+v, want %+v", seen[0], want)
	}

	if filepath.Dir(seen[0].Path) != f.dir {
		t.Errorf("StagedBinary.Path = %q, want one in %q", seen[0].Path, f.dir)
	}

	if got, want := readFile(t, f.exe), string(updatetest.ReleaseExecutable(t, dlTag)); got != want {
		t.Errorf("executable holds %.64q, want the release's binary %.64q", got, want)
	}
}

func TestApplyRefusesABinaryThePreflightRefuses(t *testing.T) {
	t.Parallel()

	veto := errors.New("database schema too new")

	tests := []struct {
		name  string
		setup func(f *applyFixture, s *fsSteps, goos, goarch string)
		cause error
	}{
		{
			name: "wrong version",
			setup: func(_ *applyFixture, s *fsSteps, goos, goarch string) {
				s.run = answering(versionJSON("v9.9.9", goos, goarch), "", nil, nil).run
			},
		},
		{
			name: "hook veto",
			setup: func(f *applyFixture, s *fsSteps, goos, goarch string) {
				s.run = answering(versionJSON(dlTag, goos, goarch), "", nil, nil).run
				f.cfg.Preflight = []Preflight{func(context.Context, StagedBinary) error { return veto }}
			},
			cause: veto,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			f := newApplyFixture(t)
			store := &fakeStore{loads: []loadResult{{}}}
			f.cfg.StateStore = store

			r, err := f.cfg.resolve()
			if err != nil {
				t.Fatalf("resolve: %v", err)
			}

			var log stepLog
			s := loggedSteps(&log)
			tt.setup(&f, &s, r.GOOS, r.GOARCH)

			installed, err := f.cfg.apply(context.Background(), f.sel, s)
			if installed != nil {
				t.Errorf("apply Installed = %v, want nil", installed)
			}

			var pe *PreflightError
			if !errors.As(err, &pe) {
				t.Fatalf("apply error = %v, want a *PreflightError", err)
			}

			if tt.cause != nil && !errors.Is(err, tt.cause) {
				t.Errorf("apply error = %v, want one matching %v", err, tt.cause)
			}

			assertNoReplacement(t, &log)
			f.assertUntouched(t)
			assertNothingRecorded(t, store)
		})
	}
}

func TestApplyPassesAParentCancelDuringThePreflightThrough(t *testing.T) {
	t.Parallel()

	f := newApplyFixture(t)
	store := &fakeStore{loads: []loadResult{{}}}
	f.cfg.StateStore = store

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	var log stepLog
	s := loggedSteps(&log)
	s.run = func(context.Context, string, []string, io.Writer, io.Writer) error {
		cancel()
		return ctx.Err()
	}

	installed, err := f.cfg.apply(ctx, f.sel, s)
	if installed != nil {
		t.Errorf("apply Installed = %v, want nil", installed)
	}

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("apply error = %v, want context.Canceled", err)
	}

	var pe *PreflightError
	if errors.As(err, &pe) {
		t.Errorf("apply error = %v, want no *PreflightError", err)
	}

	assertNoReplacement(t, &log)
	f.assertUntouched(t)
	assertNothingRecorded(t, store)
}

// assertNoReplacement fails t if log holds a chmod or a rename: the steps that replace the
// executable.
func assertNoReplacement(t *testing.T, log *stepLog) {
	t.Helper()

	for _, call := range log.list() {
		if strings.HasPrefix(call, "chmod ") || strings.HasPrefix(call, "rename ") {
			t.Errorf("steps = %q, want no chmod and no rename", log.list())
			return
		}
	}
}

// assertNothingRecorded fails t unless store was loaded only by the downgrade guard and never
// saved, so HighestApplied is as it was.
func assertNothingRecorded(t *testing.T, store *fakeStore) {
	t.Helper()

	if store.loadCalls != 1 {
		t.Errorf("Load calls = %d, want 1, the guard's", store.loadCalls)
	}

	if saves := store.recordedSaves(); len(saves) != 0 {
		t.Errorf("saves = %+v, want none", saves)
	}
}

// TestApplyJoinsTheDiscardFailureOntoThePreflightRefusal replaces the staged file with a directory
// holding a file while the preflight runs, so removing the staged binary fails.
func TestApplyJoinsTheDiscardFailureOntoThePreflightRefusal(t *testing.T) {
	t.Parallel()

	f := newApplyFixture(t)
	f.cfg.StateStore = &fakeStore{loads: []loadResult{{}}}

	s := defaultSteps()
	s.run = func(_ context.Context, name string, _ []string, _, _ io.Writer) error {
		if err := os.Remove(name); err != nil {
			return err
		}

		if err := os.Mkdir(name, 0o700); err != nil {
			return err
		}

		return os.WriteFile(filepath.Join(name, "keep"), nil, 0o600)
	}

	installed, err := f.cfg.apply(context.Background(), f.sel, s)
	if installed != nil {
		t.Errorf("apply Installed = %v, want nil", installed)
	}

	var pe *PreflightError
	if !errors.As(err, &pe) {
		t.Fatalf("apply error = %v, want a *PreflightError", err)
	}

	var pathErr *fs.PathError
	if !errors.As(err, &pathErr) || pathErr.Op != "remove" {
		t.Errorf("apply error = %v, want it to carry the failed remove of the staged binary", err)
	}

	t.Cleanup(func() { _ = os.RemoveAll(pe.Path) })
}
