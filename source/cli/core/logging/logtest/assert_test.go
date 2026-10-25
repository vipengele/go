package logtest_test

import (
	"fmt"
	"log/slog"
	"runtime"
	"strings"
	"testing"

	"go.vipengele.org/cli/core/logging/logtest"
)

// fakeTB records a Fatalf instead of failing the real test. When returns is false Fatalf ends its
// goroutine as testing.T does; otherwise it returns to the caller.
type fakeTB struct {
	testing.TB
	returns bool
	failed  bool
	message string
}

func (f *fakeTB) Helper() {}

func (f *fakeTB) Fatalf(format string, args ...any) {
	f.failed = true
	f.message = fmt.Sprintf(format, args...)
	if !f.returns {
		runtime.Goexit()
	}
}

// run calls fn on its own goroutine, so a Fatalf that calls runtime.Goexit stops fn as it stops a
// test, and waits for it to finish.
func run(f *fakeTB, fn func(tb testing.TB)) {
	done := make(chan struct{})
	go func() {
		defer close(done)
		fn(f)
	}()
	<-done
}

func captured() *logtest.Recorder {
	rec := logtest.New()
	log := slog.New(rec)
	log.Info("started", "id", 7)
	log.Warn("cloning", "repo", "go", "attempt", 2)
	return rec
}

func TestRequireRecordReturnsMatch(t *testing.T) {
	rec := captured()
	f := &fakeTB{}
	var got logtest.Record
	run(f, func(tb testing.TB) {
		got = rec.RequireRecord(tb, slog.LevelWarn, "cloning", map[string]any{"attempt": int64(2)})
	})
	if f.failed {
		t.Fatalf("RequireRecord failed: %s", f.message)
	}
	if got.Message != "cloning" || got.Attrs["repo"] != "go" {
		t.Errorf("record = %+v, want the cloning record", got)
	}
}

func TestRequireRecordNilAttrsMatchesLevelAndMessage(t *testing.T) {
	f := &fakeTB{}
	run(f, func(tb testing.TB) { captured().RequireRecord(tb, slog.LevelInfo, "started", nil) })
	if f.failed {
		t.Fatalf("RequireRecord failed: %s", f.message)
	}
}

func TestRequireRecordFails(t *testing.T) {
	tests := []struct {
		name    string
		level   slog.Level
		message string
		attrs   map[string]any
	}{
		{"level mismatch", slog.LevelError, "cloning", nil},
		{"message mismatch", slog.LevelWarn, "other", nil},
		{"attribute value mismatch", slog.LevelWarn, "cloning", map[string]any{"repo": "other"}},
		{"attribute type mismatch", slog.LevelWarn, "cloning", map[string]any{"attempt": 2}},
		{"missing attribute", slog.LevelWarn, "cloning", map[string]any{"absent": true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := &fakeTB{}
			reached := false
			run(f, func(tb testing.TB) {
				captured().RequireRecord(tb, tt.level, tt.message, tt.attrs)
				reached = true
			})
			if !f.failed {
				t.Fatal("RequireRecord did not fail")
			}
			if reached {
				t.Error("RequireRecord continued past Fatalf")
			}
		})
	}
}

func TestRequireRecordFailureListsCapturedRecords(t *testing.T) {
	f := &fakeTB{}
	run(f, func(tb testing.TB) { captured().RequireRecord(tb, slog.LevelError, "boom", nil) })
	for _, want := range []string{`"boom"`, `[0] INFO "started" [id=7 (int64)]`, `[1] WARN "cloning" [attempt=2 (int64) repo=go (string)]`} {
		if !strings.Contains(f.message, want) {
			t.Errorf("message %q lacks %q", f.message, want)
		}
	}
}

func TestRequireRecordFailureWithNoRecords(t *testing.T) {
	f := &fakeTB{}
	run(f, func(tb testing.TB) { logtest.New().RequireRecord(tb, slog.LevelInfo, "x", nil) })
	if !f.failed || !strings.Contains(f.message, "(none)") {
		t.Errorf("failed = %v, message %q, want a failure naming no records", f.failed, f.message)
	}
}

func TestRequireRecordReturnsZeroWhenFatalfReturns(t *testing.T) {
	f := &fakeTB{returns: true}
	var got logtest.Record
	run(f, func(tb testing.TB) { got = captured().RequireRecord(tb, slog.LevelError, "boom", nil) })
	if !f.failed {
		t.Fatal("RequireRecord did not fail")
	}
	if got.Message != "" || got.Attrs != nil {
		t.Errorf("record = %+v, want the zero Record", got)
	}
}

func TestRequireNonePasses(t *testing.T) {
	f := &fakeTB{}
	run(f, func(tb testing.TB) { captured().RequireNone(tb, slog.LevelError) })
	if f.failed {
		t.Fatalf("RequireNone failed: %s", f.message)
	}
}

func TestRequireNoneFailsAtOrAboveLevel(t *testing.T) {
	for _, level := range []slog.Level{slog.LevelWarn, slog.LevelInfo} {
		f := &fakeTB{}
		run(f, func(tb testing.TB) { captured().RequireNone(tb, level) })
		if !f.failed {
			t.Fatalf("RequireNone(%v) did not fail", level)
		}
		if !strings.Contains(f.message, `[1] WARN "cloning"`) {
			t.Errorf("message %q does not list the captured records", f.message)
		}
	}
}

func TestRequireNoneReturnsWhenFatalfReturns(t *testing.T) {
	f := &fakeTB{returns: true}
	run(f, func(tb testing.TB) { captured().RequireNone(tb, slog.LevelInfo) })
	if !f.failed {
		t.Fatal("RequireNone did not fail")
	}
}
