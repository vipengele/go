package selfupdate

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
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

func TestApplyRecordsThePendingUpdateAndAsksForARestart(t *testing.T) {
	f := newApplyFixture(t)
	store := NewFileStore(recordPath(t))
	m, buf := newApplyMachine(t, store)

	err := m.Apply(t.Context(), f.cfg, f.sel)
	if !daemon.RestartRequested(err) {
		t.Fatalf("Apply error = %v, want one for which RestartRequested is true", err)
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

// failingUpdateStore is an idle fakeStore whose Update runs before and then fails with err,
// storing nothing.
type failingUpdateStore struct {
	fakeStore

	before func()
	err    error
}

func (s *failingUpdateStore) Update(context.Context, func(Record) (Record, error)) error {
	if s.before != nil {
		s.before()
	}

	return s.err
}

func TestApplyRollsBackWhenBeginFails(t *testing.T) {
	f := newApplyFixture(t)

	errStore := errors.New("disk full")
	store := &failingUpdateStore{err: errStore}
	m, _ := newApplyMachine(t, store)

	err := m.Apply(t.Context(), f.cfg, f.sel)
	if !errors.Is(err, errStore) {
		t.Fatalf("Apply error = %v, want the store's error", err)
	}

	if daemon.RestartRequested(err) {
		t.Errorf("RestartRequested(%v) = true, want false", err)
	}

	if got := readBytes(t, f.exe); string(got) != "old" {
		t.Errorf("executable holds %q, want the previous binary %q", got, "old")
	}

	if _, err := os.Stat(f.exe + ".old"); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("Stat(backup) error = %v, want ErrNotExist", err)
	}

	if got := load(t, store); got != (Record{}) || store.writeCount() != 0 {
		t.Errorf("record = %+v after %d writes, want no record stored", got, store.writeCount())
	}
}

func TestApplyNamesTheBackupWhenTheRollbackFailsToo(t *testing.T) {
	f := newApplyFixture(t)

	backup := f.exe + ".old"
	errStore := errors.New("disk full")
	store := &failingUpdateStore{
		err: errStore,
		// Without the backup, the rename that rolls the install back fails.
		before: func() {
			if err := os.Remove(backup); err != nil {
				t.Errorf("Remove(backup): %v", err)
			}
		},
	}
	m, _ := newApplyMachine(t, store)

	err := m.Apply(t.Context(), f.cfg, f.sel)
	if err == nil {
		t.Fatal("Apply error = nil, want the record and rollback failures")
	}

	if !errors.Is(err, errStore) || !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("Apply error = %v, want it to carry the store's error and the rollback's ErrNotExist", err)
	}

	if !strings.Contains(err.Error(), backup) {
		t.Errorf("Apply error = %q, want it to name the backup %q", err, backup)
	}

	if daemon.RestartRequested(err) {
		t.Errorf("RestartRequested(%v) = true, want false", err)
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
