package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeClock is a clock for newHealth that tests move by hand. It is safe for concurrent use.
type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

func pass(context.Context) error { return nil }

func failWith(msg string) Check {
	return func(context.Context) error { return errors.New(msg) }
}

func TestSnapshotOverallState(t *testing.T) {
	tests := []struct {
		name   string
		add    func(t *testing.T, h *Health)
		status State
		live   bool
		ready  bool
	}{
		{
			name:   "no checks",
			add:    func(*testing.T, *Health) {},
			status: StateOK, live: true, ready: true,
		},
		{
			name: "all pass",
			add: func(t *testing.T, h *Health) {
				mustAdd(t, h, "db", pass)
				mustAdd(t, h, "loop", pass, Liveness)
			},
			status: StateOK, live: true, ready: true,
		},
		{
			name: "readiness fails",
			add: func(t *testing.T, h *Health) {
				mustAdd(t, h, "db", failWith("refused"), Readiness)
				mustAdd(t, h, "loop", pass, Liveness)
			},
			status: StateUnready, live: true, ready: false,
		},
		{
			name: "liveness fails",
			add: func(t *testing.T, h *Health) {
				mustAdd(t, h, "db", pass)
				mustAdd(t, h, "loop", failWith("stuck"), Liveness)
			},
			status: StateDown, live: false, ready: false,
		},
		{
			name: "liveness and readiness fail",
			add: func(t *testing.T, h *Health) {
				mustAdd(t, h, "db", failWith("refused"))
				mustAdd(t, h, "loop", failWith("stuck"), Liveness)
			},
			status: StateDown, live: false, ready: false,
		},
		{
			name: "check of both kinds fails",
			add: func(t *testing.T, h *Health) {
				mustAdd(t, h, "supervisor", failWith("stopping"), Liveness, Readiness)
			},
			status: StateDown, live: false, ready: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := NewHealth()
			tt.add(t, h)
			snap := h.Snapshot(t.Context())
			if snap.Status != tt.status || snap.Live != tt.live || snap.Ready != tt.ready {
				t.Errorf("Snapshot = status %q, live %v, ready %v; want %q, %v, %v",
					snap.Status, snap.Live, snap.Ready, tt.status, tt.live, tt.ready)
			}
		})
	}
}

func mustAdd(t *testing.T, h *Health, name string, check Check, kinds ...Kind) {
	t.Helper()
	if err := h.Add(name, check, kinds...); err != nil {
		t.Fatalf("Add(%q) = %v", name, err)
	}
}

func TestSnapshotCheckResults(t *testing.T) {
	clock := &fakeClock{t: time.Date(2026, 11, 4, 3, 22, 0, 0, time.UTC)}
	h := newHealth(clock.now)
	mustAdd(t, h, "zeta", pass)
	mustAdd(t, h, "alpha", failWith("refused"), Readiness, Liveness, Readiness)
	mustAdd(t, h, "mid", pass, Liveness)
	clock.advance(90*time.Second + 500*time.Millisecond)

	snap := h.Snapshot(t.Context())

	if !snap.StartedAt.Equal(time.Date(2026, 11, 4, 3, 22, 0, 0, time.UTC)) {
		t.Errorf("StartedAt = %v, want the clock at NewHealth", snap.StartedAt)
	}
	if snap.Uptime != 90*time.Second+500*time.Millisecond {
		t.Errorf("Uptime = %v, want 1m30.5s", snap.Uptime)
	}
	want := []CheckResult{
		{Name: "alpha", Kinds: []Kind{Liveness, Readiness}, Status: CheckFail, Error: "refused"},
		{Name: "mid", Kinds: []Kind{Liveness}, Status: CheckPass},
		{Name: "zeta", Kinds: []Kind{Readiness}, Status: CheckPass},
	}
	if len(snap.Checks) != len(want) {
		t.Fatalf("Checks = %+v, want %+v", snap.Checks, want)
	}
	for i := range want {
		got := snap.Checks[i]
		if got.Name != want[i].Name || !slices.Equal(got.Kinds, want[i].Kinds) ||
			got.Status != want[i].Status || got.Error != want[i].Error || got.Duration != 0 {
			t.Errorf("Checks[%d] = %+v, want %+v", i, got, want[i])
		}
	}
}

func TestSnapshotTimeout(t *testing.T) {
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })

	h := NewHealth(WithCheckTimeout(time.Millisecond))
	mustAdd(t, h, "ignores-context", func(context.Context) error {
		<-release
		return nil
	})
	mustAdd(t, h, "honours-context", func(ctx context.Context) error {
		<-ctx.Done()
		return ctx.Err()
	})

	snap := h.Snapshot(t.Context())

	if snap.Status != StateUnready {
		t.Errorf("Status = %q, want %q", snap.Status, StateUnready)
	}
	for _, r := range snap.Checks {
		if r.Status != CheckFail {
			t.Errorf("check %q: Status = %q, want %q", r.Name, r.Status, CheckFail)
		}
		if !strings.HasPrefix(r.Error, ErrCheckTimeout.Error()) {
			t.Errorf("check %q: Error = %q, want the timeout error", r.Name, r.Error)
		}
		if r.Duration < time.Millisecond {
			t.Errorf("check %q: Duration = %v, want at least the timeout", r.Name, r.Duration)
		}
	}
}

func TestSnapshotDoesNotRerunCheckWithAbandonedRun(t *testing.T) {
	release, returned := make(chan struct{}), make(chan struct{}, 1)
	var calls atomic.Int32
	h := NewHealth(WithCheckTimeout(time.Millisecond))
	mustAdd(t, h, "stuck", func(context.Context) error {
		defer func() { returned <- struct{}{} }()
		if calls.Add(1) == 1 {
			<-release
		}
		return nil
	})
	state := h.checks["stuck"].state

	first := h.Snapshot(t.Context()).Checks[0]
	if first.Status != CheckFail || !strings.HasPrefix(first.Error, ErrCheckTimeout.Error()) {
		t.Fatalf("first run = %+v, want a fail with the timeout", first)
	}

	second := h.Snapshot(t.Context()).Checks[0]
	if second.Status != CheckFail || !strings.Contains(second.Error, "still running") {
		t.Errorf("run during the abandoned one = %+v, want a fail naming the run still running", second)
	}
	if !strings.HasPrefix(second.Error, ErrCheckTimeout.Error()) {
		t.Errorf("run during the abandoned one: Error = %q, want the timeout error", second.Error)
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("check called %d times while its abandoned run was still running, want 1", got)
	}

	close(release)
	<-returned
	<-state.idleChan()

	third := h.Snapshot(t.Context()).Checks[0]
	if third.Status != CheckPass {
		t.Errorf("run after the abandoned one returned = %+v, want a pass", third)
	}
	if got := calls.Load(); got != 2 {
		t.Errorf("check called %d times, want 2", got)
	}
}

func TestSnapshotCancelledContext(t *testing.T) {
	h := NewHealth()
	mustAdd(t, h, "waits", func(ctx context.Context) error {
		<-ctx.Done()
		return ctx.Err()
	})
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	snap := h.Snapshot(ctx)

	if got := snap.Checks[0]; got.Status != CheckFail || got.Error != context.Canceled.Error() {
		t.Errorf("check = %+v, want a fail with %q", got, context.Canceled)
	}
}

func TestSnapshotPanic(t *testing.T) {
	h := NewHealth()
	mustAdd(t, h, "boom", func(context.Context) error { panic("kaboom") }, Liveness)
	mustAdd(t, h, "fine", pass)

	snap := h.Snapshot(t.Context())

	if snap.Status != StateDown {
		t.Errorf("Status = %q, want %q", snap.Status, StateDown)
	}
	got := snap.Checks[0]
	if got.Status != CheckFail || !strings.Contains(got.Error, "panicked: kaboom") {
		t.Errorf("check = %+v, want a fail naming the panic", got)
	}
	if snap.Checks[1].Status != CheckPass {
		t.Errorf("check %q = %q, want %q", snap.Checks[1].Name, snap.Checks[1].Status, CheckPass)
	}
}

func TestSnapshotRunsChecksConcurrently(t *testing.T) {
	// Each check returns only once the other has started, so running them in turn deadlocks.
	a, b := make(chan struct{}), make(chan struct{})
	h := NewHealth()
	mustAdd(t, h, "a", func(context.Context) error {
		close(a)
		<-b
		return nil
	})
	mustAdd(t, h, "b", func(context.Context) error {
		close(b)
		<-a
		return nil
	})

	if snap := h.Snapshot(t.Context()); snap.Status != StateOK {
		t.Errorf("Snapshot = %+v, want both checks to pass", snap)
	}
}

func TestAddRejects(t *testing.T) {
	tests := []struct {
		name      string
		checkName string
		check     Check
		kinds     []Kind
		want      error
	}{
		{name: "empty name", checkName: "", check: pass, want: ErrBlankCheckName},
		{name: "blank name", checkName: " \t", check: pass, want: ErrBlankCheckName},
		{name: "duplicate name", checkName: "taken", check: pass, want: ErrDuplicateCheck},
		{name: "nil check", checkName: "new", check: nil, want: ErrNilCheck},
		{name: "zero kind", checkName: "new", check: pass, kinds: []Kind{0}, want: ErrUnknownKind},
		{name: "unknown kind", checkName: "new", check: pass, kinds: []Kind{Readiness, 7}, want: ErrUnknownKind},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := NewHealth()
			mustAdd(t, h, "taken", pass)

			if err := h.Add(tt.checkName, tt.check, tt.kinds...); !errors.Is(err, tt.want) {
				t.Errorf("Add = %v, want %v", err, tt.want)
			}
			if snap := h.Snapshot(t.Context()); len(snap.Checks) != 1 {
				t.Errorf("Checks = %+v, want only the check added before", snap.Checks)
			}
		})
	}
}

func TestAddAllIsAtomic(t *testing.T) {
	h := NewHealth()
	mustAdd(t, h, "taken", pass)

	err := h.addAll([]namedCheck{
		{name: "first", check: pass},
		{name: "taken", check: pass},
	})
	if !errors.Is(err, ErrDuplicateCheck) {
		t.Fatalf("addAll = %v, want %v", err, ErrDuplicateCheck)
	}
	err = h.addAll([]namedCheck{
		{name: "twice", check: pass},
		{name: "twice", check: pass},
	})
	if !errors.Is(err, ErrDuplicateCheck) {
		t.Fatalf("addAll = %v, want %v", err, ErrDuplicateCheck)
	}
	if snap := h.Snapshot(t.Context()); len(snap.Checks) != 1 {
		t.Errorf("Checks = %+v, want only the check added before", snap.Checks)
	}

	if err := h.addAll([]namedCheck{
		{name: "supervisor", check: pass, kinds: []Kind{Liveness, Readiness}},
		{name: "db", check: pass},
	}); err != nil {
		t.Fatalf("addAll = %v", err)
	}
	if snap := h.Snapshot(t.Context()); len(snap.Checks) != 3 {
		t.Errorf("Checks = %+v, want three", snap.Checks)
	}
}

func TestAddAndSnapshotConcurrently(t *testing.T) {
	h := NewHealth()
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Go(func() {
			if err := h.Add(fmt.Sprintf("check-%d", i), pass); err != nil {
				t.Errorf("Add = %v", err)
			}
		})
		wg.Go(func() { h.Snapshot(t.Context()) })
	}
	wg.Wait()

	if snap := h.Snapshot(t.Context()); len(snap.Checks) != 8 {
		t.Errorf("Checks = %d, want 8", len(snap.Checks))
	}
}

func TestWithCheckTimeout(t *testing.T) {
	tests := []struct {
		name    string
		timeout time.Duration
		want    time.Duration
	}{
		{name: "positive", timeout: time.Minute, want: time.Minute},
		{name: "zero keeps the default", timeout: 0, want: defaultCheckTimeout},
		{name: "negative keeps the default", timeout: -time.Second, want: defaultCheckTimeout},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NewHealth(WithCheckTimeout(tt.timeout)).timeout; got != tt.want {
				t.Errorf("timeout = %v, want %v", got, tt.want)
			}
		})
	}
}

// fixedSnapshot is a Snapshot whose JSON form snapshotJSONBytes pins.
func fixedSnapshot() Snapshot {
	return Snapshot{
		Status:    StateUnready,
		Live:      true,
		Ready:     false,
		StartedAt: time.Date(2026, 11, 4, 4, 22, 7, 900_000_000, time.FixedZone("CET", 3600)),
		Uptime:    3*time.Minute + 59*time.Second + 999*time.Millisecond,
		Checks: []CheckResult{
			{Name: "db", Kinds: []Kind{Readiness}, Status: CheckFail, Error: "connection refused", Duration: 12*time.Millisecond + 999*time.Microsecond},
			{Name: "supervisor", Kinds: []Kind{Liveness, Readiness}, Status: CheckPass, Duration: 400 * time.Microsecond},
		},
	}
}

const snapshotJSONBytes = `{"status":"unready","live":true,"ready":false,"started_at":"2026-11-04T03:22:07Z",` +
	`"uptime_seconds":239,"checks":[` +
	`{"name":"db","kinds":["readiness"],"status":"fail","error":"connection refused","duration_ms":12},` +
	`{"name":"supervisor","kinds":["liveness","readiness"],"status":"pass","duration_ms":0}]}`

func TestSnapshotMarshalJSON(t *testing.T) {
	got, err := json.Marshal(fixedSnapshot())
	if err != nil {
		t.Fatalf("Marshal = %v", err)
	}
	if string(got) != snapshotJSONBytes {
		t.Errorf("Marshal =\n%s\nwant\n%s", got, snapshotJSONBytes)
	}
}

func TestSnapshotMarshalJSONEmptyChecks(t *testing.T) {
	got, err := json.Marshal(Snapshot{Status: StateOK, Live: true, Ready: true, StartedAt: time.Unix(0, 0)})
	if err != nil {
		t.Fatalf("Marshal = %v", err)
	}
	const want = `{"status":"ok","live":true,"ready":true,"started_at":"1970-01-01T00:00:00Z","uptime_seconds":0,"checks":[]}`
	if string(got) != want {
		t.Errorf("Marshal = %s, want %s", got, want)
	}
}

func TestCheckResultMarshalJSONRejectsUnknownKind(t *testing.T) {
	if _, err := json.Marshal(CheckResult{Name: "x", Kinds: []Kind{9}}); !errors.Is(err, ErrUnknownKind) {
		t.Errorf("Marshal = %v, want %v", err, ErrUnknownKind)
	}
}

func TestSnapshotJSONRoundTrip(t *testing.T) {
	// The body of /status is the snapshot inside an envelope that carries schema first.
	body := `{"schema":1,` + strings.TrimPrefix(snapshotJSONBytes, "{")

	var got Snapshot
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatalf("Unmarshal = %v", err)
	}

	want := fixedSnapshot()
	if got.Status != want.Status || got.Live != want.Live || got.Ready != want.Ready {
		t.Errorf("Unmarshal = status %q, live %v, ready %v; want %q, %v, %v",
			got.Status, got.Live, got.Ready, want.Status, want.Live, want.Ready)
	}
	if !got.StartedAt.Equal(want.StartedAt.Truncate(time.Second)) {
		t.Errorf("StartedAt = %v, want %v to the second", got.StartedAt, want.StartedAt)
	}
	if got.Uptime != want.Uptime.Truncate(time.Second) {
		t.Errorf("Uptime = %v, want %v to the second", got.Uptime, want.Uptime)
	}
	if len(got.Checks) != len(want.Checks) {
		t.Fatalf("Checks = %+v, want %+v", got.Checks, want.Checks)
	}
	for i, w := range want.Checks {
		g := got.Checks[i]
		if g.Name != w.Name || !slices.Equal(g.Kinds, w.Kinds) || g.Status != w.Status ||
			g.Error != w.Error || g.Duration != w.Duration.Truncate(time.Millisecond) {
			t.Errorf("Checks[%d] = %+v, want %+v to the millisecond", i, g, w)
		}
	}

	again, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("Marshal = %v", err)
	}
	if string(again) != snapshotJSONBytes {
		t.Errorf("Marshal after Unmarshal =\n%s\nwant\n%s", again, snapshotJSONBytes)
	}
}

func TestSnapshotUnmarshalJSONRejects(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "bad started_at", body: `{"started_at":"yesterday"}`},
		{name: "unknown kind", body: `{"started_at":"2026-11-04T03:22:07Z","checks":[{"kinds":["startup"]}]}`},
		{name: "not an object", body: `[]`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var s Snapshot
			if err := json.Unmarshal([]byte(tt.body), &s); err == nil {
				t.Errorf("Unmarshal succeeded, want an error")
			}
		})
	}
}

func TestKindString(t *testing.T) {
	for k, want := range map[Kind]string{Liveness: "liveness", Readiness: "readiness", 0: "Kind(0)"} {
		if got := k.String(); got != want {
			t.Errorf("Kind(%d).String() = %q, want %q", uint8(k), got, want)
		}
	}
}

func TestCheckResultMarshalJSONNilKindsIsAnEmptyArray(t *testing.T) {
	got, err := json.Marshal(CheckResult{Name: "x", Status: CheckPass})
	if err != nil {
		t.Fatalf("Marshal = %v", err)
	}
	const want = `{"name":"x","kinds":[],"status":"pass","duration_ms":0}`
	if string(got) != want {
		t.Errorf("Marshal = %s, want %s", got, want)
	}
}

func TestCheckStateStartsIdle(t *testing.T) {
	s := newCheckState()
	if s.stuck() {
		t.Error("stuck() = true for a check that never ran")
	}
	if !isClosed(s.idleChan()) {
		t.Error("idleChan() is open for a check that never ran, want it closed")
	}
}

func TestCheckStateAbandonAfterFinishCountsNothing(t *testing.T) {
	s := newCheckState()
	r := &checkRun{}
	s.finish(r)
	s.abandon(r)
	if r.abandoned {
		t.Error("a run abandoned after it returned is marked abandoned")
	}
	if s.stuck() {
		t.Error("stuck() = true after abandoning a run that had returned")
	}
	if !isClosed(s.idleChan()) {
		t.Error("idleChan() is open after abandoning a run that had returned, want it closed")
	}
}

func TestCheckStateAbandonBeforeFinishIsStuckUntilFinish(t *testing.T) {
	s := newCheckState()
	r := &checkRun{}
	s.abandon(r)
	idle := s.idleChan()
	if !s.stuck() || isClosed(idle) {
		t.Fatalf("after abandon: stuck() = %v, idle closed = %v, want stuck and open", s.stuck(), isClosed(idle))
	}
	s.finish(r)
	if s.stuck() || !isClosed(idle) {
		t.Errorf("after finish: stuck() = %v, idle closed = %v, want idle", s.stuck(), isClosed(idle))
	}
}

func TestCheckErrReportsTheCauseOfAnEndedContext(t *testing.T) {
	errCause := errors.New("check deadline")
	errCheck := errors.New("query cancelled")
	ended, cancel := context.WithCancelCause(context.Background())
	cancel(errCause)

	tests := []struct {
		name string
		ctx  context.Context
		err  error
		want error
	}{
		{name: "error under an ended context", ctx: ended, err: errCheck, want: errCause},
		{name: "error under a live context", ctx: context.Background(), err: errCheck, want: errCheck},
		{name: "nil under an ended context", ctx: ended, err: nil, want: nil},
		{name: "nil under a live context", ctx: context.Background(), err: nil, want: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := checkErr(tt.ctx, tt.err); got != tt.want {
				t.Errorf("checkErr = %v, want %v", got, tt.want)
			}
		})
	}
}
