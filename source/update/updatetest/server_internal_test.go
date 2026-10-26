package updatetest

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"sync"
	"testing"
	"time"
)

// cleanupTB passes every call to the test it embeds except Cleanup, whose functions it keeps for
// the test to run.
type cleanupTB struct {
	testing.TB
	cleanups []func()
}

func (c *cleanupTB) Cleanup(fn func()) {
	c.cleanups = append(c.cleanups, fn)
}

// cleanup returns a function running every kept function once, also registered with the
// embedded test so a test that stops early still runs them.
func (c *cleanupTB) cleanup() func() {
	run := sync.OnceFunc(func() {
		for _, fn := range slices.Backward(c.cleanups) {
			fn()
		}
	})
	c.TB.Cleanup(run)
	return run
}

// deadlockTimeout bounds a call that returns at once unless it deadlocks; it only ever fails a
// test, never paces one.
const deadlockTimeout = 10 * time.Second

func TestCloseLeavesTheServerUsable(t *testing.T) {
	t.Parallel()
	rel := NewRelease(t, "vipengele", "tool", "v1.2.3", "tool")
	tb := &cleanupTB{TB: t}
	s := NewServer(tb, rel)
	closeServer := tb.cleanup()
	closeServer()

	got := make(chan int, 1)
	go func() { got <- s.Requests(TargetArchive) }()
	timer := time.NewTimer(deadlockTimeout)
	defer timer.Stop()
	select {
	case n := <-got:
		if n != 0 {
			t.Errorf("Requests(TargetArchive) after close = %d, want 0", n)
		}
	case <-timer.C:
		t.Fatal("Requests after close did not return: close holds the server's lock")
	}
}

func TestCloseStopsTheListener(t *testing.T) {
	t.Parallel()
	rel := NewRelease(t, "vipengele", "tool", "v1.2.3", "tool")
	tb := &cleanupTB{TB: t}
	s := NewServer(tb, rel)
	closeServer := tb.cleanup()
	closeServer()

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, s.AssetURL(rel.ArchiveName), nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	resp, err := s.Client().Do(req)
	if err == nil {
		_ = resp.Body.Close()
		t.Errorf("GET after close = %d, want a connection error", resp.StatusCode)
	}
}

func TestServeAbortsASlowRequestWhoseContextEnded(t *testing.T) {
	t.Parallel()
	rel := NewRelease(t, "vipengele", "tool", "v1.2.3", "tool")
	s := NewServer(t, rel, Slow(TargetArchive, make(chan struct{})))

	// The gate never opens and the server stays open, so only the request's context can end the
	// wait.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	assetURL, err := url.Parse(s.AssetURL(rel.ArchiveName))
	if err != nil {
		t.Fatalf("parsing AssetURL: %v", err)
	}
	req := httptest.NewRequest(http.MethodGet, assetURL.EscapedPath(), nil).WithContext(ctx)
	rec := httptest.NewRecorder()

	recovered := func() (recovered any) {
		defer func() { recovered = recover() }()
		s.serve(rec, req)
		return nil
	}()
	if err, ok := recovered.(error); !ok || !errors.Is(err, http.ErrAbortHandler) {
		t.Errorf("serve with an ended context recovered %v, want http.ErrAbortHandler", recovered)
	}
	if rec.Body.Len() != 0 || len(rec.Header()) != 0 {
		t.Errorf("serve with an ended context wrote %d body bytes and header %v, want nothing", rec.Body.Len(), rec.Header())
	}
	if got := s.Requests(TargetArchive); got != 1 {
		t.Errorf("Requests(TargetArchive) = %d, want 1", got)
	}
}

func TestCloseReleasesASlowRequestWithNoAnswer(t *testing.T) {
	t.Parallel()
	rel := NewRelease(t, "vipengele", "tool", "v1.2.3", "tool")
	tb := &cleanupTB{TB: t}
	s := NewServer(tb, rel, Slow(TargetArchive, make(chan struct{})))
	closeServer := tb.cleanup()

	// The request's context is never cancelled, so only the server's cleanup can release the
	// handler.
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, s.AssetURL(rel.ArchiveName), nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	errs := make(chan error, 1)
	go func() {
		resp, err := s.Client().Do(req)
		if err == nil {
			_ = resp.Body.Close()
		}
		errs <- err
	}()
	<-s.Blocked(TargetArchive)

	closeServer()
	if err := <-errs; err == nil {
		t.Error("request answered after the server closed, want no answer")
	}
}
