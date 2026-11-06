package daemon

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"sync"
	"testing"
)

// logged is one record a captureHandler received, with its attributes by key.
type logged struct {
	level slog.Level
	msg   string
	attrs map[string]slog.Value
}

// capturedLog is the records of one test, in the order they were handled. Run logs from one
// goroutine, but the lock keeps a reader in the test goroutine safe while Run still runs.
type capturedLog struct {
	mu      sync.Mutex
	records []logged
}

func (c *capturedLog) add(r logged) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.records = append(c.records, r)
}

func (c *capturedLog) all() []logged {
	c.mu.Lock()
	defer c.mu.Unlock()
	return slices.Clone(c.records)
}

// captureHandler is a slog.Handler that keeps every record, at every level, in its capturedLog.
// Groups are not modelled: an attribute is kept under its own key.
type captureHandler struct {
	log   *capturedLog
	attrs []slog.Attr
}

func (h captureHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h captureHandler) Handle(_ context.Context, r slog.Record) error {
	rec := logged{level: r.Level, msg: r.Message, attrs: make(map[string]slog.Value)}
	for _, a := range h.attrs {
		rec.attrs[a.Key] = a.Value
	}
	r.Attrs(func(a slog.Attr) bool {
		rec.attrs[a.Key] = a.Value
		return true
	})
	h.log.add(rec)
	return nil
}

func (h captureHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return captureHandler{log: h.log, attrs: append(slices.Clone(h.attrs), attrs...)}
}

func (h captureHandler) WithGroup(string) slog.Handler { return h }

// newCapture returns a log to read records from and the logger that writes them.
func newCapture() (*capturedLog, *slog.Logger) {
	c := &capturedLog{}
	return c, slog.New(captureHandler{log: c})
}

// wantRecord is a record a test expects. A Warn or Error record must carry an "err" attribute,
// matching err under errors.Is when err is set and holding a *PanicError when panicked is set; an
// Info record must carry none.
type wantRecord struct {
	level    slog.Level
	msg      string
	runner   string
	err      error
	panicked bool
}

func checkRecords(t *testing.T, got []logged, want []wantRecord) {
	t.Helper()
	if len(got) != len(want) {
		t.Errorf("logged %d records, want %d:\n got  %v\n want %v", len(got), len(want), got, want)
		return
	}
	for i, w := range want {
		g := got[i]
		if g.level != w.level || g.msg != w.msg {
			t.Errorf("record %d = %v %q, want %v %q", i, g.level, g.msg, w.level, w.msg)
		}
		if r, ok := g.attrs["runner"]; !ok || r.String() != w.runner {
			t.Errorf("record %d %q: runner = %v (present %v), want %q", i, g.msg, r, ok, w.runner)
		}
		v, hasErr := g.attrs["err"]
		if w.level < slog.LevelWarn {
			if hasErr {
				t.Errorf("record %d %q: err = %v, want none", i, g.msg, v)
			}
			continue
		}
		if !hasErr {
			t.Errorf("record %d %q: no err attribute", i, g.msg)
			continue
		}
		err, ok := v.Any().(error)
		if !ok {
			t.Errorf("record %d %q: err = %v (%T), want an error", i, g.msg, v.Any(), v.Any())
			continue
		}
		if w.err != nil && !errors.Is(err, w.err) {
			t.Errorf("record %d %q: err = %v, want it to wrap %v", i, g.msg, err, w.err)
		}
		var pe *PanicError
		if w.panicked && (!errors.As(err, &pe) || pe.Runner != w.runner) {
			t.Errorf("record %d %q: err = %v, want a *PanicError of %q", i, g.msg, err, w.runner)
		}
	}
}

func started(name string) wantRecord {
	return wantRecord{level: slog.LevelInfo, msg: "runner started", runner: name}
}

func stopped(name string) wantRecord {
	return wantRecord{level: slog.LevelInfo, msg: "runner stopped", runner: name}
}

func panicked(name string) wantRecord {
	return wantRecord{level: slog.LevelError, msg: "runner panicked", runner: name, panicked: true}
}

func TestRunLogsAStartAndAStopOfEachRunner(t *testing.T) {
	logs, logger := newCapture()
	h := runScriptedWith(t, []Option{WithLogger(logger)}, newScripted("a"), newScripted("b"))
	h.cancelRun()
	if err := <-h.result; err != nil {
		t.Errorf("Run = %v, want nil", err)
	}
	checkRecords(t, logs.all(), []wantRecord{started("a"), started("b"), stopped("b"), stopped("a")})
	h.finish()
}

func TestRunLogsAStartFailureOnce(t *testing.T) {
	boom := errors.New("boom")
	tests := []struct {
		name   string
		policy PanicPolicy
		setup  func(b *scriptedRunner)
		failed wantRecord
	}{
		{
			name:   "start error",
			setup:  func(b *scriptedRunner) { b.startErr = boom },
			failed: wantRecord{level: slog.LevelError, msg: "runner start failed", runner: "b", err: boom},
		},
		{
			name:   "start panic under fail",
			setup:  func(b *scriptedRunner) { b.startPanics, b.startPanicValue = true, boom },
			failed: panicked("b"),
		},
		{
			name:   "start panic under isolate",
			policy: PanicIsolate,
			setup:  func(b *scriptedRunner) { b.startPanics, b.startPanicValue = true, "oops" },
			failed: panicked("b"),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logs, logger := newCapture()
			a, b, c := newScripted("a"), newScripted("b"), newScripted("c")
			tt.setup(b)
			h := launchRun(t, []Option{WithPanicPolicy(tt.policy), WithLogger(logger)}, a, b, c)
			if err := <-h.result; err == nil {
				t.Errorf("Run = nil, want b's start failure")
			}
			checkRecords(t, logs.all(), []wantRecord{started("a"), tt.failed, stopped("a")})
			h.finish()
		})
	}
}

func TestRunLogsAStartTimeoutAsAStartFailure(t *testing.T) {
	logs, logger := newCapture()
	a, b := newScripted("a"), newScripted("b")
	b.hangStart = true
	h := launchRun(t, []Option{WithLogger(logger)}, a, b)
	<-a.started
	<-b.started
	h.expireStart()
	if err := <-h.result; !errors.Is(err, ErrStartTimeout) {
		t.Errorf("Run = %v, want ErrStartTimeout", err)
	}
	checkRecords(t, logs.all(), []wantRecord{
		started("a"),
		{level: slog.LevelError, msg: "runner start failed", runner: "b", err: ErrStartTimeout},
		stopped("a"),
	})
	h.finish()
}

func TestRunLogsARunnerExitThatBeginsShutdown(t *testing.T) {
	boom := errors.New("boom")
	tests := []struct {
		name   string
		end    func(b *scriptedRunner)
		exited wantRecord
	}{
		{
			name:   "exit with an error",
			end:    func(b *scriptedRunner) { b.exit(boom) },
			exited: wantRecord{level: slog.LevelError, msg: "runner exited", runner: "b", err: boom},
		},
		{
			name:   "exit without an error",
			end:    func(b *scriptedRunner) { b.exit(nil) },
			exited: wantRecord{level: slog.LevelError, msg: "runner exited", runner: "b", err: ErrRunnerExited},
		},
		{
			name:   "exit with a panic under fail",
			end:    func(b *scriptedRunner) { b.panicExit("oops") },
			exited: panicked("b"),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logs, logger := newCapture()
			a, b := newScripted("a"), newScripted("b")
			h := runScriptedWith(t, []Option{WithLogger(logger)}, a, b)
			tt.end(b)
			if err := <-h.result; err == nil {
				t.Errorf("Run = nil, want b's exit")
			}
			checkRecords(t, logs.all(), []wantRecord{started("a"), started("b"), tt.exited, stopped("a")})
			h.finish()
		})
	}
}

func TestRunLogsAnIsolatedPanicWhenItHappens(t *testing.T) {
	logs, logger := newCapture()
	a, b := newScripted("a"), newScripted("b")
	h := runScriptedWith(t, []Option{WithPanicPolicy(PanicIsolate), WithLogger(logger)}, a, b)
	b.panicExit("oops")
	<-h.waiting
	checkRecords(t, logs.all(), []wantRecord{started("a"), started("b"), panicked("b")})

	h.cancelRun()
	if err := <-h.result; err == nil {
		t.Errorf("Run = nil, want the isolated panic")
	}
	checkRecords(t, logs.all(), []wantRecord{started("a"), started("b"), panicked("b"), stopped("a")})
	h.finish()
}

func TestRunLogsAStopProblemAtWarn(t *testing.T) {
	stopBoom := errors.New("stop boom")
	tests := []struct {
		name  string
		setup func(b *scriptedRunner)
		hangs bool
		want  []wantRecord
	}{
		{
			name:  "stop hangs past the deadline",
			setup: func(b *scriptedRunner) { b.hangStop = true },
			hangs: true,
			want: []wantRecord{
				started("a"), started("b"),
				{level: slog.LevelWarn, msg: "runner stop timed out", runner: "b", err: ErrStopTimeout},
				{level: slog.LevelWarn, msg: "runner stop timed out", runner: "a", err: ErrStopTimeout},
			},
		},
		{
			name:  "stop returns an error",
			setup: func(b *scriptedRunner) { b.stopErr = stopBoom },
			want: []wantRecord{
				started("a"), started("b"),
				{level: slog.LevelWarn, msg: "runner stop timed out", runner: "b", err: stopBoom},
				stopped("b"), stopped("a"),
			},
		},
		{
			name:  "stop panics",
			setup: func(b *scriptedRunner) { b.stopPanic = "oops" },
			want:  []wantRecord{started("a"), started("b"), panicked("b"), stopped("b"), stopped("a")},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logs, logger := newCapture()
			a, b := newScripted("a"), newScripted("b")
			tt.setup(b)
			h := runScriptedWith(t, []Option{WithLogger(logger)}, a, b)
			h.cancelRun()
			if tt.hangs {
				<-b.stopCalled
				h.cancelStop()
			}
			if err := <-h.result; err == nil {
				t.Errorf("Run = nil, want b's stop problem")
			}
			checkRecords(t, logs.all(), tt.want)
			h.finish()
		})
	}
}
