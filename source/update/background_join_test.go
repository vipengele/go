package update

import (
	"context"
	"errors"
	"testing"
	"time"
)

// lingerDelay is how long a lingeringLocker keeps running after its context is done.
const lingerDelay = 100 * time.Millisecond

// lingeringLocker is a Locker whose Lock blocks until ctx is done and then takes lingerDelay to
// return, so a caller that does not join the goroutine running it returns first.
type lingeringLocker struct{}

func (lingeringLocker) Lock(ctx context.Context) (func(), error) {
	<-ctx.Done()
	time.Sleep(lingerDelay)
	return nil, ctx.Err()
}

func (lingeringLocker) TryLock() (func(), error) {
	return nil, errors.New("lingeringLocker: not supported")
}

// failingLocker is a Locker whose Lock always fails.
type failingLocker struct{}

func (failingLocker) Lock(context.Context) (func(), error) {
	return nil, errors.New("failingLocker: cannot lock")
}

func (failingLocker) TryLock() (func(), error) {
	return nil, errors.New("failingLocker: cannot lock")
}

// lingeringCheck starts a check whose goroutine is blocked in a lingeringLocker.
func lingeringCheck(t *testing.T, wait time.Duration) *Check {
	t.Helper()
	srv := newKitServer(t)
	c := bgConfig(t, srv, &fakeStore{})
	c.Locker = lingeringLocker{}
	c.CheckWait = wait
	k := start(t, c)
	if k == nil {
		t.Fatal("Start() = nil, want a check")
	}
	return k
}

// assertJoined fails t when k's goroutine is still running.
func assertJoined(t *testing.T, k *Check, caller string) {
	t.Helper()
	select {
	case <-k.done:
	default:
		t.Errorf("%s returned before the check's goroutine did", caller)
	}
}

func TestRunCheckLearnsNothingWhenTheLockFails(t *testing.T) {
	t.Parallel()
	c := bgConfig(t, newKitServer(t), &fakeStore{})
	c.Locker = failingLocker{}
	if got := runCheck(context.Background(), c); got != "" {
		t.Errorf("runCheck() = %q, want none", got)
	}
}

func TestRunCheckLearnsNothingWhenTheReloadFails(t *testing.T) {
	t.Parallel()
	store := &fakeStore{loads: []loadResult{{err: errors.New("fakeStore: unreadable")}}}
	c := bgConfig(t, newKitServer(t), store)
	if got := runCheck(context.Background(), c); got != "" {
		t.Errorf("runCheck() = %q, want none", got)
	}
}

func TestRunCheckLearnsNothingWhenTheSourceFails(t *testing.T) {
	t.Parallel()
	c := bgConfig(t, newKitServer(t), &fakeStore{})
	c.Source = errSource{}
	if got := runCheck(context.Background(), c); got != "" {
		t.Errorf("runCheck() = %q, want none", got)
	}
}

func TestFinishJoinsTheGoroutine(t *testing.T) {
	t.Parallel()
	k := lingeringCheck(t, time.Millisecond)
	k.Finish()
	assertJoined(t, k, "Finish()")
}

func TestCancelNowJoinsTheGoroutine(t *testing.T) {
	t.Parallel()
	k := lingeringCheck(t, maxCheckWait)
	k.cancelNow()
	assertJoined(t, k, "cancelNow()")
}

func TestPostHookJoinsTheGoroutineOfACancelledRun(t *testing.T) {
	t.Parallel()
	c := hookConfig(t, newKitServer(t))
	c.Locker = lingeringLocker{}
	pre, post := mustHooks(t, c)
	r := newHookRun(true)

	ctx, cancel := context.WithCancel(context.Background())
	ctx, err := pre(ctx, r.inv, plainCommand())
	if err != nil {
		t.Fatalf("pre() error = %v", err)
	}
	k, _ := ctx.Value(checkKey{}).(*Check)
	if k == nil {
		t.Fatal("pre() returned a context without a check")
	}
	cancel()
	if err := post(ctx, r.inv, plainCommand(), nil); err != nil {
		t.Errorf("post() = %v, want nil", err)
	}
	assertJoined(t, k, "post()")
}
