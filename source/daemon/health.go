package daemon

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
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
// Checks is sorted by name. Update is the self-update state from the Health's UpdateReporter, nil
// when it has none or the snapshot was taken for a probe; it never changes Status, Live or Ready.
//
// A Snapshot carries no schema field: the envelope that versions it is added where it is written.
type Snapshot struct {
	Status    State
	Live      bool
	Ready     bool
	StartedAt time.Time
	Uptime    time.Duration
	Checks    []CheckResult
	Update    *UpdateStatus
}

// snapshotJSON is the wire form of a Snapshot.
type snapshotJSON struct {
	Status        State         `json:"status"`
	Live          bool          `json:"live"`
	Ready         bool          `json:"ready"`
	StartedAt     string        `json:"started_at"`
	UptimeSeconds int64         `json:"uptime_seconds"`
	Checks        []CheckResult `json:"checks"`
	Update        *UpdateStatus `json:"update,omitempty"`
}

// MarshalJSON encodes s with the keys status, live, ready, started_at, uptime_seconds, checks and
// update. started_at is RFC 3339 in UTC to the second; uptime_seconds is the uptime in whole
// seconds, truncated; checks is an array, never null; update is omitted when Update is nil.
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
		Update:        s.Update,
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
		Update:    w.Update,
	}
	return nil
}

// UpdateState is where a daemon's self-update stands.
type UpdateState string

const (
	// UpdateIdle is a daemon with no update in progress.
	UpdateIdle UpdateState = "idle"

	// UpdatePending is an update installed and booted that has not confirmed itself yet.
	UpdatePending UpdateState = "pending"

	// UpdateConfirmed is an update that confirmed itself.
	UpdateConfirmed UpdateState = "confirmed"

	// UpdateRolledBack is an update that failed to confirm and was replaced by the binary it
	// updated.
	UpdateRolledBack UpdateState = "rolled_back"

	// UpdateUnknown is an update state that could not be read: the reporter failed, panicked or
	// outlasted its timeout, or the state it gave is none of the others.
	UpdateUnknown UpdateState = "unknown"
)

// valid reports whether s is one of the five UpdateState constants.
func (s UpdateState) valid() bool {
	switch s {
	case UpdateIdle, UpdatePending, UpdateConfirmed, UpdateRolledBack, UpdateUnknown:
		return true
	default:
		return false
	}
}

// known returns s when it is one of the five UpdateState constants and UpdateUnknown otherwise.
// An empty state is therefore unknown, never idle: a daemon that cannot say where its update
// stands must not read as one with no update in progress.
func (s UpdateState) known() UpdateState {
	if s.valid() {
		return s
	}
	return UpdateUnknown
}

// UpdateStatus is the self-update state of a daemon, as a Snapshot carries it. FromVersion and
// ToVersion are the versions an update moves between; BootAttempts counts the boots of the new
// binary that have not confirmed it; Reason says why the update is in its state; StartedAt is when
// the update began and UpdatedAt when its state last changed; Channel and Watermark are the
// release channel and the newest release seen on it; Error is what went wrong reading the state,
// or the update itself. Every field but State is optional.
type UpdateStatus struct {
	State        UpdateState
	FromVersion  string
	ToVersion    string
	BootAttempts int
	Reason       string
	StartedAt    time.Time
	UpdatedAt    time.Time
	Channel      string
	Watermark    string
	Error        string
}

// updateStatusJSON is the wire form of an UpdateStatus.
type updateStatusJSON struct {
	Status       UpdateState `json:"status"`
	FromVersion  string      `json:"from_version,omitzero"`
	ToVersion    string      `json:"to_version,omitzero"`
	BootAttempts int         `json:"boot_attempts,omitzero"`
	Reason       string      `json:"reason,omitzero"`
	StartedAt    string      `json:"started_at,omitzero"`
	UpdatedAt    string      `json:"updated_at,omitzero"`
	Channel      string      `json:"channel,omitzero"`
	Watermark    string      `json:"watermark,omitzero"`
	Error        string      `json:"error,omitzero"`
}

// MarshalJSON encodes u with the key status, always present, then from_version, to_version,
// boot_attempts, reason, started_at, updated_at, channel, watermark and error, each omitted when
// zero. status is "unknown" for a State that is none of the UpdateState constants, the empty
// State included. started_at and updated_at are RFC 3339 in UTC to the second.
func (u UpdateStatus) MarshalJSON() ([]byte, error) {
	return json.Marshal(updateStatusJSON{
		Status:       u.State.known(),
		FromVersion:  u.FromVersion,
		ToVersion:    u.ToVersion,
		BootAttempts: u.BootAttempts,
		Reason:       u.Reason,
		StartedAt:    formatUpdateTime(u.StartedAt),
		UpdatedAt:    formatUpdateTime(u.UpdatedAt),
		Channel:      u.Channel,
		Watermark:    u.Watermark,
		Error:        u.Error,
	})
}

// UnmarshalJSON decodes the form MarshalJSON writes. A status that is missing, empty or none of
// the UpdateState constants decodes as UpdateUnknown. StartedAt and UpdatedAt come back at second
// resolution, in UTC, and zero when absent.
func (u *UpdateStatus) UnmarshalJSON(data []byte) error {
	var w updateStatusJSON
	if err := json.Unmarshal(data, &w); err != nil {
		return err
	}
	startedAt, err := parseUpdateTime(w.StartedAt)
	if err != nil {
		return fmt.Errorf("daemon: update started_at: %w", err)
	}
	updatedAt, err := parseUpdateTime(w.UpdatedAt)
	if err != nil {
		return fmt.Errorf("daemon: update updated_at: %w", err)
	}
	*u = UpdateStatus{
		State:        w.Status.known(),
		FromVersion:  w.FromVersion,
		ToVersion:    w.ToVersion,
		BootAttempts: w.BootAttempts,
		Reason:       w.Reason,
		StartedAt:    startedAt,
		UpdatedAt:    updatedAt,
		Channel:      w.Channel,
		Watermark:    w.Watermark,
		Error:        w.Error,
	}
	return nil
}

// formatUpdateTime is t in RFC 3339 in UTC to the second, or empty when t is zero.
func formatUpdateTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

// parseUpdateTime parses the form formatUpdateTime writes, the empty string as the zero time.
func parseUpdateTime(text string) (time.Time, error) {
	if text == "" {
		return time.Time{}, nil
	}
	t, err := time.Parse(time.RFC3339, text)
	if err != nil {
		return time.Time{}, err
	}
	return t.UTC(), nil
}

// UpdateReporter reports a daemon's self-update state for a Snapshot. It is bounded like a Check:
// the context is cancelled when the check timeout expires, a reporter that panics or outlasts the
// timeout makes the snapshot's Update UpdateUnknown with the Error saying why, and while such a
// call is still running the reporter is not called again: every Snapshot in the meantime reports
// UpdateUnknown without calling it.
type UpdateReporter func(ctx context.Context) UpdateStatus

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

// WithHealthClock sets the clock behind StartedAt, Uptime, each check's Duration and the per-check
// timeout. A test passes a Clock it advances itself, so that a check times out without waiting for
// it. A nil clock is the system clock, which is also the clock without this option.
func WithHealthClock(clock Clock) HealthOption {
	return func(h *Health) {
		if clock == nil {
			clock = realClock{}
		}
		h.clock = clock
	}
}

// WithUpdateReporter sets the reporter whose UpdateStatus a Snapshot carries as Update. It is
// called for Health.Snapshot and the /status route, never for /livez or /readyz, so a slow
// reporter cannot delay a probe. A nil reporter leaves Update nil, as without this option.
func WithUpdateReporter(reporter UpdateReporter) HealthOption {
	return func(h *Health) {
		h.reporter = reporter
	}
}

// Health is a registry of health checks that runs them all when asked for a Snapshot. Build one
// with NewHealth. Add and Snapshot are safe to call from several goroutines at once.
type Health struct {
	timeout   time.Duration
	startedAt time.Time

	// clock is the clock behind StartedAt, Uptime, each check's Duration and its timeout.
	clock Clock

	// reporter is the source of a Snapshot's Update, nil when it has none. reporterState holds its
	// abandoned calls as a check's state holds its abandoned runs.
	reporter      UpdateReporter
	reporterState *checkState

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

// NewHealth returns an empty Health whose uptime counts from now on its clock.
func NewHealth(opts ...HealthOption) *Health {
	h := &Health{
		timeout:       defaultCheckTimeout,
		clock:         realClock{},
		checks:        make(map[string]registered),
		reporterState: newCheckState(),
	}
	for _, opt := range opts {
		if opt != nil {
			opt(h)
		}
	}
	h.startedAt = h.clock.Now()
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
// and a Duration of zero. With an UpdateReporter, Snapshot calls it alongside the checks, under the
// same timeout, and carries its result as Update. Snapshot never fails: every problem is the
// result of a check, or an Update of UpdateUnknown.
func (h *Health) Snapshot(ctx context.Context) Snapshot {
	return h.collect(ctx, 0, true)
}

// snapshot is the Snapshot a probe answers from: over the checks of kind only, or of every kind
// when only is zero, a check of both kinds being of each. It never calls the UpdateReporter, so
// its Update is nil.
func (h *Health) snapshot(ctx context.Context, only Kind) Snapshot {
	return h.collect(ctx, only, false)
}

// collect runs the checks of kind only, or of every kind when only is zero, and, when update is
// true, the UpdateReporter alongside them, and adds up the Snapshot.
func (h *Health) collect(ctx context.Context, only Kind, update bool) Snapshot {
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
	var updateStatus *UpdateStatus
	var wg sync.WaitGroup
	for i, c := range checks {
		wg.Go(func() { results[i] = h.run(ctx, c) })
	}
	if update && h.reporter != nil {
		wg.Go(func() { updateStatus = h.report(ctx) })
	}
	wg.Wait()

	snap := Snapshot{
		Status:    StateOK,
		Live:      true,
		Ready:     true,
		StartedAt: h.startedAt,
		Uptime:    h.clock.Now().Sub(h.startedAt),
		Checks:    results,
		Update:    updateStatus,
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

// run runs one check under its timeout and returns its result. A check with an abandoned run that
// has not returned fails at once, without being called.
func (h *Health) run(ctx context.Context, c namedCheck) CheckResult {
	result := CheckResult{Name: c.name, Kinds: slices.Clone(c.kinds), Status: CheckPass}
	_, duration, err := bounded(h, ctx, c.state, fmt.Sprintf("health check %q", c.name),
		func(ctx context.Context) (struct{}, error) { return struct{}{}, c.check(ctx) })
	result.Duration = duration
	if err != nil {
		result.Status = CheckFail
		result.Error = err.Error()
	}
	return result
}

// report calls the UpdateReporter under the check timeout and returns the status it gives, with a
// State that is none of the UpdateState constants made UpdateUnknown. A reporter that panics, is
// still running when the timeout expires or ctx is done, or has an abandoned call that has not
// returned yields UpdateUnknown with the Error saying why.
func (h *Health) report(ctx context.Context) *UpdateStatus {
	status, _, err := bounded(h, ctx, h.reporterState, "update reporter",
		func(ctx context.Context) (UpdateStatus, error) { return h.reporter(ctx), nil })
	if err != nil {
		return &UpdateStatus{State: UpdateUnknown, Error: err.Error()}
	}
	status.State = status.State.known()
	return &status
}

// callOutcome is what one bounded call of fn delivers from the goroutine running it.
type callOutcome[T any] struct {
	value T
	err   error
}

// bounded calls fn under a context derived from ctx that the check timeout bounds, and returns
// what fn returned and how long it ran, up to the timeout. fn runs in a goroutine of its own, which
// bounded abandons when the timeout expires or ctx is done first; state counts it as abandoned
// until it returns, and while it does bounded fails at once with ErrCheckTimeout and a duration of
// zero, without calling fn. A panic in fn is recovered as an error naming what, the thing fn runs.
func bounded[T any](h *Health, ctx context.Context, state *checkState, what string, fn func(context.Context) (T, error)) (T, time.Duration, error) {
	var zero T
	if state.stuck() {
		return zero, 0, fmt.Errorf("%w: an earlier run is still running", ErrCheckTimeout)
	}

	// The start is read before the deadline is armed, so that the clock cannot reach the deadline
	// before the start is taken and the duration never exceeds the timeout on a clock that only
	// moves when advanced.
	start := h.clock.Now()
	callCtx, cancel := withDeadline(h.clock, ctx, h.timeout)
	defer cancel()

	// done is buffered so that an abandoned call can still deliver its outcome and exit.
	done := make(chan callOutcome[T], 1)
	cr := &checkRun{}
	go func() {
		defer state.finish(cr)
		defer func() {
			if v := recover(); v != nil {
				done <- callOutcome[T]{err: fmt.Errorf("daemon: %s panicked: %v", what, v)}
			}
		}()
		value, err := fn(callCtx)
		done <- callOutcome[T]{value: value, err: err}
	}()

	var out callOutcome[T]
	select {
	case out = <-done:
	case <-callCtx.Done():
		out = callOutcome[T]{err: callCtx.Err()}
		state.abandon(cr)
	}
	// An abandoned call, and one that returns an error after its context has ended, fail with why
	// the context ended.
	if out.err != nil && callCtx.Err() != nil {
		out.err = h.endedErr(ctx, callCtx)
	}
	return out.value, h.clock.Now().Sub(start), out.err
}

// endedErr is what a check fails with once checkCtx, its context under the caller's ctx, has
// ended. The check timed out when checkCtx reports context.DeadlineExceeded while ctx is still
// live, and the error wraps ErrCheckTimeout. Otherwise checkCtx ended with ctx, and the error is
// context.Cause(ctx), which is ctx.Err() when ctx carries no cause, so a caller that cancels with
// a cause sees the check fail with it. checkCtx's own cause is never read: the clock's deadline
// context carries none. A check that returns an error after its context has ended fails with this
// error too, so a check that gives up on its context reports why the context ended.
func (h *Health) endedErr(ctx, checkCtx context.Context) error {
	if ctx.Err() != nil {
		return context.Cause(ctx)
	}
	if errors.Is(checkCtx.Err(), context.DeadlineExceeded) {
		return fmt.Errorf("%w after %v", ErrCheckTimeout, h.timeout)
	}
	return checkCtx.Err()
}
