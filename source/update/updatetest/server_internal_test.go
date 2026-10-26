package updatetest

import (
	"context"
	"net/http"
	"slices"
	"sync"
	"testing"
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
