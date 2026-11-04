package daemon

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"
	"time"
)

// defaultCheckTimeout is the per-check timeout when WithCheckTimeout is not given.
const defaultCheckTimeout = 2 * time.Second

// Kind says which question a health check answers. Liveness asks whether the process should be
// restarted; Readiness asks whether it should receive traffic.
type Kind uint8

const (
	// Liveness is a check the process can answer about itself alone. A failing liveness check makes
	// the snapshot down. It holds no downstream dependency, whose outage a restart cannot fix.
	Liveness Kind = iota + 1

	// Readiness is a check of whether the process can serve, downstream dependencies included. A
	// failing readiness check makes the snapshot unready. It is the kind of a check added with none.
	Readiness
)

// valid reports whether k is Liveness or Readiness.
func (k Kind) valid() bool {
	return k == Liveness || k == Readiness
}

// String returns "liveness" or "readiness", the form k takes in JSON.
func (k Kind) String() string {
	switch k {
	case Liveness:
		return "liveness"
	case Readiness:
		return "readiness"
	default:
		return fmt.Sprintf("Kind(%d)", uint8(k))
	}
}

// MarshalText encodes k as "liveness" or "readiness" and rejects any other value.
func (k Kind) MarshalText() ([]byte, error) {
	if !k.valid() {
		return nil, fmt.Errorf("%w: %d", ErrUnknownKind, uint8(k))
	}
	return []byte(k.String()), nil
}

// UnmarshalText decodes "liveness" or "readiness" and rejects any other text.
func (k *Kind) UnmarshalText(text []byte) error {
	switch string(text) {
	case "liveness":
		*k = Liveness
	case "readiness":
		*k = Readiness
	default:
		return fmt.Errorf("%w: %q", ErrUnknownKind, text)
	}
	return nil
}

// Check is one health check. It returns nil when the check passes and an error saying what is
// wrong when it fails. The context is cancelled when the check's timeout expires; a Check that
// ignores it fails at the timeout all the same, and keeps running until it returns. While such a
// run of a check is still running, the check is not run again: every Snapshot taken in the
// meantime reports it as failing with ErrCheckTimeout without calling it, so a Check stuck on
// something it does not let the context interrupt holds one goroutine, not one per Snapshot.
type Check func(ctx context.Context) error

// State is the overall status of a Snapshot.
type State string

const (
	// StateOK is a snapshot in which no check failed.
	StateOK State = "ok"

	// StateUnready is a snapshot in which a readiness check failed and no liveness check did.
	StateUnready State = "unready"

	// StateDown is a snapshot in which a liveness check failed.
	StateDown State = "down"
)

// CheckStatus is the outcome of one check in a Snapshot.
type CheckStatus string

const (
	// CheckPass is a check that returned nil within its timeout.
	CheckPass CheckStatus = "pass"

	// CheckFail is a check that returned an error, panicked or outlasted its timeout.
	CheckFail CheckStatus = "fail"
)

// CheckResult is the outcome of one check in a Snapshot. Kinds is in ascending order, Liveness
// before Readiness. Error is empty when Status is CheckPass. Duration is how long the check ran,
// up to its timeout.
type CheckResult struct {
	Name     string
	Kinds    []Kind
	Status   CheckStatus
	Error    string
	Duration time.Duration
}

// checkResultJSON is the wire form of a CheckResult.
type checkResultJSON struct {
	Name       string      `json:"name"`
	Kinds      []Kind      `json:"kinds"`
	Status     CheckStatus `json:"status"`
	Error      string      `json:"error,omitempty"`
	DurationMS int64       `json:"duration_ms"`
}

// MarshalJSON encodes r with the keys name, kinds, status, error and duration_ms. kinds is an
// array, never null; error is omitted when empty; duration_ms is the duration in whole
// milliseconds, truncated.
func (r CheckResult) MarshalJSON() ([]byte, error) {
	kinds := r.Kinds
	if kinds == nil {
		kinds = []Kind{}
	}
	return json.Marshal(checkResultJSON{
		Name:       r.Name,
		Kinds:      kinds,
		Status:     r.Status,
		Error:      r.Error,
		DurationMS: r.Duration.Milliseconds(),
	})
}

// UnmarshalJSON decodes the form MarshalJSON writes. Duration comes back at millisecond
// resolution.
func (r *CheckResult) UnmarshalJSON(data []byte) error {
	var w checkResultJSON
	if err := json.Unmarshal(data, &w); err != nil {
		return err
	}
	*r = CheckResult{
		Name:     w.Name,
		Kinds:    w.Kinds,
		Status:   w.Status,
		Error:    w.Error,
		Duration: time.Duration(w.DurationMS) * time.Millisecond,
	}
	return nil
}

// Snapshot is the health of a process at one moment: every check's result and the overall
// status they add up to.
//
// Status is StateDown when any liveness check failed, else StateUnready when any readiness check
// failed, else StateOK. Live is true when no liveness check failed. Ready is true when no check
// failed, of either kind, so Ready holds exactly when Status is StateOK: a process that is not
// live is not ready either. StartedAt is when the Health was created and Uptime the time since.
// Checks is sorted by name.
//
// A Snapshot carries no schema field: the envelope that versions it is added where it is written.
type Snapshot struct {
	Status    State
	Live      bool
	Ready     bool
	StartedAt time.Time
	Uptime    time.Duration
	Checks    []CheckResult
}

// snapshotJSON is the wire form of a Snapshot.
type snapshotJSON struct {
	Status        State         `json:"status"`
	Live          bool          `json:"live"`
	Ready         bool          `json:"ready"`
	StartedAt     string        `json:"started_at"`
	UptimeSeconds int64         `json:"uptime_seconds"`
	Checks        []CheckResult `json:"checks"`
}

// MarshalJSON encodes s with the keys status, live, ready, started_at, uptime_seconds and checks.
// started_at is RFC 3339 in UTC to the second; uptime_seconds is the uptime in whole seconds,
// truncated; checks is an array, never null.
func (s Snapshot) MarshalJSON() ([]byte, error) {
	checks := s.Checks
	if checks == nil {
		checks = []CheckResult{}
	}
	return json.Marshal(snapshotJSON{
		Status:        s.Status,
		Live:          s.Live,
		Ready:         s.Ready,
		StartedAt:     s.StartedAt.UTC().Format(time.RFC3339),
		UptimeSeconds: int64(s.Uptime / time.Second),
		Checks:        checks,
	})
}

// UnmarshalJSON decodes the form MarshalJSON writes, ignoring any other key, so the body of an
// envelope that carries schema decodes as well. StartedAt and Uptime come back at second
// resolution.
func (s *Snapshot) UnmarshalJSON(data []byte) error {
	var w snapshotJSON
	if err := json.Unmarshal(data, &w); err != nil {
		return err
	}
	startedAt, err := time.Parse(time.RFC3339, w.StartedAt)
	if err != nil {
		return fmt.Errorf("daemon: snapshot started_at: %w", err)
	}
	*s = Snapshot{
		Status:    w.Status,
		Live:      w.Live,
		Ready:     w.Ready,
		StartedAt: startedAt,
		Uptime:    time.Duration(w.UptimeSeconds) * time.Second,
		Checks:    w.Checks,
	}
	return nil
}

// HealthOption configures a Health in NewHealth.
type HealthOption func(*Health)

// WithCheckTimeout sets how long each check may run before it fails with ErrCheckTimeout. The
// default is two seconds. A timeout that is zero or negative leaves the default in place.
func WithCheckTimeout(timeout time.Duration) HealthOption {
	return func(h *Health) {
		if timeout > 0 {
			h.timeout = timeout
		}
	}
}

// Health is a registry of health checks that runs them all when asked for a Snapshot. Build one
// with NewHealth. Add and Snapshot are safe to call from several goroutines at once.
type Health struct {
	timeout   time.Duration
	startedAt time.Time

	// now is the clock behind StartedAt, Uptime and each check's Duration.
	now func() time.Time

	// mu guards checks.
	mu     sync.Mutex
	checks map[string]registered
}

// registered is one check a Health holds. kinds is sorted and free of duplicates.
type registered struct {
	check Check
	kinds []Kind
	state *checkState
}

// namedCheck is one check to add, as Add receives it, or to run, as Snapshot takes it. state is
// nil until the check is registered.
type namedCheck struct {
	name  string
	check Check
	kinds []Kind
	state *checkState
}

// checkState counts the runs of one registered check that a Snapshot abandoned and that have not
// returned yet. A check is not started again while the count is above zero.
type checkState struct {
	mu        sync.Mutex
	abandoned int

	// idle is closed while abandoned is zero, and replaced by an open channel when it rises from
	// zero.
	idle chan struct{}
}

// newCheckState returns the state of a check with no abandoned run.
func newCheckState() *checkState {
	idle := make(chan struct{})
	close(idle)
	return &checkState{idle: idle}
}

// stuck reports whether an abandoned run of the check has not returned yet.
func (s *checkState) stuck() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.abandoned > 0
}

// idleChan returns a channel that is closed once no abandoned run of the check is still running.
func (s *checkState) idleChan() <-chan struct{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.idle
}

// checkRun is one run of a check, shared between the goroutine running it and the run that waits
// for it. Its fields are guarded by the check's checkState.mu.
type checkRun struct {
	returned  bool
	abandoned bool
}

// abandon records that the waiter stopped waiting for r. It counts r as abandoned unless the check
// has already returned.
func (s *checkState) abandon(r *checkRun) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r.returned {
		return
	}
	r.abandoned = true
	if s.abandoned == 0 {
		s.idle = make(chan struct{})
	}
	s.abandoned++
}

// finish records that the check of r returned, and uncounts r when it was abandoned.
func (s *checkState) finish(r *checkRun) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r.returned = true
	if !r.abandoned {
		return
	}
	s.abandoned--
	if s.abandoned == 0 {
		close(s.idle)
	}
}

// NewHealth returns an empty Health whose uptime counts from now.
func NewHealth(opts ...HealthOption) *Health {
	return newHealth(time.Now, opts...)
}

// newHealth returns an empty Health that reads the time from now.
func newHealth(now func() time.Time, opts ...HealthOption) *Health {
	h := &Health{
		timeout: defaultCheckTimeout,
		now:     now,
		checks:  make(map[string]registered),
	}
	for _, opt := range opts {
		if opt != nil {
			opt(h)
		}
	}
	h.startedAt = h.now()
	return h
}

// Add registers check under name, as a check of each of kinds; with no kinds it is a Readiness
// check, and a kind given twice counts once. It rejects a blank name (ErrBlankCheckName), a name
// already registered (ErrDuplicateCheck), a nil check (ErrNilCheck) and a Kind other than
// Liveness or Readiness (ErrUnknownKind), and then registers nothing.
func (h *Health) Add(name string, check Check, kinds ...Kind) error {
	return h.addAll([]namedCheck{{name: name, check: check, kinds: kinds}})
}

// addAll registers every check in checks, or, when any of them is rejected for a reason Add
// gives, none of them. Two checks in checks with the same name are a duplicate.
func (h *Health) addAll(checks []namedCheck) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	batch := make(map[string]registered, len(checks))
	for _, c := range checks {
		if strings.TrimSpace(c.name) == "" {
			return ErrBlankCheckName
		}
		if c.check == nil {
			return fmt.Errorf("%w: %q", ErrNilCheck, c.name)
		}
		_, held := h.checks[c.name]
		_, repeated := batch[c.name]
		if held || repeated {
			return fmt.Errorf("%w: %q", ErrDuplicateCheck, c.name)
		}
		kinds := []Kind{Readiness}
		if len(c.kinds) > 0 {
			kinds = slices.Clone(c.kinds)
			slices.Sort(kinds)
			kinds = slices.Compact(kinds)
		}
		for _, k := range kinds {
			if !k.valid() {
				return fmt.Errorf("%w: check %q has kind %d", ErrUnknownKind, c.name, uint8(k))
			}
		}
		batch[c.name] = registered{check: c.check, kinds: kinds, state: newCheckState()}
	}
	maps.Copy(h.checks, batch)
	return nil
}

// Snapshot runs every registered check and reports the result. The checks run concurrently, each
// under a context derived from ctx that its timeout bounds. A check that returns an error, panics,
// or is still running when its timeout expires or ctx is done fails; one that outlasts its timeout
// fails with ErrCheckTimeout, and Snapshot returns without waiting for it to finish. Until that
// run returns, the check is not run again: a Snapshot reports it as failing with ErrCheckTimeout
// and a Duration of zero. Snapshot never fails: every problem is the result of a check.
func (h *Health) Snapshot(ctx context.Context) Snapshot {
	return h.snapshot(ctx, 0)
}

// snapshot is Snapshot over the checks of kind only, or of every kind when only is zero. A check
// of both kinds is of each.
func (h *Health) snapshot(ctx context.Context, only Kind) Snapshot {
	h.mu.Lock()
	checks := make([]namedCheck, 0, len(h.checks))
	for name, r := range h.checks {
		if only != 0 && !slices.Contains(r.kinds, only) {
			continue
		}
		checks = append(checks, namedCheck{name: name, check: r.check, kinds: r.kinds, state: r.state})
	}
	h.mu.Unlock()
	slices.SortFunc(checks, func(a, b namedCheck) int { return cmp.Compare(a.name, b.name) })

	results := make([]CheckResult, len(checks))
	var wg sync.WaitGroup
	for i, c := range checks {
		wg.Go(func() { results[i] = h.run(ctx, c) })
	}
	wg.Wait()

	snap := Snapshot{
		Status:    StateOK,
		Live:      true,
		Ready:     true,
		StartedAt: h.startedAt,
		Uptime:    h.now().Sub(h.startedAt),
		Checks:    results,
	}
	for _, r := range results {
		if r.Status != CheckFail {
			continue
		}
		snap.Ready = false
		if slices.Contains(r.Kinds, Liveness) {
			snap.Live = false
		}
	}
	switch {
	case !snap.Live:
		snap.Status = StateDown
	case !snap.Ready:
		snap.Status = StateUnready
	}
	return snap
}

// run runs one check under its timeout and returns its result. The check runs in a goroutine of
// its own, which run abandons when the timeout expires or ctx is done first. A check with an
// abandoned run that has not returned fails at once, without being called.
func (h *Health) run(ctx context.Context, c namedCheck) CheckResult {
	result := CheckResult{Name: c.name, Kinds: slices.Clone(c.kinds), Status: CheckPass}
	if c.state.stuck() {
		result.Status = CheckFail
		result.Error = fmt.Errorf("%w: an earlier run is still running", ErrCheckTimeout).Error()
		return result
	}

	checkCtx, cancel := context.WithTimeoutCause(ctx, h.timeout,
		fmt.Errorf("%w after %v", ErrCheckTimeout, h.timeout))
	defer cancel()

	start := h.now()

	// done is buffered so that an abandoned check can still deliver its error and exit.
	done := make(chan error, 1)
	cr := &checkRun{}
	go func() {
		defer c.state.finish(cr)
		defer func() {
			if v := recover(); v != nil {
				done <- fmt.Errorf("daemon: health check %q panicked: %v", c.name, v)
			}
		}()
		done <- c.check(checkCtx)
	}()

	var err error
	select {
	case err = <-done:
		// A check that gives up because its context ended reports why the context ended.
		if err != nil && checkCtx.Err() != nil {
			err = context.Cause(checkCtx)
		}
	case <-checkCtx.Done():
		err = context.Cause(checkCtx)
		c.state.abandon(cr)
	}
	result.Duration = h.now().Sub(start)
	if err != nil {
		result.Status = CheckFail
		result.Error = err.Error()
	}
	return result
}
