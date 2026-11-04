package daemon

import (
	"context"
	"errors"
	"strings"
	"testing"

	"go.vipengele.org/cli/spec"
	"go.vipengele.org/cli/spec/paths"
)

func lockIdentity() spec.Identity {
	return spec.Identity{
		Name:      "demo",
		Owner:     "vipengele",
		Repo:      "demo",
		EnvPrefix: "DEMO",
		DirName:   "demo",
	}
}

// lockHeld reports nil when another holder has the instance lock of opts, and otherwise why not.
// It is safe to call from any goroutine.
func lockHeld(opts paths.Options) error {
	in, err := AcquireInstance(lockIdentity(), opts)
	if err == nil {
		in.Release()
		return errors.New("instance lock is free")
	}
	if !errors.Is(err, ErrAlreadyRunning) {
		return err
	}
	return nil
}

// checkHeldIn reports, from any goroutine, a lock that is not held while what happens.
func checkHeldIn(t *testing.T, opts paths.Options, what string) {
	t.Helper()
	if err := lockHeld(opts); err != nil {
		t.Errorf("%s: %v, want the instance lock held", what, err)
	}
}

// checkFree fails the test unless a fresh AcquireInstance succeeds.
func checkFree(t *testing.T, opts paths.Options) {
	t.Helper()
	in, err := AcquireInstance(lockIdentity(), opts)
	if err != nil {
		t.Fatalf("AcquireInstance after Run: %v, want the instance lock free", err)
	}
	in.Release()
}

func TestNewRejectsAnInvalidInstanceIdentity(t *testing.T) {
	id := lockIdentity()
	id.DirName = ""
	s, err := New([]Runner{&fakeRunner{"a"}}, WithInstanceLock(id, paths.Options{RuntimeDir: t.TempDir()}))
	if err == nil {
		t.Fatalf("New succeeded, want an error")
	}
	if s != nil {
		t.Errorf("New returned a Supervisor alongside an error")
	}
	if want := id.Validate().Error(); !strings.Contains(err.Error(), want) {
		t.Errorf("New = %v, want it to carry the identity's problem %q", err, want)
	}
}

func TestRunHoldsTheInstanceLockFromFirstStartUntilLastStop(t *testing.T) {
	boom := errors.New("boom")
	tests := []struct {
		name   string
		policy PanicPolicy
		// setup configures b before Run starts.
		setup func(b *scriptedRunner)
		// end drives Run to its return once a has started.
		end func(t *testing.T, h *harness, opts paths.Options)
		// wantErr is what Run returns, nil for a clean shutdown.
		wantErr []wantErr
	}{
		{
			name: "clean shutdown",
			end: func(t *testing.T, h *harness, opts paths.Options) {
				<-h.runners[1].started
				<-h.waiting
				checkHeldIn(t, opts, "while every runner runs")
				h.cancelRun()
			},
		},
		{
			name:    "start failure",
			setup:   func(b *scriptedRunner) { b.startErr = boom },
			end:     func(*testing.T, *harness, paths.Options) {},
			wantErr: []wantErr{{is: boom}},
		},
		{
			name:    "start panic",
			setup:   func(b *scriptedRunner) { b.startPanics, b.startPanicValue = true, "oops" },
			end:     func(*testing.T, *harness, paths.Options) {},
			wantErr: []wantErr{{panicked: "b"}},
		},
		{
			name:  "start timeout",
			setup: func(b *scriptedRunner) { b.hangStart = true },
			end: func(t *testing.T, h *harness, opts paths.Options) {
				<-h.runners[1].started
				checkHeldIn(t, opts, "while a Start hangs")
				h.expireStart()
			},
			wantErr: []wantErr{{is: ErrStartTimeout}},
		},
		{
			name:  "run context cancelled during start",
			setup: func(b *scriptedRunner) { b.hangStart = true },
			end: func(t *testing.T, h *harness, opts paths.Options) {
				<-h.runners[1].started
				checkHeldIn(t, opts, "while a Start hangs")
				h.cancelRun()
			},
		},
		{
			name:   "panic exit under PanicFail",
			policy: PanicFail,
			end: func(t *testing.T, h *harness, opts paths.Options) {
				<-h.runners[1].started
				<-h.waiting
				checkHeldIn(t, opts, "while every runner runs")
				h.runners[1].panicExit("oops")
			},
			wantErr: []wantErr{{panicked: "b"}},
		},
		{
			name:   "panic exit under PanicIsolate",
			policy: PanicIsolate,
			end: func(t *testing.T, h *harness, opts paths.Options) {
				<-h.runners[1].started
				<-h.waiting
				h.runners[1].panicExit("oops")
				<-h.waiting
				checkHeldIn(t, opts, "while a panic is isolated")
				h.cancelRun()
			},
			wantErr: []wantErr{{panicked: "b"}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := paths.Options{RuntimeDir: t.TempDir()}
			a, b := newScripted("a"), newScripted("b")
			a.onStart = func() { checkHeldIn(t, opts, "in the first Start") }
			// a is stopped last, so its Stop runs after every other Runner has been stopped.
			a.onStop = func() { checkHeldIn(t, opts, "in the last Stop") }
			if tt.setup != nil {
				tt.setup(b)
			}
			h := launchRun(t, []Option{WithPanicPolicy(tt.policy), WithInstanceLock(lockIdentity(), opts)}, a, b)
			<-a.started
			tt.end(t, h, opts)

			err := <-h.result
			if tt.wantErr == nil {
				if err != nil {
					t.Errorf("Run = %v, want nil", err)
				}
			} else {
				checkJoined(t, err, tt.wantErr)
			}
			checkOrder(t, "stop", h.rec.stopOrder(), []string{"b", "a"})
			checkFree(t, opts)
			h.finish()
		})
	}
}

func TestRunReleasesTheInstanceLockOnlyAfterTheLastStopReturns(t *testing.T) {
	opts := paths.Options{RuntimeDir: t.TempDir()}
	a, b := newScripted("a"), newScripted("b")
	a.hangStop = true
	h := runScriptedWith(t, []Option{WithInstanceLock(lockIdentity(), opts)}, a, b)
	h.cancelRun()

	<-a.stopCalled
	checkHeldIn(t, opts, "while the last Stop hangs")
	select {
	case err := <-h.result:
		t.Fatalf("Run returned %v while the last Stop hangs", err)
	default:
	}
	a.free()

	if err := <-h.result; err != nil {
		t.Errorf("Run = %v, want nil", err)
	}
	checkFree(t, opts)
	h.finish()
}

func TestRunWithAHeldInstanceLockStartsNothing(t *testing.T) {
	opts := paths.Options{RuntimeDir: t.TempDir()}
	holder, err := AcquireInstance(lockIdentity(), opts)
	if err != nil {
		t.Fatalf("AcquireInstance: %v", err)
	}
	defer holder.Release()

	a := newScripted("a")
	a.rec = &recorder{}
	s, err := New([]Runner{a}, WithInstanceLock(lockIdentity(), opts))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	err = s.Run(context.Background())
	if !errors.Is(err, ErrAlreadyRunning) {
		t.Fatalf("Run = %v, want ErrAlreadyRunning", err)
	}
	var are *AlreadyRunningError
	if !errors.As(err, &are) {
		t.Fatalf("Run error %T is not an *AlreadyRunningError", err)
	}
	if are.Path != holder.Path() {
		t.Errorf("Path = %q, want %q", are.Path, holder.Path())
	}
	if got := a.rec.startOrder(); len(got) != 0 {
		t.Errorf("Start called on %v, want none", got)
	}
	if got := a.rec.stopOrder(); len(got) != 0 {
		t.Errorf("Stop called on %v, want none", got)
	}

	holder.Release()
	if err := s.Run(context.Background()); !errors.Is(err, ErrAlreadyRan) {
		t.Errorf("Run after the lock is free = %v, want %v", err, ErrAlreadyRan)
	}
	checkFree(t, opts)
}
