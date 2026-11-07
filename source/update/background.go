package update

import (
	"context"
	"errors"
	"sync"
	"time"

	"go.vipengele.org/cli/spec/version"
)

// Check is an update check running in the background, which [Config.Start] begins. A nil *Check
// is valid: its Notice reports nothing and its Finish returns at once.
//
// The check's result is published by closing done: result is written before the close and read
// only after it, so the hot path takes no lock.
type Check struct {
	// name is the tool's name, for the Notice.
	name string
	// current is the running program's version.
	current string
	// saved is the LatestVersion the state held when the check started, or empty when it was
	// learnt on another channel than the Config's.
	saved string
	// highestApplied is the State's HighestApplied when the check started: no candidate below it is
	// announced.
	highestApplied string
	// wait is the longest Finish waits before cancelling the check.
	wait time.Duration
	// cancel stops the check's goroutine.
	cancel context.CancelFunc
	// done is closed once the goroutine has returned.
	done chan struct{}
	// result is the newest release version this run learnt, or empty when it learnt none. It is
	// read only after done is closed.
	result string
	// instead is the Instead of the notice of the release this run announces, from the managed
	// install check, or empty when the install is not managed, the check failed, the check's
	// context ended first or there is no release to announce. It is read only after done is closed.
	instead string
	// once makes the wait and the join of Finish and cancelNow happen once.
	once sync.Once
}

// Start begins an update check in the background and returns at once. It returns an error only
// when c is invalid, as [Config.Resolved] reports it.
//
// Start loads the Update state through the StateStore under ctx; a Load error counts as the zero
// State. A LatestVersion learnt on a channel other than Config.Channel is unknown: it is never
// announced, and its LastAttempt does not throttle the check. When the check is ineligible (a dev build, in CI, stderr not a terminal, disabled, opted
// out, or within CheckInterval of the last attempt) it returns a nil *Check and starts nothing,
// so no notice is shown in that run.
//
// Otherwise the check runs on a goroutine whose context is ctx without its cancellation, plus a
// cancel of its own that [Check.Finish] calls. The goroutine takes the Locker's lock, reloads the
// state and applies the throttle again, since another process may have checked meanwhile; when it
// has, the reloaded LatestVersion is this run's result and nothing is queried or saved. Otherwise
// it asks the Source for the latest release and saves the state when the query counts as an
// attempt. A failed lock or reload abandons the check without a query or a save. No failure is
// returned or printed.
//
// When the check leaves a release to announce, the goroutine then runs [Config.CheckManaged], so
// that the [Notice] of a managed install says how to update it; Start itself never locates or
// inspects the executable. Only a [*ManagedInstallError] changes the notice: any other error from
// CheckManaged leaves its default text. CheckManaged takes no context, so it runs on a goroutine of
// its own that the check waits for only until its context ends; a managed check still running then
// is abandoned, leaves the default text, and never holds up [Check.Finish].
func (c Config) Start(ctx context.Context) (*Check, error) {
	r, err := c.resolve()
	if err != nil {
		return nil, err
	}
	st, err := r.StateStore.Load(ctx)
	if err != nil {
		st = State{}
	}
	if eligibility(checkInputsFrom(r, st)) != checkEligible {
		return nil, nil
	}
	checkCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	k := &Check{
		name:           r.Name,
		current:        r.Version,
		saved:          st.latestVersionOn(r.Channel),
		highestApplied: st.HighestApplied,
		wait:           r.CheckWait,
		cancel:         cancel,
		done:           make(chan struct{}),
	}
	go func() {
		defer close(k.done)
		k.result = runCheck(checkCtx, r)
		if k.announces(higher(k.saved, k.result)) {
			k.instead = managedInsteadWithin(checkCtx, r)
		}
	}()
	return k, nil
}

// runCheck runs one check of r under ctx and returns the newest release version it learnt on
// r.Channel, or empty when it learnt none. The reloaded state is read on r.Channel as Start reads
// it: an attempt or a version learnt on another channel neither throttles nor is returned.
//
// The Locker is held from the reload to the save, across the Source query, so that two processes
// never query the source for the same check and neither saves over the other's attempt. On Beta and
// Edge the query reads the release list page by page, so the lock can be held for several requests,
// each bounded by the Source's own timeout; the check's ctx, cancelled at most CheckWait after the
// command finishes, bounds the whole hold.
func runCheck(ctx context.Context, r Config) string {
	unlock, err := r.Locker.Lock(ctx)
	if err != nil {
		return ""
	}
	defer unlock()
	st, err := r.StateStore.Load(ctx)
	if err != nil {
		return ""
	}
	if !throttleElapsed(st.lastAttemptOn(r.Channel), r.Now(), r.CheckInterval) {
		return st.latestVersionOn(r.Channel)
	}
	rel, err := r.Source.Latest(ctx, r.Channel)
	next, save := afterAttempt(ctx, st, r.Now(), r.Channel, rel.Tag, err)
	if save {
		_ = r.StateStore.Save(ctx, next) // a failed save never fails the command
	}
	if err != nil {
		return ""
	}
	return rel.Tag
}

// Notice reports the release to announce, and false when there is none. It never blocks.
//
// The candidate is the higher, in the order of [version.Newer], of the LatestVersion the state
// held when the check started and, once the check has finished, the version it learnt. It is
// reported when it is newer than the running version and not below the HighestApplied the state
// held when the check started: a release below the watermark is a downgrade, never an update.
// The Notice's Instead is set only once the check has finished, from its managed install check.
func (k *Check) Notice() (Notice, bool) {
	if k == nil {
		return Notice{}, false
	}
	latest, instead := k.saved, ""
	select {
	case <-k.done:
		latest = higher(latest, k.result)
		instead = k.instead
	default:
	}
	if !k.announces(latest) {
		return Notice{}, false
	}
	return Notice{Name: k.name, Current: k.current, Latest: latest, Instead: instead}, true
}

// announces reports whether latest is a release to announce: newer than the running version and
// not below the HighestApplied the state held when the check started.
func (k *Check) announces(latest string) bool {
	return version.Newer(k.current, latest) && !version.Newer(latest, k.highestApplied)
}

// higher returns the higher of a and b in the order of [version.Newer], and a when neither is.
func higher(a, b string) string {
	if version.Newer(a, b) {
		return b
	}
	return a
}

// managedInstead returns the Instead of the [*ManagedInstallError] r's executable gets, and empty
// when it gets none or another error: a check that cannot tell keeps the default notice.
func managedInstead(r Config) string {
	var managed *ManagedInstallError
	if errors.As(r.CheckManaged(), &managed) {
		return managed.Instead
	}
	return ""
}

// managedInsteadWithin returns managedInstead(r), or empty once ctx ends first. A ctx already done
// skips the managed check. CheckManaged honours no context, so it runs on a goroutine of its own;
// the channel's buffer lets that goroutine deliver and return after it is abandoned.
func managedInsteadWithin(ctx context.Context, r Config) string {
	if ctx.Err() != nil {
		return ""
	}
	result := make(chan string, 1)
	go func() { result <- managedInstead(r) }()
	select {
	case instead := <-result:
		return instead
	case <-ctx.Done():
		return ""
	}
}

// Finish waits up to CheckWait for the check to finish, then cancels it and waits for its
// goroutine to return. A check cancelled this way is not an attempt, so it saves nothing. Its
// bound holds while the Locker, the Source and the StateStore honour their context; the managed
// install check is bounded by the context whether or not it returns.
//
// Finish is safe to call more than once and from several goroutines; every call returns once the
// goroutine has. On a nil *Check it does nothing.
func (k *Check) Finish() {
	if k == nil {
		return
	}
	k.once.Do(func() {
		timer := time.NewTimer(k.wait)
		defer timer.Stop()
		select {
		case <-k.done:
		case <-timer.C:
		}
		k.cancel()
		<-k.done
	})
}

// cancelNow cancels the check without waiting for it to finish, then waits for its goroutine to
// return. Called while a Finish is waiting, it cuts that wait short too. On a nil *Check it does
// nothing.
func (k *Check) cancelNow() {
	if k == nil {
		return
	}
	k.cancel()
	k.once.Do(func() { <-k.done })
}
