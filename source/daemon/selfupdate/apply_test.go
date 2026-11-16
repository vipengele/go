package selfupdate

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"go.vipengele.org/cli/spec"
	"go.vipengele.org/daemon"
	"go.vipengele.org/update"
	"go.vipengele.org/update/updatetest"
)

// The tests that run Apply never call t.Parallel: Apply runs the staged release binary, and a
// process forked by another test inherits every descriptor open at that moment until it execs, so
// an executable some parallel test still holds open for writing fails the exec with ETXTBSY.

const (
	applyName  = "atk"
	applyOwner = "pedromvgomes"
	applyRepo  = "agentic-toolkit"
)

// applyFixture is an executable holding "old" in its own resolved directory, an updatetest server
// publishing release newVersion of it, and a Config whose executable is that file and whose
// requests go to the server, which trusts the server's signing key, with the located Selection of
// the release's assets.
type applyFixture struct {
	exe string
	srv *updatetest.Server
	cfg update.Config
	sel update.Selection
}

func newApplyFixture(t *testing.T) applyFixture {
	t.Helper()

	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}

	exe := filepath.Join(dir, applyName)
	if err := os.WriteFile(exe, []byte("old"), 0o755); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	rel := updatetest.NewRelease(t, applyOwner, applyRepo, newVersion, applyName)
	srv := updatetest.NewServer(t, rel)
	home := t.TempDir()

	cfg := update.Config{
		Identity: spec.Identity{
			Name:      applyName,
			Owner:     applyOwner,
			Repo:      applyRepo,
			EnvPrefix: "ATK",
			DirName:   applyRepo,
		},
		TrustedKeys: []string{srv.PublicKey()},
		TempDir:     t.TempDir(),
		StateDir:    t.TempDir(),
		HTTPClient:  srv.Client(),
		Executable:  func() (string, error) { return exe, nil },
		InContainer: func() bool { return false },
		Getenv:      func(string) string { return "" },
		UserHomeDir: func() (string, error) { return home, nil },
	}

	sig := rel.ChecksumsName + ".minisig"
	sel := update.Selection{
		Tag:       newVersion,
		Located:   true,
		Archive:   update.Asset{Name: rel.ArchiveName, URL: srv.AssetURL(rel.ArchiveName)},
		Checksums: update.Asset{Name: rel.ChecksumsName, URL: srv.AssetURL(rel.ChecksumsName)},
		Signature: update.Asset{Name: sig, URL: srv.AssetURL(sig)},
	}

	return applyFixture{exe: exe, srv: srv, cfg: cfg, sel: sel}
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

// readBytes returns the content of the file at path, failing t when it cannot be read.
func readBytes(t *testing.T, path string) []byte {
	t.Helper()

	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	return b
}

// syncBuffer is a bytes.Buffer safe for the concurrent writes of a logger.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buf.String()
}

// textLogger returns a logger writing text records at every level to the returned buffer.
func textLogger() (*slog.Logger, *syncBuffer) {
	buf := &syncBuffer{}

	return slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})), buf
}

// supervised is an environment lookup under which the process counts as started by systemd.
func supervised(key string) (string, bool) {
	if key == "SYSTEMD_EXEC_PID" {
		return strconv.Itoa(os.Getpid()), true
	}

	return "", false
}

// unsupervised is an environment lookup that holds no variable.
func unsupervised(string) (string, bool) { return "", false }

// newApplyMachine returns a Machine running oldVersion over store with the real file operations,
// logging to the returned buffer and counting as supervised unless opts say otherwise.
func newApplyMachine(t *testing.T, store Store, opts ...Option) (*Machine, *syncBuffer) {
	t.Helper()

	logger, buf := textLogger()
	opts = append([]Option{
		WithClock(fakeClock{now: testTime()}),
		WithLogger(logger),
		WithSupervisorCheck("", supervised),
	}, opts...)

	m, err := New(store, oldVersion, opts...)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	return m, buf
}

func TestApplyRecordsThePendingUpdateBeforeTheSwapAndAsksForARestart(t *testing.T) {
	f := newApplyFixture(t)
	store := &observingStore{Store: NewFileStore(recordPath(t))}
	m, buf := newApplyMachine(t, store)

	var atRecord []byte
	store.observe(func(rec Record) {
		if rec.Status == StatusPending {
			atRecord = readBytes(t, f.exe)
		}
	})

	err := m.Apply(t.Context(), f.cfg, f.sel)
	if !daemon.RestartRequested(err) {
		t.Fatalf("Apply error = %v, want one for which RestartRequested is true", err)
	}

	if string(atRecord) != "old" {
		t.Errorf("executable when pending was stored = %q, want the previous binary %q", atRecord, "old")
	}

	backup := f.exe + ".old"
	want := Record{
		Status:      StatusPending,
		FromVersion: oldVersion,
		ToVersion:   newVersion,
		Target:      f.exe,
		Backup:      backup,
		StartedAt:   testTime(),
		UpdatedAt:   testTime(),
	}
	if got := load(t, store); got != want {
		t.Errorf("record = %+v, want %+v", got, want)
	}

	if got := readBytes(t, backup); string(got) != "old" {
		t.Errorf("backup holds %q, want %q", got, "old")
	}

	if got, want := readBytes(t, f.exe), updatetest.ReleaseExecutable(t, newVersion); !bytes.Equal(got, want) {
		t.Errorf("executable holds %.64q, want the release's binary %.64q", got, want)
	}

	if strings.Contains(buf.String(), "level=WARN") {
		t.Errorf("log = %q, want no warning", buf.String())
	}
}

func TestApplyWhilePendingRefusesBeforeTheDownload(t *testing.T) {
	f := newApplyFixture(t)
	store := storeWith(t, pendingRecord())
	m, _ := newApplyMachine(t, store)

	err := m.Apply(t.Context(), f.cfg, f.sel)
	if !errors.Is(err, update.ErrBackupInUse) {
		t.Fatalf("Apply error = %v, want ErrBackupInUse", err)
	}

	if daemon.RestartRequested(err) {
		t.Errorf("RestartRequested(%v) = true, want false", err)
	}

	if n := f.requests(); n != 0 {
		t.Errorf("server requests = %d, want 0", n)
	}

	if got := readBytes(t, f.exe); string(got) != "old" {
		t.Errorf("executable holds %q, want %q", got, "old")
	}

	if got := load(t, store); got != pendingRecord() {
		t.Errorf("record = %+v, want the pending record unchanged", got)
	}
}

func TestApplyLogsAFailedPostUpdateHookWithoutJoiningIt(t *testing.T) {
	f := newApplyFixture(t)

	errHook := errors.New("completion install failed")

	var hookPaths []string
	f.cfg.PostUpdate = []update.PostUpdate{
		func(path string) error {
			hookPaths = append(hookPaths, path)
			return errHook
		},
	}

	store := NewFileStore(recordPath(t))
	m, buf := newApplyMachine(t, store)

	err := m.Apply(t.Context(), f.cfg, f.sel)
	if !daemon.RestartRequested(err) {
		t.Fatalf("Apply error = %v, want one for which RestartRequested is true", err)
	}

	if errors.Is(err, errHook) {
		t.Errorf("Apply error = %v, want it without the hook's error", err)
	}

	if len(hookPaths) != 1 || hookPaths[0] != f.exe {
		t.Errorf("hook paths = %q, want [%q]", hookPaths, f.exe)
	}

	log := buf.String()
	if !strings.Contains(log, "level=WARN") || !strings.Contains(log, msgPostUpdateFailed) ||
		!strings.Contains(log, errHook.Error()) {
		t.Errorf("log = %q, want a warning %q carrying %q", log, msgPostUpdateFailed, errHook)
	}

	if got := load(t, store); got.Status != StatusPending {
		t.Errorf("record status = %v, want pending", got.Status)
	}
}

// wantNothingInstalled fails t unless the executable of f still holds "old", with no backup and no
// staged file beside it.
func (f applyFixture) wantNothingInstalled(t *testing.T) {
	t.Helper()

	if got := readBytes(t, f.exe); string(got) != "old" {
		t.Errorf("executable holds %q, want the previous binary %q", got, "old")
	}

	entries, err := os.ReadDir(filepath.Dir(f.exe))
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}

	if len(entries) != 1 || entries[0].Name() != applyName {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}

		t.Errorf("executable's directory holds %q, want only %q", names, applyName)
	}
}

func TestApplyWhoseBeginFailsInstallsNothing(t *testing.T) {
	f := newApplyFixture(t)

	errStore := errors.New("disk full")
	store := &fakeStore{updateErr: errStore}
	m, _ := newApplyMachine(t, store)

	err := m.Apply(t.Context(), f.cfg, f.sel)
	if !errors.Is(err, errStore) {
		t.Fatalf("Apply error = %v, want the store's error", err)
	}

	if daemon.RestartRequested(err) {
		t.Errorf("RestartRequested(%v) = true, want false", err)
	}

	f.wantNothingInstalled(t)

	if got := load(t, store); got != (Record{}) || store.writeCount() != 0 {
		t.Errorf("record = %+v after %d writes, want no record stored", got, store.writeCount())
	}
}

func TestApplyWhoseBeginFindsAnotherUpdatePendingInstallsNothing(t *testing.T) {
	f := newApplyFixture(t)
	store := NewFileStore(recordPath(t))
	m, buf := newApplyMachine(t, store)

	// Another process records its own update once this Apply has passed its guard and staged the
	// binary.
	other := pendingRecord()
	f.cfg.Preflight = []update.Preflight{
		func(ctx context.Context, _ update.StagedBinary) error {
			return store.Update(ctx, put(other))
		},
	}

	err := m.Apply(t.Context(), f.cfg, f.sel)
	if !errors.Is(err, ErrUpdatePending) {
		t.Fatalf("Apply error = %v, want ErrUpdatePending", err)
	}

	if daemon.RestartRequested(err) {
		t.Errorf("RestartRequested(%v) = true, want false", err)
	}

	f.wantNothingInstalled(t)

	if got := load(t, store); got != other {
		t.Errorf("record = %+v, want the other update %+v unchanged", got, other)
	}

	if strings.Contains(buf.String(), "level=WARN") {
		t.Errorf("log = %q, want no warning", buf.String())
	}
}

func TestApplyAbandonsAnInstallFailingAfterBegin(t *testing.T) {
	f := newApplyFixture(t)
	store := &observingStore{Store: NewFileStore(recordPath(t))}
	m, _ := newApplyMachine(t, store)

	// Once pending is stored, the executable resolves to a file in another directory, so Install
	// refuses the staged binary before it renames anything.
	elsewhere := filepath.Join(t.TempDir(), applyName)
	if err := os.WriteFile(elsewhere, []byte("old"), 0o755); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	var (
		mu  sync.Mutex
		exe = f.exe
	)
	f.cfg.Executable = func() (string, error) {
		mu.Lock()
		defer mu.Unlock()

		return exe, nil
	}
	store.observe(func(rec Record) {
		if rec.Status == StatusPending {
			mu.Lock()
			defer mu.Unlock()
			exe = elsewhere
		}
	})

	err := m.Apply(t.Context(), f.cfg, f.sel)
	if err == nil || daemon.RestartRequested(err) {
		t.Fatalf("Apply error = %v, want the install's failure", err)
	}

	got := load(t, store)
	if got.Status != StatusRolledBack || got.ToVersion != newVersion || !strings.Contains(got.Reason, newVersion) {
		t.Errorf("record = %+v, want rolled_back for %s with a reason naming it", got, newVersion)
	}

	f.wantNothingInstalled(t)
}

// failAfterBegin is an installFunc that resolves the target, calls beforeInstall with its plan and
// then, when that succeeds, runs between and fails with the error it returns, renaming nothing.
func failAfterBegin(between func(ctx context.Context, plan update.InstallPlan) error) installFunc {
	return func(
		ctx context.Context,
		cfg update.Config,
		sel update.Selection,
		beforeInstall func(context.Context, update.InstallPlan) error,
		_ ...update.ApplyOption,
	) (*update.Installed, error) {
		target, err := cfg.Target()
		if err != nil {
			return nil, err
		}

		plan := update.InstallPlan{Target: target, Backup: target + ".old", Version: sel.Tag}
		if err := beforeInstall(ctx, plan); err != nil {
			return nil, err
		}

		return nil, between(ctx, plan)
	}
}

func TestApplyLeavesTheUpdatePendingAfterARestoreError(t *testing.T) {
	t.Parallel()

	f := newApplyFixture(t)
	store := NewFileStore(recordPath(t))
	m, _ := newApplyMachine(t, store)

	// No real install fails its restore on demand.
	install := failAfterBegin(func(_ context.Context, plan update.InstallPlan) error {
		return &update.RestoreError{
			Path:       plan.Target,
			Backup:     plan.Backup,
			Err:        errors.New("rename staged: refused"),
			RestoreErr: errors.New("rename backup: refused"),
		}
	})

	err := m.apply(t.Context(), f.cfg, f.sel, install)

	var restoreErr *update.RestoreError
	if !errors.As(err, &restoreErr) {
		t.Fatalf("apply error = %v, want a *update.RestoreError", err)
	}

	if got := load(t, store); got.Status != StatusPending || got.ToVersion != newVersion || got.Target != f.exe {
		t.Errorf("record = %+v, want pending for %s at %s", got, newVersion, f.exe)
	}
}

func TestApplyJoinsAFailedAbandonOntoTheInstallsError(t *testing.T) {
	t.Parallel()

	f := newApplyFixture(t)
	store := NewFileStore(recordPath(t))
	m, _ := newApplyMachine(t, store)

	// Another process settles the pending record between Begin and the failure of the install.
	errRename := errors.New("rename staged: refused")
	install := failAfterBegin(func(ctx context.Context, _ update.InstallPlan) error {
		other, err := New(store, oldVersion)
		if err != nil {
			return err
		}

		if err := other.Abandon(ctx, "settled elsewhere"); err != nil {
			return err
		}

		return errRename
	})

	err := m.apply(t.Context(), f.cfg, f.sel, install)
	if !errors.Is(err, errRename) || !errors.Is(err, ErrNoPendingUpdate) {
		t.Fatalf("apply error = %v, want the install's error and ErrNoPendingUpdate", err)
	}

	if got := load(t, store); got.Status != StatusRolledBack || got.Reason != "settled elsewhere" {
		t.Errorf("record = %+v, want rolled_back by the other process", got)
	}
}

func TestApplyKeepsEveryOtherConfigGuardedByTheMachineRefused(t *testing.T) {
	f := newApplyFixture(t)
	store := &observingStore{Store: NewFileStore(recordPath(t))}
	m, _ := newApplyMachine(t, store)

	other := f.cfg
	other.BackupGuard = m

	var refusedWhilePending error
	store.observe(func(rec Record) {
		if rec.Status == StatusPending {
			_, refusedWhilePending = other.Apply(t.Context(), f.sel)
		}
	})

	if err := m.Apply(t.Context(), f.cfg, f.sel); !daemon.RestartRequested(err) {
		t.Fatalf("Apply error = %v, want one for which RestartRequested is true", err)
	}

	if !errors.Is(refusedWhilePending, update.ErrBackupInUse) {
		t.Errorf("other Apply while pending = %v, want ErrBackupInUse", refusedWhilePending)
	}

	store.observe(nil)

	if _, err := other.Apply(t.Context(), f.sel); !errors.Is(err, update.ErrBackupInUse) {
		t.Errorf("other Apply after the install = %v, want ErrBackupInUse", err)
	}
}

func TestApplyGuardAnswersAsTheMachineUntilItsBeginSucceeds(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		rec   Record
		began bool
		want  bool
	}{
		{name: "idle before Begin", rec: Record{}, want: false},
		{name: "another update pending before Begin", rec: pendingRecord(), want: true},
		{name: "its own update pending after Begin", rec: pendingRecord(), began: true, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			m, _ := newApplyMachine(t, &fakeStore{rec: tt.rec})
			g := &applyGuard{m: m}
			g.began.Store(tt.began)

			got, err := g.BackupInUse(t.Context())
			if err != nil || got != tt.want {
				t.Errorf("BackupInUse() = %v, %v; want %v, nil", got, err, tt.want)
			}
		})
	}
}

func TestApplyGuardReportsTheMachinesError(t *testing.T) {
	t.Parallel()

	errLoad := errors.New("unreadable")
	m, _ := newApplyMachine(t, &fakeStore{loadErr: errLoad})

	if _, err := (&applyGuard{m: m}).BackupInUse(t.Context()); !errors.Is(err, errLoad) {
		t.Errorf("BackupInUse() error = %v, want %v", err, errLoad)
	}
}

func TestApplyWarnsWhenNoSupervisorRestartsTheProcess(t *testing.T) {
	f := newApplyFixture(t)
	m, buf := newApplyMachine(t, NewFileStore(recordPath(t)), WithSupervisorCheck("org.example.atk", unsupervised))

	if err := m.Apply(t.Context(), f.cfg, f.sel); !daemon.RestartRequested(err) {
		t.Fatalf("Apply error = %v, want one for which RestartRequested is true", err)
	}

	log := buf.String()
	if !strings.Contains(log, "level=WARN") || !strings.Contains(log, msgNotSupervised) ||
		!strings.Contains(log, "label=org.example.atk") {
		t.Errorf("log = %q, want a warning %q naming the label", log, msgNotSupervised)
	}
}

func TestApplyLogsThroughTheDefaultLoggerWithoutWithLogger(t *testing.T) {
	logger, buf := textLogger()
	prev := slog.Default()
	slog.SetDefault(logger)
	t.Cleanup(func() { slog.SetDefault(prev) })

	f := newApplyFixture(t)
	m, err := New(NewFileStore(recordPath(t)), oldVersion,
		WithClock(fakeClock{now: testTime()}), WithSupervisorCheck("org.example.atk", unsupervised))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if err := m.Apply(t.Context(), f.cfg, f.sel); !daemon.RestartRequested(err) {
		t.Fatalf("Apply error = %v, want one for which RestartRequested is true", err)
	}

	log := buf.String()
	if !strings.Contains(log, "level=WARN") || !strings.Contains(log, msgNotSupervised) {
		t.Errorf("default log = %q, want the warning %q", log, msgNotSupervised)
	}
}

// applyRunner is a daemon.Runner whose work is one Machine.Apply, after which it exits with
// Apply's error.
type applyRunner struct {
	apply func(context.Context) error

	done chan struct{}
	err  error
}

func (r *applyRunner) Name() string { return "update" }

func (r *applyRunner) Start(context.Context) error {
	go func() {
		defer close(r.done)
		r.err = r.apply(context.Background())
	}()

	return nil
}

func (r *applyRunner) Done() <-chan struct{} { return r.done }

func (r *applyRunner) Err() error { return r.err }

func (r *applyRunner) Stop(ctx context.Context) error {
	select {
	case <-r.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func TestARunnerReturningApplysErrorEndsRunWithARestart(t *testing.T) {
	f := newApplyFixture(t)
	m, _ := newApplyMachine(t, NewFileStore(recordPath(t)))

	runner := &applyRunner{
		apply: func(ctx context.Context) error { return m.Apply(ctx, f.cfg, f.sel) },
		done:  make(chan struct{}),
	}

	logger, _ := textLogger()

	sup, err := daemon.New([]daemon.Runner{runner}, daemon.WithLogger(logger))
	if err != nil {
		t.Fatalf("daemon.New: %v", err)
	}

	if err := sup.Run(t.Context()); !daemon.RestartRequested(err) {
		t.Errorf("Run error = %v, want one for which RestartRequested is true", err)
	}
}
