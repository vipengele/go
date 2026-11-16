package selfupdate

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"go.vipengele.org/cli/spec"
	"go.vipengele.org/daemon"
	"go.vipengele.org/filelock"
	"go.vipengele.org/update"
	"go.vipengele.org/update/updatetest"
)

const (
	runningVersion = "v1.0.0"
	newerVersion   = "v1.1.0"
)

// oldBinary is the content of the executable the tests update.
func oldBinary() []byte { return []byte("#!/bin/sh\necho old\n") }

// memState is an update.StateStore in memory, so that no test touches the user's state directory.
type memState struct {
	mu sync.Mutex
	st update.State
}

func (s *memState) Load(context.Context) (update.State, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.st, nil
}

func (s *memState) Save(_ context.Context, st update.State) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.st = st
	return nil
}

// noRelease is an update.Source that holds no release.
type noRelease struct{}

func (noRelease) Latest(context.Context, update.Channel) (update.Release, error) {
	return update.Release{}, fmt.Errorf("source: %w", update.ErrNoRelease)
}

// observingStore is a Store that calls onWrite with every record an Update stores, once it is
// stored.
type observingStore struct {
	Store

	mu      sync.Mutex
	onWrite func(Record)
}

func (s *observingStore) Update(ctx context.Context, fn func(Record) (Record, error)) error {
	var written Record
	err := s.Store.Update(ctx, func(rec Record) (Record, error) {
		next, err := fn(rec)
		written = next
		return next, err
	})
	if err != nil {
		return err
	}
	s.mu.Lock()
	onWrite := s.onWrite
	s.mu.Unlock()
	if onWrite != nil {
		onWrite(written)
	}
	return nil
}

func (s *observingStore) observe(onWrite func(Record)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onWrite = onWrite
}

// cycleEnv is a Runner over a real update.Config, served by an updatetest.Server, whose
// executable, state and lock all live in temporary directories of the test.
type cycleEnv struct {
	t         *testing.T
	target    string
	storePath string
	srv       *updatetest.Server
	cfg       update.Config
	store     *observingStore
	machine   *Machine
	r         *Runner[Settings]

	exeMu sync.Mutex
	exe   string

	logMu   sync.Mutex
	records []record
}

// envOptions shapes a cycleEnv.
type envOptions struct {
	version   string
	release   string
	autoApply bool
	source    update.Source
	server    []updatetest.Option
	preflight []update.Preflight
}

func newCycleEnv(t *testing.T, o envOptions) *cycleEnv {
	t.Helper()
	if o.version == "" {
		o.version = runningVersion
	}
	if o.release == "" {
		o.release = newerVersion
	}

	dir := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		dir = resolved
	}
	stateDir := t.TempDir()

	e := &cycleEnv{t: t, target: filepath.Join(dir, "tool"), storePath: filepath.Join(stateDir, "selfupdate.json")}
	e.exe = e.target
	if err := os.WriteFile(e.target, oldBinary(), 0o755); err != nil {
		t.Fatal(err)
	}

	identity := spec.Identity{Name: "tool", Owner: "acme", Repo: "tool", EnvPrefix: "TOOL", DirName: "tool"}
	e.srv = updatetest.NewServer(t, updatetest.NewRelease(t, "acme", "tool", o.release, "tool"), o.server...)
	source := o.source
	if source == nil {
		gh := update.NewGitHubSource(identity, e.srv.Client())
		gh.WebURL = e.srv.WebURL()
		gh.APIURL = e.srv.APIURL()
		source = gh
	}

	e.cfg = update.Config{
		Identity:    identity,
		TrustedKeys: []string{e.srv.PublicKey()},
		Version:     o.version,
		Source:      source,
		HTTPClient:  e.srv.Client(),
		GOOS:        runtime.GOOS,
		GOARCH:      runtime.GOARCH,
		TempDir:     t.TempDir(),
		StateDir:    stateDir,
		StateStore:  &memState{},
		Locker:      filelock.New(filepath.Join(stateDir, "update.lock")),
		Executable:  e.executable,
		InContainer: func() bool { return false },
		IsTerminal:  func() bool { return false },
		Preflight:   o.preflight,
	}

	e.store = &observingStore{Store: NewFileStore(e.storePath)}
	m, err := New(e.store, o.version)
	if err != nil {
		t.Fatal(err)
	}
	e.machine = m

	clock := newArmingClock(time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC))
	logger := slog.New(captureHandler{mu: &e.logMu, records: &e.records})
	settings := Settings{AutoApply: o.autoApply}
	r, err := NewRunner(e.cfg, m, settings, func(s Settings) Settings { return s },
		WithLogger(logger), WithRunnerClock(clock))
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}
	e.r = r
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), failAfter)
		defer cancel()
		if err := r.Stop(ctx); err != nil {
			t.Errorf("cleanup Stop() = %v", err)
		}
	})

	return e
}

func (e *cycleEnv) executable() (string, error) {
	e.exeMu.Lock()
	defer e.exeMu.Unlock()
	return e.exe, nil
}

func (e *cycleEnv) setExecutable(path string) {
	e.exeMu.Lock()
	defer e.exeMu.Unlock()
	e.exe = path
}

// run runs one check.
func (e *cycleEnv) run() error {
	e.t.Helper()
	ctx, cancel := context.WithTimeout(e.t.Context(), failAfter)
	defer cancel()
	return e.r.check.run(ctx)
}

// runSwallowed runs one check and fails the test unless it returned nil.
func (e *cycleEnv) runSwallowed() {
	e.t.Helper()
	if err := e.run(); err != nil {
		e.t.Fatalf("check = %v, want nil", err)
	}
}

// logged fails the test unless exactly one record with msg was logged, at level, and returns it.
func (e *cycleEnv) logged(level slog.Level, msg string) record {
	e.t.Helper()
	e.logMu.Lock()
	defer e.logMu.Unlock()
	var found []record
	for _, rec := range e.records {
		if rec.msg == msg {
			found = append(found, rec)
		}
	}
	if len(found) != 1 {
		e.t.Fatalf("records with %q = %d, want 1; all: %+v", msg, len(found), e.records)
	}
	if found[0].level != level {
		e.t.Fatalf("%q logged at %v, want %v", msg, found[0].level, level)
	}
	return found[0]
}

// notLogged fails the test if any record was logged with msg.
func (e *cycleEnv) notLogged(msg string) {
	e.t.Helper()
	e.logMu.Lock()
	defer e.logMu.Unlock()
	for _, rec := range e.records {
		if rec.msg == msg {
			e.t.Errorf("logged %q at %v, want no such record", msg, rec.level)
		}
	}
}

// noWarnings fails the test if any record was logged at Warn or above.
func (e *cycleEnv) noWarnings() {
	e.t.Helper()
	e.logMu.Lock()
	defer e.logMu.Unlock()
	for _, rec := range e.records {
		if rec.level >= slog.LevelWarn {
			e.t.Fatalf("logged %q at %v, want nothing at Warn", rec.msg, rec.level)
		}
	}
}

func (e *cycleEnv) record() Record {
	e.t.Helper()
	rec, err := e.machine.State(e.t.Context())
	if err != nil {
		e.t.Fatalf("State() error = %v", err)
	}
	return rec
}

func (e *cycleEnv) wantTarget(want []byte) {
	e.t.Helper()
	got, err := os.ReadFile(e.target)
	if err != nil {
		e.t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		e.t.Fatalf("target holds %q, want %q", got, want)
	}
}

func (e *cycleEnv) wantNoDownload() {
	e.t.Helper()
	for _, target := range []updatetest.Target{updatetest.TargetArchive, updatetest.TargetChecksums, updatetest.TargetSignature} {
		if n := e.srv.Requests(target); n != 0 {
			e.t.Fatalf("%v requests = %d, want 0", target, n)
		}
	}
}

func (e *cycleEnv) wantNoBackup() {
	e.t.Helper()
	if _, err := os.Stat(e.target + ".old"); !errors.Is(err, os.ErrNotExist) {
		e.t.Fatalf("stat backup = %v, want it absent", err)
	}
}

func TestRunnerOfADevelopmentBuildSchedulesNothing(t *testing.T) {
	t.Parallel()
	e := newCycleEnv(t, envOptions{version: "dev", autoApply: true})

	if err := e.r.Start(t.Context()); err != nil {
		t.Fatalf("Start() = %v", err)
	}
	if err := e.r.Start(t.Context()); !errors.Is(err, errRunnerStarted) {
		t.Fatalf("second Start() = %v, want errRunnerStarted", err)
	}
	select {
	case d := <-e.r.sched.clock.(armingClock).armed:
		t.Fatalf("armed a timer for %v, want none", d)
	default:
	}
	rec := e.logged(slog.LevelInfo, msgDevelopmentBuild)
	if got := rec.attrs["version"].String(); got != "dev" {
		t.Errorf("version attr = %q, want dev", got)
	}
	select {
	case <-e.r.Done():
		t.Fatal("Done closed before Stop")
	default:
	}

	if err := e.r.Stop(t.Context()); err != nil {
		t.Fatalf("Stop() = %v", err)
	}
	select {
	case <-e.r.Done():
	default:
		t.Fatal("Done open after Stop")
	}
	if err := e.r.Err(); err != nil {
		t.Errorf("Err() = %v, want nil", err)
	}
	if n := e.srv.Requests(updatetest.TargetLatest) + e.srv.Requests(updatetest.TargetLatestAPI); n != 0 {
		t.Errorf("latest requests = %d, want 0", n)
	}
}

func TestRunnerOfADevelopmentBuildStoppedBeforeStart(t *testing.T) {
	t.Parallel()
	e := newCycleEnv(t, envOptions{version: "dev"})

	if err := e.r.Stop(t.Context()); err != nil {
		t.Fatalf("Stop() = %v", err)
	}
	if err := e.r.Start(t.Context()); !errors.Is(err, context.Canceled) {
		t.Fatalf("Start() after Stop = %v, want context.Canceled", err)
	}
}

func TestRunnerOfAReleaseSchedulesTheFirstCheck(t *testing.T) {
	t.Parallel()
	e := newCycleEnv(t, envOptions{})

	if err := e.r.Start(t.Context()); err != nil {
		t.Fatalf("Start() = %v", err)
	}
	select {
	case <-e.r.sched.clock.(armingClock).armed:
	case <-time.After(failAfter):
		t.Fatal("the runner armed no timer")
	}
}

func TestReloadAppliesSettings(t *testing.T) {
	t.Parallel()
	e := newCycleEnv(t, envOptions{})

	if err := e.r.Reload(t.Context(), Settings{AutoApply: true, Interval: 2 * time.Hour}); err != nil {
		t.Fatalf("Reload() = %v", err)
	}
	if !e.r.sched.autoApply() {
		t.Error("autoApply() = false after a reload turning it on")
	}
	if err := e.r.Reload(t.Context(), Settings{Interval: time.Minute}); err != nil {
		t.Fatalf("Reload() of an invalid interval = %v, want nil", err)
	}
}

func TestCycleWithNoReleaseLogsAtDebug(t *testing.T) {
	t.Parallel()
	e := newCycleEnv(t, envOptions{source: noRelease{}, autoApply: true})

	e.runSwallowed()

	e.logged(slog.LevelDebug, msgNoRelease)
	e.noWarnings()
	e.wantNoDownload()
}

func TestCycleUpToDateLogsAtDebug(t *testing.T) {
	t.Parallel()
	e := newCycleEnv(t, envOptions{release: runningVersion, autoApply: true})

	e.runSwallowed()

	rec := e.logged(slog.LevelDebug, msgUpToDate)
	if got := rec.attrs["latest"].String(); got != runningVersion {
		t.Errorf("latest attr = %q, want %q", got, runningVersion)
	}
	e.noWarnings()
	e.wantNoDownload()
	e.wantTarget(oldBinary())
}

func TestCycleWithAutoApplyOffReportsTheUpdate(t *testing.T) {
	t.Parallel()
	e := newCycleEnv(t, envOptions{})

	e.runSwallowed()

	rec := e.logged(slog.LevelInfo, msgAvailable)
	if got := rec.attrs["latest"].String(); got != newerVersion {
		t.Errorf("latest attr = %q, want %q", got, newerVersion)
	}
	e.noWarnings()
	e.wantNoDownload()
	e.wantTarget(oldBinary())
	if got := e.record().Status; got != StatusIdle {
		t.Errorf("status = %v, want idle", got)
	}
}

func TestCycleWithAnUpdatePendingSkips(t *testing.T) {
	t.Parallel()
	e := newCycleEnv(t, envOptions{autoApply: true})
	pending := Pending{FromVersion: "v0.9.0", ToVersion: runningVersion, Target: e.target, Backup: e.target + ".old"}
	if err := e.machine.Begin(t.Context(), pending); err != nil {
		t.Fatal(err)
	}

	e.runSwallowed()

	e.logged(slog.LevelInfo, msgPendingSkipped)
	e.noWarnings()
	if n := e.srv.Requests(updatetest.TargetLatest) + e.srv.Requests(updatetest.TargetLatestAPI); n != 0 {
		t.Errorf("latest requests = %d, want 0", n)
	}
	e.wantNoDownload()
	e.wantTarget(oldBinary())
}

// A test that installs runs the staged binary, so it does not call t.Parallel: a process forked
// by a parallel test would hold the staged file open for writing, and its exec would fail with
// ETXTBSY.

func TestCycleInstallsAndRequiresARestart(t *testing.T) {
	e := newCycleEnv(t, envOptions{autoApply: true})

	var (
		mu       sync.Mutex
		atRecord []byte
	)
	e.store.observe(func(rec Record) {
		if rec.Status != StatusPending {
			return
		}
		b, err := os.ReadFile(e.target)
		if err != nil {
			t.Errorf("reading the target when pending was stored: %v", err)
		}
		mu.Lock()
		defer mu.Unlock()
		atRecord = b
	})

	err := e.run()

	if !errors.Is(err, daemon.ErrRestartRequired) || !daemon.RestartRequested(err) {
		t.Fatalf("check = %v, want a requested restart", err)
	}
	mu.Lock()
	if !bytes.Equal(atRecord, oldBinary()) {
		t.Errorf("target when pending was stored = %q, want the previous binary", atRecord)
	}
	mu.Unlock()

	rec := e.record()
	want := Record{
		Status:      StatusPending,
		FromVersion: runningVersion,
		ToVersion:   newerVersion,
		Target:      e.target,
		Backup:      e.target + ".old",
	}
	if rec.Status != want.Status || rec.FromVersion != want.FromVersion || rec.ToVersion != want.ToVersion ||
		rec.Target != want.Target || rec.Backup != want.Backup {
		t.Errorf("record = %+v, want %+v", rec, want)
	}

	e.wantTarget(updatetest.ReleaseExecutable(t, newerVersion))
	backup, err := os.ReadFile(e.target + ".old")
	if err != nil {
		t.Fatalf("the backup was not kept: %v", err)
	}
	if !bytes.Equal(backup, oldBinary()) {
		t.Errorf("backup holds %q, want the previous binary", backup)
	}
	e.noWarnings()
}

func TestCycleApplyFailingBeforeTheHookLeavesTheRecordIdle(t *testing.T) {
	e := newCycleEnv(t, envOptions{autoApply: true, server: []updatetest.Option{updatetest.BadChecksum()}})

	e.runSwallowed()

	rec := e.logged(slog.LevelWarn, msgApplyFailed)
	var mismatch *update.ChecksumMismatchError
	if err, ok := rec.attrs["err"].Any().(error); !ok || !errors.As(err, &mismatch) {
		t.Errorf("err attr = %v, want a *update.ChecksumMismatchError", rec.attrs["err"])
	}
	if got := e.record().Status; got != StatusIdle {
		t.Errorf("status = %v, want idle", got)
	}
	e.wantTarget(oldBinary())
	e.wantNoBackup()
}

func TestCycleInstallFailingAfterTheHookAbandonsTheUpdate(t *testing.T) {
	e := newCycleEnv(t, envOptions{autoApply: true})

	// Once pending is stored, the executable resolves to a file in another directory, so Install
	// refuses the staged binary before it renames anything.
	elsewhere := filepath.Join(t.TempDir(), "tool")
	if err := os.WriteFile(elsewhere, oldBinary(), 0o755); err != nil {
		t.Fatal(err)
	}
	e.store.observe(func(rec Record) {
		if rec.Status == StatusPending {
			e.setExecutable(elsewhere)
		}
	})

	e.runSwallowed()

	e.logged(slog.LevelWarn, msgApplyFailed)
	e.notLogged(msgAbandonFailed)
	rec := e.record()
	if rec.Status != StatusRolledBack || rec.ToVersion != newerVersion || rec.Reason == "" {
		t.Errorf("record = %+v, want rolled_back for %s with a reason", rec, newerVersion)
	}
	e.wantTarget(oldBinary())
	e.wantNoBackup()

	e.store.observe(nil)
	e.setExecutable(e.target)

	if err := e.run(); !errors.Is(err, daemon.ErrRestartRequired) {
		t.Fatalf("retried check = %v, want ErrRestartRequired", err)
	}
	if rec := e.record(); rec.Status != StatusPending || rec.ToVersion != newerVersion {
		t.Errorf("record after the retry = %+v, want pending for %s", rec, newerVersion)
	}
	e.wantTarget(updatetest.ReleaseExecutable(t, newerVersion))
}

func TestCycleRestoreErrorLeavesTheUpdatePending(t *testing.T) {
	t.Parallel()
	e := newCycleEnv(t, envOptions{autoApply: true})

	// The executable's path may hold no binary after a *RestoreError, which no real install
	// produces on demand; the apply calls the hook and then fails with one.
	e.r.check.apply = func(
		ctx context.Context,
		r update.Config,
		sel update.Selection,
		beforeInstall func(context.Context, update.InstallPlan) error,
	) (*update.Installed, error) {
		target, err := r.Target()
		if err != nil {
			return nil, err
		}
		plan := update.InstallPlan{Target: target, Backup: target + ".old", Version: sel.Tag}
		if err := beforeInstall(ctx, plan); err != nil {
			return nil, err
		}
		return nil, &update.RestoreError{
			Path:       target,
			Backup:     plan.Backup,
			Err:        errors.New("rename staged: refused"),
			RestoreErr: errors.New("rename backup: refused"),
		}
	}

	e.runSwallowed()

	rec := e.logged(slog.LevelWarn, msgRestorePending)
	if got := rec.attrs["backup"].String(); got != e.target+".old" {
		t.Errorf("backup attr = %q, want %q", got, e.target+".old")
	}
	if got := e.record(); got.Status != StatusPending || got.ToVersion != newerVersion {
		t.Errorf("record = %+v, want pending for %s", got, newerVersion)
	}
}

func TestCycleBeginRefusedByAPendingUpdateSkipsTheInstall(t *testing.T) {
	var e *cycleEnv
	other := Pending{FromVersion: runningVersion, ToVersion: "v1.2.0", Target: "/elsewhere/tool", Backup: "/elsewhere/tool.old"}
	// Another process records its own update once this check has found the release idle and
	// staged it.
	e = newCycleEnv(t, envOptions{autoApply: true, preflight: []update.Preflight{
		func(ctx context.Context, _ update.StagedBinary) error {
			m, err := New(NewFileStore(e.storePath), runningVersion)
			if err != nil {
				return err
			}
			return m.Begin(ctx, other)
		},
	}})

	e.runSwallowed()

	e.logged(slog.LevelInfo, msgBeginPending)
	e.noWarnings()
	if rec := e.record(); rec.Status != StatusPending || rec.ToVersion != other.ToVersion {
		t.Errorf("record = %+v, want the other update pending", rec)
	}
	e.wantTarget(oldBinary())
	e.wantNoBackup()
}

func TestNewRunnerSetsANilBackupGuardToTheMachine(t *testing.T) {
	t.Parallel()
	e := newCycleEnv(t, envOptions{})

	if e.cfg.BackupGuard != nil {
		t.Fatal("the test's Config carries a BackupGuard")
	}
	if got, ok := e.r.check.cfg.BackupGuard.(*Machine); !ok || got != e.machine {
		t.Errorf("BackupGuard = %v, want the Machine", e.r.check.cfg.BackupGuard)
	}

	e.cfg.BackupGuard = e.machine
	r, err := NewRunner(e.cfg, e.machine, Settings{}, func(s Settings) Settings { return s })
	if err != nil {
		t.Fatalf("NewRunner() with the Machine as BackupGuard = %v", err)
	}
	if got := r.check.cfg.BackupGuard; got != e.machine {
		t.Errorf("BackupGuard = %v, want the Machine", got)
	}
}

// otherGuard is a BackupGuard that is not a Machine.
type otherGuard struct{}

func (otherGuard) BackupInUse(context.Context) (bool, error) { return false, nil }

func TestNewRunnerRefuses(t *testing.T) {
	t.Parallel()
	e := newCycleEnv(t, envOptions{})
	another, err := New(NewFileStore(e.storePath), runningVersion)
	if err != nil {
		t.Fatal(err)
	}
	identity := func(s Settings) Settings { return s }

	tests := []struct {
		name       string
		cfg        func() update.Config
		machine    *Machine
		settings   Settings
		settingsOf func(Settings) Settings
	}{
		{name: "another Machine as BackupGuard", cfg: func() update.Config {
			c := e.cfg
			c.BackupGuard = another
			return c
		}, machine: e.machine, settingsOf: identity},
		{name: "a guard that is not a Machine", cfg: func() update.Config {
			c := e.cfg
			c.BackupGuard = otherGuard{}
			return c
		}, machine: e.machine, settingsOf: identity},
		{name: "nil machine", cfg: func() update.Config { return e.cfg }, settingsOf: identity},
		{name: "nil settings function", cfg: func() update.Config { return e.cfg }, machine: e.machine},
		{name: "invalid Config", cfg: func() update.Config { return update.Config{} }, machine: e.machine, settingsOf: identity},
		{name: "invalid Settings", cfg: func() update.Config { return e.cfg }, machine: e.machine,
			settings: Settings{Interval: time.Minute}, settingsOf: identity},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if r, err := NewRunner(tt.cfg(), tt.machine, tt.settings, tt.settingsOf); err == nil {
				t.Fatalf("NewRunner() = %v, nil; want an error", r)
			}
		})
	}
}

// failingSource is an update.Source whose every lookup fails with err.
type failingSource struct{ err error }

func (s failingSource) Latest(context.Context, update.Channel) (update.Release, error) {
	return update.Release{}, s.err
}

func TestCycleSourceFailureWarns(t *testing.T) {
	t.Parallel()
	unreachable := errors.New("source unreachable")
	e := newCycleEnv(t, envOptions{source: failingSource{err: unreachable}, autoApply: true})

	e.runSwallowed()

	rec := e.logged(slog.LevelWarn, msgCheckFailed)
	if err, ok := rec.attrs["err"].Any().(error); !ok || !errors.Is(err, unreachable) {
		t.Errorf("err attr = %v, want %v", rec.attrs["err"], unreachable)
	}
}

func TestCycleOnACorruptRecordWarnsAndChecksNothing(t *testing.T) {
	t.Parallel()
	e := newCycleEnv(t, envOptions{autoApply: true})
	if err := os.WriteFile(e.storePath, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}

	e.runSwallowed()

	e.logged(slog.LevelWarn, msgCheckFailed)
	if n := e.srv.Requests(updatetest.TargetLatest) + e.srv.Requests(updatetest.TargetLatestAPI); n != 0 {
		t.Errorf("latest requests = %d, want 0", n)
	}
}

func TestCycleEndedByItsTimeoutLogsTheCauseAtDebug(t *testing.T) {
	t.Parallel()
	e := newCycleEnv(t, envOptions{autoApply: true})
	ctx, cancel := context.WithCancelCause(t.Context())
	cancel(context.DeadlineExceeded)

	if err := e.r.check.run(ctx); err != nil {
		t.Fatalf("check = %v, want nil", err)
	}

	rec := e.logged(slog.LevelDebug, msgCheckFailed)
	if cause, ok := rec.attrs["cause"].Any().(error); !ok || !errors.Is(cause, context.DeadlineExceeded) {
		t.Errorf("cause attr = %v, want context.DeadlineExceeded", rec.attrs["cause"])
	}
	e.noWarnings()
}

func TestRunnerEndsWithARequiredRestartOnceItHasInstalled(t *testing.T) {
	e := newCycleEnv(t, envOptions{autoApply: true})
	clock := e.r.sched.clock.(armingClock)

	if got := e.r.Name(); got != defaultRunnerName {
		t.Errorf("Name() = %q, want %q", got, defaultRunnerName)
	}
	if err := e.r.Start(t.Context()); err != nil {
		t.Fatalf("Start() = %v", err)
	}
	var delay time.Duration
	select {
	case delay = <-clock.armed:
	case <-time.After(failAfter):
		t.Fatal("the runner armed no timer")
	}
	if err := e.r.Err(); err != nil {
		t.Fatalf("Err() before the end = %v, want nil", err)
	}
	clock.Advance(delay)

	select {
	case <-e.r.Done():
	case <-time.After(failAfter):
		t.Fatal("the runner did not end after installing")
	}
	if err := e.r.Err(); !daemon.RestartRequested(err) {
		t.Fatalf("Err() = %v, want a requested restart", err)
	}
	e.wantTarget(updatetest.ReleaseExecutable(t, newerVersion))
}

func TestCycleWhoseConfigDoesNotResolveWarnsAndChecksNothing(t *testing.T) {
	t.Parallel()
	e := newCycleEnv(t, envOptions{autoApply: true})

	// An empty StateDir is resolved from the environment when the check runs, and with no
	// variable set and no home directory it cannot be.
	cfg := e.cfg
	cfg.StateDir = ""
	cfg.Getenv = func(string) string { return "" }
	cfg.UserHomeDir = func() (string, error) { return "", errors.New("no home directory") }
	r, err := NewRunner(cfg, e.machine, Settings{AutoApply: true}, func(s Settings) Settings { return s },
		WithLogger(slog.New(captureHandler{mu: &e.logMu, records: &e.records})))
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}

	if err := r.check.run(t.Context()); err != nil {
		t.Fatalf("check = %v, want nil", err)
	}

	e.logged(slog.LevelWarn, msgCheckFailed)
	if n := e.srv.Requests(updatetest.TargetLatest) + e.srv.Requests(updatetest.TargetLatestAPI); n != 0 {
		t.Errorf("latest requests = %d, want 0", n)
	}
}

// fixedSource is an update.Source whose latest release is rel.
type fixedSource struct{ rel update.Release }

func (s fixedSource) Latest(context.Context, update.Channel) (update.Release, error) {
	return s.rel, nil
}

func TestCycleReleaseWithoutThePlatformsAssetsWarnsAndDownloadsNothing(t *testing.T) {
	t.Parallel()
	e := newCycleEnv(t, envOptions{
		autoApply: true,
		source:    fixedSource{rel: update.Release{Tag: newerVersion, Assets: []update.Asset{}}},
	})

	e.runSwallowed()

	rec := e.logged(slog.LevelWarn, msgApplyFailed)
	var notFound *update.AssetNotFoundError
	if err, ok := rec.attrs["err"].Any().(error); !ok || !errors.As(err, &notFound) {
		t.Errorf("err attr = %v, want a *update.AssetNotFoundError", rec.attrs["err"])
	}
	if got := e.record().Status; got != StatusIdle {
		t.Errorf("status = %v, want idle", got)
	}
	e.wantNoDownload()
	e.wantTarget(oldBinary())
}

func TestCycleInstallFailingAfterAnotherProcessSettledTheRecordWarns(t *testing.T) {
	t.Parallel()
	e := newCycleEnv(t, envOptions{autoApply: true})

	// Another process settles the pending record between the hook and the failure of the install,
	// so the check finds nothing pending to abandon.
	e.r.check.apply = func(
		ctx context.Context,
		r update.Config,
		sel update.Selection,
		beforeInstall func(context.Context, update.InstallPlan) error,
	) (*update.Installed, error) {
		target, err := r.Target()
		if err != nil {
			return nil, err
		}
		if err := beforeInstall(ctx, update.InstallPlan{Target: target, Backup: target + ".old", Version: sel.Tag}); err != nil {
			return nil, err
		}
		other, err := New(NewFileStore(e.storePath), runningVersion)
		if err != nil {
			return nil, err
		}
		if err := other.Abandon(ctx, "settled elsewhere"); err != nil {
			return nil, err
		}
		return nil, errors.New("rename staged: refused")
	}

	e.runSwallowed()

	e.logged(slog.LevelWarn, msgApplyFailed)
	rec := e.logged(slog.LevelWarn, msgAbandonFailed)
	if err, ok := rec.attrs["err"].Any().(error); !ok || !errors.Is(err, ErrNoPendingUpdate) {
		t.Errorf("err attr = %v, want ErrNoPendingUpdate", rec.attrs["err"])
	}
	if got := e.record(); got.Status != StatusRolledBack || got.Reason != "settled elsewhere" {
		t.Errorf("record = %+v, want rolled_back by the other process", got)
	}
}
