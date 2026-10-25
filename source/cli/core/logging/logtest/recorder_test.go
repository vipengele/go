package logtest_test

import (
	"fmt"
	"log/slog"
	"maps"
	"sync"
	"testing"

	"go.vipengele.org/cli/core/logging/logtest"
)

// only returns the single record rec has captured, failing t when there is not exactly one.
func only(t *testing.T, rec *logtest.Recorder) logtest.Record {
	t.Helper()
	got := rec.Records()
	if len(got) != 1 {
		t.Fatalf("Records() holds %d records, want 1: %+v", len(got), got)
	}
	return got[0]
}

func assertAttrs(t *testing.T, got logtest.Record, want map[string]any) {
	t.Helper()
	if !maps.Equal(got.Attrs, want) {
		t.Errorf("Attrs = %v, want %v", got.Attrs, want)
	}
}

func TestRecorderCapturesLevelMessageAndAttrs(t *testing.T) {
	rec := logtest.New()
	slog.New(rec).Warn("cloning", "repo", "go", "attempt", 2)

	got := only(t, rec)
	if got.Level != slog.LevelWarn || got.Message != "cloning" {
		t.Errorf("record = %v %q, want WARN %q", got.Level, got.Message, "cloning")
	}
	assertAttrs(t, got, map[string]any{"repo": "go", "attempt": int64(2)})
}

func TestRecorderFlattensGroups(t *testing.T) {
	rec := logtest.New()
	slog.New(rec).Info("request",
		slog.Group("http", slog.Int("status", 200), slog.Group("req", slog.String("method", "GET"))),
	)
	assertAttrs(t, only(t, rec), map[string]any{"http.status": int64(200), "http.req.method": "GET"})
}

func TestRecorderInlinesGroupWithEmptyKey(t *testing.T) {
	rec := logtest.New()
	slog.New(rec).Info("m", slog.Group("", slog.String("a", "1")))
	assertAttrs(t, only(t, rec), map[string]any{"a": "1"})
}

func TestRecorderIgnoresEmptyAttrsAndEmptyGroups(t *testing.T) {
	rec := logtest.New()
	slog.New(rec).Info("m", slog.Attr{}, slog.String("", "dropped"), slog.Group("empty"), slog.String("kept", "x"))
	assertAttrs(t, only(t, rec), map[string]any{"kept": "x"})
}

func TestRecorderResolvesLogValuers(t *testing.T) {
	rec := logtest.New()
	slog.New(rec).Info("m", slog.Any("user", user{name: "ada"}))
	assertAttrs(t, only(t, rec), map[string]any{"user.name": "ada"})
}

type user struct{ name string }

func (u user) LogValue() slog.Value {
	return slog.GroupValue(slog.String("name", u.name))
}

func TestRecorderWithAttrsThenWithGroup(t *testing.T) {
	rec := logtest.New()
	slog.New(rec).With("a", 1).WithGroup("g").Info("m", "b", 2)
	assertAttrs(t, only(t, rec), map[string]any{"a": int64(1), "g.b": int64(2)})
}

func TestRecorderWithGroupThenWithAttrs(t *testing.T) {
	rec := logtest.New()
	slog.New(rec).WithGroup("g").With("a", 1).WithGroup("h").Info("m", "b", 2)
	assertAttrs(t, only(t, rec), map[string]any{"g.a": int64(1), "g.h.b": int64(2)})
}

func TestRecorderEmptyWithAttrsAndWithGroupReturnReceiver(t *testing.T) {
	rec := logtest.New()
	if h := rec.WithAttrs(nil); h != rec {
		t.Errorf("WithAttrs(nil) = %p, want the receiver %p", h, rec)
	}
	if h := rec.WithGroup(""); h != rec {
		t.Errorf("WithGroup(\"\") = %p, want the receiver %p", h, rec)
	}
}

func TestRecorderGroupWithNoAttrsContributesNoKeys(t *testing.T) {
	rec := logtest.New()
	slog.New(rec).WithGroup("g").Info("m")
	assertAttrs(t, only(t, rec), map[string]any{})
}

func TestRecorderDuplicateKeyKeepsLastValue(t *testing.T) {
	rec := logtest.New()
	slog.New(rec).With("k", "handler").Info("m", "k", "first", "k", "last")
	assertAttrs(t, only(t, rec), map[string]any{"k": "last"})
}

func TestRecorderGroupKeyCollidingWithDottedKeyKeepsLastValue(t *testing.T) {
	rec := logtest.New()
	log := slog.New(rec)
	log.Info("m", slog.Group("http", slog.Int("status", 200)), slog.Int("http.status", 500))
	log.Info("m", slog.Int("http.status", 500), slog.Group("http", slog.Int("status", 200)))

	got := rec.Records()
	if len(got) != 2 {
		t.Fatalf("Records() holds %d records, want 2", len(got))
	}
	assertAttrs(t, got[0], map[string]any{"http.status": int64(500)})
	assertAttrs(t, got[1], map[string]any{"http.status": int64(200)})
}

func TestRecorderDerivedHandlersShareTheStore(t *testing.T) {
	rec := logtest.New()
	derived := rec.WithGroup("g").WithAttrs([]slog.Attr{slog.String("a", "1")})
	slog.New(rec).Info("root")
	slog.New(derived).Info("derived")

	for name, h := range map[string]slog.Handler{"root": rec, "derived": derived} {
		got := h.(*logtest.Recorder).Records()
		if len(got) != 2 || got[0].Message != "root" || got[1].Message != "derived" {
			t.Errorf("%s Records() = %+v, want root then derived", name, got)
		}
	}
	if got := rec.Records()[0].Attrs; len(got) != 0 {
		t.Errorf("root record Attrs = %v, want none of the derived handler's", got)
	}
}

func TestRecorderResetClearsSharedStore(t *testing.T) {
	rec := logtest.New()
	derived := rec.WithGroup("g").(*logtest.Recorder)
	slog.New(derived).Info("before")
	rec.Reset()
	if got := derived.Records(); len(got) != 0 {
		t.Fatalf("Records() after Reset = %+v, want none", got)
	}
	slog.New(derived).Info("after")
	if got := only(t, rec); got.Message != "after" {
		t.Errorf("Message = %q, want %q", got.Message, "after")
	}
}

func TestRecorderRecordsIsASnapshot(t *testing.T) {
	rec := logtest.New()
	log := slog.New(rec)
	log.Info("first", "k", "v")

	snap := rec.Records()
	snap[0].Attrs["k"] = "changed"
	snap[0].Message = "changed"
	log.Info("second")

	if len(snap) != 1 {
		t.Errorf("snapshot grew to %d records after a later record", len(snap))
	}
	got := rec.Records()
	if len(got) != 2 || got[0].Message != "first" || got[0].Attrs["k"] != "v" {
		t.Errorf("Records() = %+v, want the first record unchanged by edits to a snapshot", got)
	}
	rec.Reset()
	if snap[0].Attrs["k"] != "changed" {
		t.Errorf("snapshot Attrs = %v after Reset, want the snapshot's own map", snap[0].Attrs)
	}
}

func TestRecorderConcurrentHandle(t *testing.T) {
	const goroutines, perGoroutine = 8, 50
	rec := logtest.New()
	var wg sync.WaitGroup
	for g := range goroutines {
		wg.Add(1)
		go func() {
			defer wg.Done()
			log := slog.New(rec).WithGroup(fmt.Sprintf("g%d", g))
			for i := range perGoroutine {
				log.Info("m", "i", i)
				_ = rec.Records()
			}
		}()
	}
	wg.Wait()
	if got := len(rec.Records()); got != goroutines*perGoroutine {
		t.Errorf("Records() holds %d records, want %d", got, goroutines*perGoroutine)
	}
}
