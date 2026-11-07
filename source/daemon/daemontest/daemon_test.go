package daemontest

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"go.vipengele.org/daemon"
)

func ok() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "hello")
	})
}

func TestStartServesHandlerOnReturn(t *testing.T) {
	d := Start(t, Config{Handler: ok()})

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, d.URL()+"/", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "hello" {
		t.Fatalf("body = %q, want hello", body)
	}
	if d.Addr() == "" || d.URL() != "http://"+d.Addr() {
		t.Fatalf("Addr = %q, URL = %q", d.Addr(), d.URL())
	}
}

func TestStartWithoutHandlerHasNoAddress(t *testing.T) {
	d := Start(t, Config{Runners: []daemon.Runner{NewRunner(t, "a")}})
	if d.Addr() != "" || d.URL() != "" {
		t.Fatalf("Addr = %q, URL = %q, want empty", d.Addr(), d.URL())
	}
}

func TestStopIsIdempotent(t *testing.T) {
	d := Start(t, Config{Handler: ok()})
	if err := d.Err(); err != nil {
		t.Fatalf("Err before stop = %v", err)
	}
	err1 := d.Stop()
	err2 := d.Stop()
	if err1 != nil || err2 != nil {
		t.Fatalf("Stop = %v, %v, want nil, nil", err1, err2)
	}
	select {
	case <-d.Done():
	default:
		t.Fatal("Done not closed after Stop")
	}
	if err := d.Err(); err != nil {
		t.Fatalf("Err = %v", err)
	}
}

func TestEarlyExitIsReportedByErr(t *testing.T) {
	boom := errors.New("boom")
	d := Start(t, Config{Runners: []daemon.Runner{NewRunner(t, "bad", FailStart(boom))}})
	select {
	case <-d.Done():
	default:
		t.Fatal("Done not closed after early exit")
	}
	if err := d.Err(); !errors.Is(err, boom) {
		t.Fatalf("Err = %v, want %v", err, boom)
	}
	if err := d.Stop(); !errors.Is(err, boom) {
		t.Fatalf("Stop = %v, want %v", err, boom)
	}
}

func TestStopObservesStopTimeout(t *testing.T) {
	const grace = 30 * time.Second
	clock := NewClock(time.Unix(0, 0))
	r := NewRunner(t, "stuck", IgnoreStop())
	d := Start(t, Config{
		Runners: []daemon.Runner{r},
		Options: []daemon.Option{daemon.WithClock(clock), daemon.WithGrace(grace)},
	})

	stopped := make(chan error, 1)
	go func() { stopped <- d.Stop() }()

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	select {
	case <-r.Stopped():
	case <-ctx.Done():
		t.Fatal("Stop never reached the runner")
	}
	if err := clock.BlockUntilTimers(ctx, 1); err != nil {
		t.Fatal(err)
	}
	clock.Advance(grace)

	select {
	case err := <-stopped:
		if !errors.Is(err, daemon.ErrStopTimeout) {
			t.Fatalf("Stop = %v, want ErrStopTimeout", err)
		}
	case <-ctx.Done():
		t.Fatal("Stop did not return after the grace period")
	}
}

func TestCleanupReleasesIgnoreStopRunners(t *testing.T) {
	begin := time.Now()
	t.Run("sub", func(t *testing.T) {
		Start(t, Config{Runners: []daemon.Runner{NewRunner(t, "stuck", IgnoreStop())}})
	})
	if elapsed := time.Since(begin); elapsed > 5*time.Second {
		t.Fatalf("cleanup took %v, want far under the default grace", elapsed)
	}
}

// recordingTB is a testing.TB that counts its Helper calls and holds the functions passed to
// Cleanup until runCleanups, which the test's own cleanup also calls.
type recordingTB struct {
	testing.TB
	helpers  int
	cleanups []func()
}

func newRecordingTB(t *testing.T) *recordingTB {
	tb := &recordingTB{TB: t}
	t.Cleanup(tb.runCleanups)
	return tb
}

func (r *recordingTB) Helper() {
	r.helpers++
	r.TB.Helper()
}

func (r *recordingTB) Cleanup(f func()) { r.cleanups = append(r.cleanups, f) }

// runCleanups runs the held cleanups, last-registered first, and forgets them.
func (r *recordingTB) runCleanups() {
	for len(r.cleanups) > 0 {
		f := r.cleanups[len(r.cleanups)-1]
		r.cleanups = r.cleanups[:len(r.cleanups)-1]
		f()
	}
}

// Start and start each mark themselves helpers, so a failure inside either is reported at the
// test's call to Start.
func TestStartAndStartAreHelpers(t *testing.T) {
	tb := newRecordingTB(t)
	Start(tb, Config{Handler: ok()})
	if tb.helpers != 2 {
		t.Fatalf("Helper called %d times, want 2: once by Start and once by start", tb.helpers)
	}
}

// fatalTB is a testing.TB whose Fatalf records the message and ends the calling goroutine, so a
// test can observe Start failing the test without failing itself.
type fatalTB struct {
	testing.TB
	fatal chan string
}

func (f *fatalTB) Fatalf(format string, args ...any) {
	f.fatal <- fmt.Sprintf(format, args...)
	runtime.Goexit()
}

// startFatal runs start with cfg and lc on a fatalTB and returns the message it fails with. It
// fails the test when start returns instead.
func startFatal(t *testing.T, cfg Config, lc *net.ListenConfig) string {
	t.Helper()
	tb := &fatalTB{TB: t, fatal: make(chan string, 1)}
	exited := make(chan struct{})
	go func() {
		defer close(exited)
		start(tb, cfg, lc)
	}()
	<-exited
	select {
	case msg := <-tb.fatal:
		return msg
	default:
		t.Fatal("start returned, want it to fail the test")
		return ""
	}
}

func TestStartFailsTheTestWhenTheListenerCannotBind(t *testing.T) {
	refused := errors.New("refused")
	lc := &net.ListenConfig{Control: func(string, string, syscall.RawConn) error { return refused }}
	msg := startFatal(t, Config{Handler: ok()}, lc)
	if !strings.HasPrefix(msg, "daemontest: listen: ") || !strings.Contains(msg, refused.Error()) {
		t.Errorf("Fatalf message = %q, want a listen failure carrying %q", msg, refused)
	}
}

func TestStartClosesTheListenerAndFailsTheTestWhenNewFails(t *testing.T) {
	var conn syscall.RawConn
	lc := &net.ListenConfig{Control: func(_, _ string, c syscall.RawConn) error {
		conn = c
		return nil
	}}
	msg := startFatal(t, Config{Handler: ok(), Options: []daemon.Option{daemon.WithClock(nil)}}, lc)
	if !strings.HasPrefix(msg, "daemontest: daemon.New: ") {
		t.Errorf("Fatalf message = %q, want a daemon.New failure", msg)
	}
	// The listener's descriptor refuses every use once the listener is closed.
	if err := conn.Control(func(uintptr) {}); err == nil {
		t.Error("the listener is open after daemon.New failed, want it closed")
	}
}
