package update

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"go.vipengele.org/filelock"
	"go.vipengele.org/update/updatetest"
)

// bgNow is the fixed time of every background check test.
func bgNow() time.Time {
	return time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
}

// loadResult is one scripted answer of a fakeStore's Load.
type loadResult struct {
	st  State
	err error
}

// fakeStore is a StateStore whose Load answers, in order, with loads, repeating the last one
// once they run out, and that records every Save.
type fakeStore struct {
	mu        sync.Mutex
	loads     []loadResult
	loadCalls int
	saves     []State
}

func (s *fakeStore) Load(context.Context) (State, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.loadCalls++
	if len(s.loads) == 0 {
		return State{}, nil
	}
	i := min(s.loadCalls, len(s.loads)) - 1
	return s.loads[i].st, s.loads[i].err
}

func (s *fakeStore) Save(_ context.Context, st State) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.saves = append(s.saves, st)
	return nil
}

// recordedSaves returns a copy of every State saved so far.
func (s *fakeStore) recordedSaves() []State {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]State(nil), s.saves...)
}

// countingLocker counts its Lock calls and delegates them to next. TryLock is not counted.
type countingLocker struct {
	calls atomic.Int32
	next  filelock.Locker
}

func (l *countingLocker) Lock(ctx context.Context) (func(), error) {
	l.calls.Add(1)
	return l.next.Lock(ctx)
}

func (l *countingLocker) TryLock() (func(), error) { return l.next.TryLock() }

// heldLocker is a lock another process holds: Lock blocks until ctx is done and TryLock fails.
type heldLocker struct{}

func (heldLocker) Lock(ctx context.Context) (func(), error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func (heldLocker) TryLock() (func(), error) {
	return nil, fmt.Errorf("heldLocker: %w", filelock.ErrLocked)
}

// errSource is a Source whose every lookup fails.
type errSource struct{}

func (errSource) Latest(context.Context, Channel) (Release, error) {
	return Release{}, errors.New("errSource: unreachable")
}

// bgConfig is an eligible Config for a background check of srv: release version v1.0.0, a
// terminal, no CI and no opt-out whatever the host environment holds, srv's signing key, the
// fixed clock bgNow, the fake store and a lock that is always free.
func bgConfig(t *testing.T, srv *updatetest.Server, store *fakeStore) Config {
	t.Helper()
	c := validConfig(t.TempDir())
	c.Source = kitSource(srv)
	c.TrustedKeys = []string{srv.PublicKey()}
	c.Version = "v1.0.0"
	c.Getenv = envOf(nil)
	c.IsTerminal = func() bool { return true }
	c.Now = bgNow
	c.StateStore = store
	c.Locker = filelock.NewNoop()
	c.CheckWait = maxCheckWait
	return c
}

// latestRequests returns how many latest-release lookups srv has received, web and API.
func latestRequests(srv *updatetest.Server) int {
	return srv.Requests(updatetest.TargetLatest) + srv.Requests(updatetest.TargetLatestAPI)
}

// start calls c.Start and fails t on an error.
func start(t *testing.T, c Config) *Check {
	t.Helper()
	k, err := c.Start(context.Background())
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	return k
}

// finishWithin calls k.Finish and fails t when it takes bound or longer, returning how long it
// took.
func finishWithin(t *testing.T, k *Check, bound time.Duration) time.Duration {
	t.Helper()
	begin := time.Now()
	k.Finish()
	elapsed := time.Since(begin)
	if elapsed >= bound {
		t.Errorf("Finish() took %v, want under %v", elapsed, bound)
	}
	return elapsed
}

// assertNoNotice fails t when k reports a notice.
func assertNoNotice(t *testing.T, k *Check) {
	t.Helper()
	if n, ok := k.Notice(); ok {
		t.Errorf("Notice() = %+v, true; want false", n)
	}
}

// assertNotice fails t unless k reports the notice of latest over v1.0.0.
func assertNotice(t *testing.T, k *Check, latest string) {
	t.Helper()
	n, ok := k.Notice()
	want := Notice{Name: "atk", Current: "v1.0.0", Latest: latest}
	if !ok || n != want {
		t.Errorf("Notice() = %+v, %v; want %+v, true", n, ok, want)
	}
}

func TestStartSlowServerNeverDelaysExit(t *testing.T) {
	t.Parallel()
	gate := make(chan struct{})
	srv := newKitServer(t, updatetest.Slow(updatetest.TargetLatest, gate))
	store := &fakeStore{}
	c := bgConfig(t, srv, store)
	c.CheckWait = 50 * time.Millisecond

	begin := time.Now()
	k := start(t, c)
	if elapsed := time.Since(begin); elapsed >= time.Second {
		t.Errorf("Start() took %v, want it to return at once", elapsed)
	}
	select {
	case <-srv.Blocked(updatetest.TargetLatest):
	case <-time.After(10 * time.Second):
		t.Fatal("the check never reached the server")
	}

	begin = time.Now()
	assertNoNotice(t, k)
	if elapsed := time.Since(begin); elapsed >= time.Second {
		t.Errorf("Notice() took %v, want it to return at once", elapsed)
	}
	finishWithin(t, k, time.Second)

	if saves := store.recordedSaves(); len(saves) != 0 {
		t.Errorf("Save called with %+v, want no save for a cancelled check", saves)
	}
}

func TestStartFastServerSavesAndAnnounces(t *testing.T) {
	t.Parallel()
	srv := newKitServer(t)
	store := &fakeStore{loads: []loadResult{{st: State{LatestVersion: "v1.1.0"}}}}
	k := start(t, bgConfig(t, srv, store))

	finishWithin(t, k, 10*time.Second)

	saves := store.recordedSaves()
	if len(saves) != 1 {
		t.Fatalf("Save called %d times, want 1", len(saves))
	}
	if want := (State{LastAttempt: bgNow(), LatestVersion: "v1.2.3"}); !saves[0].LastAttempt.Equal(want.LastAttempt) ||
		saves[0].LatestVersion != want.LatestVersion {
		t.Errorf("saved %+v, want %+v", saves[0], want)
	}
	assertNotice(t, k, "v1.2.3")
}

func TestNoticeFallsBackToTheSavedVersion(t *testing.T) {
	t.Parallel()
	gate := make(chan struct{})
	srv := newKitServer(t, updatetest.Slow(updatetest.TargetLatest, gate))
	store := &fakeStore{loads: []loadResult{{st: State{LatestVersion: "v1.1.0"}}}}
	c := bgConfig(t, srv, store)
	c.CheckWait = 10 * time.Millisecond
	k := start(t, c)

	<-srv.Blocked(updatetest.TargetLatest)
	assertNotice(t, k, "v1.1.0")
	k.Finish()
	assertNotice(t, k, "v1.1.0")
}

func TestNoticePicksTheHigherVersion(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		saved string
		want  string
	}{
		{"fresh above saved", "v1.1.0", "v1.2.3"},
		{"saved above fresh", "v2.0.0", "v2.0.0"},
		{"saved not a release", "garbage", "v1.2.3"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv := newKitServer(t)
			store := &fakeStore{loads: []loadResult{{st: State{LatestVersion: tc.saved}}}}
			k := start(t, bgConfig(t, srv, store))
			k.Finish()
			assertNotice(t, k, tc.want)
		})
	}
}

func TestNoticeIsSilentForAnEqualOrOlderRelease(t *testing.T) {
	t.Parallel()
	for _, running := range []string{"v1.2.3", "v1.3.0"} {
		t.Run(running, func(t *testing.T) {
			t.Parallel()
			srv := newKitServer(t)
			store := &fakeStore{loads: []loadResult{{st: State{LatestVersion: "v1.1.0"}}}}
			c := bgConfig(t, srv, store)
			c.Version = running
			k := start(t, c)
			k.Finish()
			if latestRequests(srv) == 0 {
				t.Error("the check made no request, want one")
			}
			assertNoNotice(t, k)
		})
	}
}

func TestStartIneligibleStartsNothing(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		modify func(*Config)
		last   time.Time
	}{
		{name: "dev build", modify: func(c *Config) { c.Version = "dev" }},
		{name: "in CI", modify: func(c *Config) { c.Getenv = envOf(map[string]string{"CI": "true"}) }},
		{name: "opted out", modify: func(c *Config) { c.Getenv = envOf(map[string]string{"ATK_NO_UPDATE_CHECK": "1"}) }},
		{name: "disabled", modify: func(c *Config) { c.DisableCheck = true }},
		{name: "not a terminal", modify: func(c *Config) { c.IsTerminal = func() bool { return false } }},
		{name: "throttled", modify: func(*Config) {}, last: bgNow().Add(-time.Hour)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv := newKitServer(t)
			store := &fakeStore{loads: []loadResult{{st: State{LastAttempt: tc.last, LatestVersion: "v9.0.0"}}}}
			locker := &countingLocker{next: filelock.NewNoop()}
			c := bgConfig(t, srv, store)
			c.Locker = locker
			tc.modify(&c)

			k := start(t, c)
			assertNoNotice(t, k)
			finishWithin(t, k, 10*time.Millisecond)

			if n := latestRequests(srv); n != 0 {
				t.Errorf("server received %d requests, want none", n)
			}
			if n := locker.calls.Load(); n != 0 {
				t.Errorf("Lock called %d times, want none", n)
			}
			if saves := store.recordedSaves(); len(saves) != 0 {
				t.Errorf("Save called with %+v, want none", saves)
			}
		})
	}
}

func TestStartReturnsAnInvalidConfigsError(t *testing.T) {
	t.Parallel()
	c := bgConfig(t, newKitServer(t), &fakeStore{})
	c.CheckWait = -time.Second
	k, err := c.Start(context.Background())
	if err == nil {
		t.Fatal("Start() error = nil, want the validation error")
	}
	if k != nil {
		t.Errorf("Start() = %p, want nil with the error", k)
	}
}

func TestStartCountsALoadErrorAsZeroState(t *testing.T) {
	t.Parallel()
	srv := newKitServer(t)
	store := &fakeStore{loads: []loadResult{{err: errors.New("unreadable")}, {}}}
	k := start(t, bgConfig(t, srv, store))
	k.Finish()

	if n := latestRequests(srv); n == 0 {
		t.Error("server received no request, want the check to run")
	}
	assertNotice(t, k, "v1.2.3")
}

func TestFinishReturnsAtTheBoundWhileTheLockIsHeld(t *testing.T) {
	t.Parallel()
	srv := newKitServer(t)
	store := &fakeStore{}
	c := bgConfig(t, srv, store)
	c.Locker = heldLocker{}
	c.CheckWait = 50 * time.Millisecond
	k := start(t, c)

	if elapsed := finishWithin(t, k, time.Second); elapsed < c.CheckWait {
		t.Errorf("Finish() took %v, want it to wait the CheckWait %v first", elapsed, c.CheckWait)
	}
	if n := latestRequests(srv); n != 0 {
		t.Errorf("server received %d requests, want none", n)
	}
	if saves := store.recordedSaves(); len(saves) != 0 {
		t.Errorf("Save called with %+v, want none", saves)
	}
	assertNoNotice(t, k)
}

func TestCheckRethrottlesUnderTheLock(t *testing.T) {
	t.Parallel()
	srv := newKitServer(t)
	store := &fakeStore{loads: []loadResult{
		{st: State{LatestVersion: "v1.1.0"}},
		{st: State{LastAttempt: bgNow(), LatestVersion: "v1.5.0"}},
	}}
	k := start(t, bgConfig(t, srv, store))
	finishWithin(t, k, 10*time.Second)

	if n := latestRequests(srv); n != 0 {
		t.Errorf("server received %d requests, want none after another process checked", n)
	}
	if saves := store.recordedSaves(); len(saves) != 0 {
		t.Errorf("Save called with %+v, want none", saves)
	}
	assertNotice(t, k, "v1.5.0")
}

func TestCheckAbandonsOnAReloadError(t *testing.T) {
	t.Parallel()
	srv := newKitServer(t)
	store := &fakeStore{loads: []loadResult{{}, {err: errors.New("unreadable")}}}
	k := start(t, bgConfig(t, srv, store))
	finishWithin(t, k, 10*time.Second)

	if n := latestRequests(srv); n != 0 {
		t.Errorf("server received %d requests, want none", n)
	}
	if saves := store.recordedSaves(); len(saves) != 0 {
		t.Errorf("Save called with %+v, want none", saves)
	}
	assertNoNotice(t, k)
}

func TestCheckRecordsAFailedAttempt(t *testing.T) {
	t.Parallel()
	store := &fakeStore{loads: []loadResult{{st: State{LastAttempt: bgNow().Add(-48 * time.Hour), LatestVersion: "v1.1.0"}}}}
	c := bgConfig(t, newKitServer(t), store)
	c.Source = errSource{}
	k := start(t, c)
	finishWithin(t, k, 10*time.Second)

	saves := store.recordedSaves()
	if len(saves) != 1 {
		t.Fatalf("Save called %d times, want 1", len(saves))
	}
	if !saves[0].LastAttempt.Equal(bgNow()) || saves[0].LatestVersion != "v1.1.0" {
		t.Errorf("saved %+v, want LastAttempt %v and LatestVersion v1.1.0", saves[0], bgNow())
	}
	assertNotice(t, k, "v1.1.0")
}

func TestFinishIsSafeConcurrently(t *testing.T) {
	t.Parallel()
	gate := make(chan struct{})
	srv := newKitServer(t, updatetest.Slow(updatetest.TargetLatest, gate))
	c := bgConfig(t, srv, &fakeStore{})
	c.CheckWait = 20 * time.Millisecond
	k := start(t, c)

	var wg sync.WaitGroup
	for range 2 {
		wg.Go(k.Finish)
	}
	wg.Wait()
	finishWithin(t, k, 10*time.Millisecond)
}

func TestNilCheckIsInert(t *testing.T) {
	t.Parallel()
	var k *Check
	assertNoNotice(t, k)
	k.Finish()
	k.cancelNow()
}

func TestCancelNowSkipsTheWait(t *testing.T) {
	t.Parallel()
	gate := make(chan struct{})
	srv := newKitServer(t, updatetest.Slow(updatetest.TargetLatest, gate))
	store := &fakeStore{}
	k := start(t, bgConfig(t, srv, store))
	<-srv.Blocked(updatetest.TargetLatest)

	begin := time.Now()
	k.cancelNow()
	if elapsed := time.Since(begin); elapsed >= time.Second {
		t.Errorf("cancelNow() took %v, want under the CheckWait %v", elapsed, maxCheckWait)
	}
	finishWithin(t, k, 10*time.Millisecond)
	if saves := store.recordedSaves(); len(saves) != 0 {
		t.Errorf("Save called with %+v, want none", saves)
	}
}

func TestCancelNowCutsAWaitingFinishShort(t *testing.T) {
	t.Parallel()
	gate := make(chan struct{})
	srv := newKitServer(t, updatetest.Slow(updatetest.TargetLatest, gate))
	k := start(t, bgConfig(t, srv, &fakeStore{}))
	<-srv.Blocked(updatetest.TargetLatest)

	finished := make(chan time.Duration)
	go func() {
		begin := time.Now()
		k.Finish()
		finished <- time.Since(begin)
	}()
	k.cancelNow()
	if elapsed := <-finished; elapsed >= maxCheckWait {
		t.Errorf("Finish() took %v, want cancelNow to end its wait before the CheckWait %v", elapsed, maxCheckWait)
	}
}
