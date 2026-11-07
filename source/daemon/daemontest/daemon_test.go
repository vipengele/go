package daemontest

import (
	"context"
	"errors"
	"io"
	"net/http"
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
