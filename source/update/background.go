package update

import (
	"context"
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
	// saved is the LatestVersion the state held when the check started.
	saved string
	// wait is the longest Finish waits before cancelling the check.
	wait time.Duration
	// cancel stops the check's goroutine.
	cancel context.CancelFunc
	// done is closed once the goroutine has returned.
	done chan struct{}
	// result is the newest release version this run learnt, or empty when it learnt none. It is
	// read only after done is closed.
	result string
	// once makes the wait and the join of Finish and cancelNow happen once.
	once sync.Once
}

// Start begins an update check in the background and returns at once. It returns an error only
// when c is invalid, as [Config.Resolved] reports it.
//
// Start loads the Update state through the StateStore under ctx; a Load error counts as the zero
// State. When the check is ineligible (a dev build, in CI, stderr not a terminal, disabled, opted
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
		name:    r.Name,
		current: r.Version,
		saved:   st.LatestVersion,
		wait:    r.CheckWait,
		cancel:  cancel,
		done:    make(chan struct{}),
	}
	go func() {
		defer close(k.done)
		k.result = runCheck(checkCtx, r)
	}()
	return k, nil
}

// runCheck runs one check of r under ctx and returns the newest release version it learnt, or
// empty when it learnt none.
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
	if !throttleElapsed(st.LastAttempt, r.Now(), r.CheckInterval) {
		return st.LatestVersion
	}
	rel, err := r.Source.Latest(ctx, r.Channel)
	next, save := afterAttempt(ctx, st, r.Now(), rel.Tag, err)
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
// reported when it is newer than the running version.
func (k *Check) Notice() (Notice, bool) {
	if k == nil {
		return Notice{}, false
	}
	latest := k.saved
	select {
	case <-k.done:
		if version.Newer(latest, k.result) {
			latest = k.result
		}
	default:
	}
	if !version.Newer(k.current, latest) {
		return Notice{}, false
	}
	return Notice{Name: k.name, Current: k.current, Latest: latest}, true
}

// Finish waits up to CheckWait for the check to finish, then cancels it and waits for its
// goroutine to return. A check cancelled this way is not an attempt, so it saves nothing. Its
// bound holds while the Locker, the Source and the StateStore honour their context.
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
