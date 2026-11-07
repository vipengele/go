package daemontest

import (
	"context"
	"net"
	"net/http"
	"slices"
	"sync"
	"testing"
	"time"

	"go.vipengele.org/daemon"
)

// readHeaderTimeout bounds how long the server of a [Config.Handler] waits for request headers.
const readHeaderTimeout = 10 * time.Second

// Config describes the daemon [Start] runs.
type Config struct {
	// Handler, when set, is served on 127.0.0.1:0 by an HTTP Runner added after Runners.
	Handler http.Handler
	// Runners are the Runners the Supervisor runs.
	Runners []daemon.Runner
	// Options are the Supervisor's options. Start adds [daemon.WithReady] after them.
	Options []daemon.Option
}

// Daemon is a [daemon.Supervisor] that [Start] runs. It is safe for concurrent use.
type Daemon struct {
	addr string

	cancel     context.CancelFunc
	cancelOnce sync.Once

	done chan struct{}
	// err is Run's error; it is written before done closes.
	err error
}

// Start runs a Supervisor over cfg's Runners, plus an HTTP Runner serving cfg.Handler on
// 127.0.0.1:0 when Handler is set, and returns once every Runner has started or Run has returned,
// whichever comes first. A Handler is therefore reachable at [Daemon.URL] as soon as Start returns.
// A Run that returns early does not fail the test: [Daemon.Done] is then closed and [Daemon.Err]
// reports why.
//
// A New error, or a failure to bind, fails the test. The test's cleanup calls [Runner.Release] on
// each [*Runner] in cfg.Runners, so an [IgnoreStop] Runner does not hold the cleanup for the
// grace period, and then [Daemon.Stop]. Calling Stop in the test does not release the Runners, so
// a test of the grace period observes [daemon.ErrStopTimeout].
func Start(t testing.TB, cfg Config) *Daemon {
	t.Helper()
	return start(t, cfg, &net.ListenConfig{})
}

// start is Start, binding the Handler's listener through lc.
func start(t testing.TB, cfg Config, lc *net.ListenConfig) *Daemon {
	t.Helper()

	runners := slices.Clone(cfg.Runners)
	d := &Daemon{done: make(chan struct{})}
	var l net.Listener
	if cfg.Handler != nil {
		var err error
		l, err = lc.Listen(t.Context(), "tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("daemontest: listen: %v", err)
		}
		d.addr = l.Addr().String()
		srv := &http.Server{Handler: cfg.Handler, ReadHeaderTimeout: readHeaderTimeout}
		runners = append(runners, daemon.NewHTTPServerListener(srv, l))
	}

	ready := make(chan struct{})
	var readyOnce sync.Once
	opts := append(slices.Clone(cfg.Options), daemon.WithReady(func() {
		readyOnce.Do(func() { close(ready) })
	}))
	s, err := daemon.New(runners, opts...)
	if err != nil {
		if l != nil {
			_ = l.Close()
		}
		t.Fatalf("daemontest: daemon.New: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	// Stop calls cancel through this closure rather than the stored func itself, so gosec's G118
	// sees the cancel func called in the function that creates it.
	d.cancel = func() { cancel() }
	go func() {
		d.err = s.Run(ctx)
		close(d.done)
	}()

	t.Cleanup(func() {
		for _, r := range cfg.Runners {
			if tr, ok := r.(*Runner); ok {
				tr.Release()
			}
		}
		_ = d.Stop()
	})

	select {
	case <-ready:
	case <-d.done:
	}
	return d
}

// Addr returns the address the Handler is served on, "127.0.0.1:NNNN", or "" without a Handler.
func (d *Daemon) Addr() string { return d.addr }

// URL returns "http://" followed by [Daemon.Addr], or "" without a Handler.
func (d *Daemon) URL() string {
	if d.addr == "" {
		return ""
	}
	return "http://" + d.addr
}

// Done is closed once Run has returned.
func (d *Daemon) Done() <-chan struct{} { return d.done }

// Err returns Run's error once [Daemon.Done] is closed, and nil before.
func (d *Daemon) Err() error {
	select {
	case <-d.done:
		return d.err
	default:
		return nil
	}
}

// Stop cancels Run's context, waits for Run to return and returns Run's error. Every call returns
// the same error. Stop does not release the [Runner]s of the Config.
func (d *Daemon) Stop() error {
	d.cancelOnce.Do(d.cancel)
	<-d.done
	return d.err
}
