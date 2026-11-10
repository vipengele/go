package daemon_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync/atomic"
	"time"

	"go.vipengele.org/cli/spec"
	"go.vipengele.org/cli/spec/paths"
	"go.vipengele.org/daemon"
)

// serve runs a daemon of three Runners: a background worker at the default Order, an HTTP server
// at DefaultHTTPOrder, and a client that starts once the server is ready, calls it, then asks for
// a restart. A requested restart is a clean exit, so serve returns nil for it.
func serve(ctx context.Context) error {
	var lc net.ListenConfig
	l, err := lc.Listen(ctx, "tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	url := "http://" + l.Addr().String() + "/"

	worker := daemon.Func("worker", func(ctx context.Context) error {
		<-ctx.Done()
		fmt.Println("worker stopped")
		return ctx.Err()
	})

	srv := &http.Server{
		ReadHeaderTimeout: 5 * time.Second,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			fmt.Println("handler:", r.Method, r.URL.Path)
			_, _ = io.WriteString(w, "hello")
		}),
	}
	server := daemon.NewHTTPServerListener(srv, l)

	client := daemon.Func("client", func(ctx context.Context) error {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return err
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return err
		}
		body, err := io.ReadAll(resp.Body)
		if closeErr := resp.Body.Close(); err == nil {
			err = closeErr
		}
		if err != nil {
			return err
		}
		fmt.Println("client got:", string(body))
		return daemon.ErrRestartRequired
	}, daemon.WithOrder(daemon.DefaultHTTPOrder+1))

	sup, err := daemon.New([]daemon.Runner{client, server, worker})
	if err != nil {
		return err
	}

	err = sup.Run(ctx)
	if daemon.RestartRequested(err) {
		fmt.Println("restart requested")
		return nil
	}
	return err
}

func ExampleSupervisor() {
	// A daemon passes a context its signal handler cancels; this one stops when the client asks
	// for a restart.
	fmt.Println("serve:", serve(context.Background()))
	// Output:
	// handler: GET /
	// client got: hello
	// worker stopped
	// restart requested
	// serve: <nil>
}

// exampleIdentity is the identity of the tool in the instance lock examples.
func exampleIdentity() spec.Identity {
	return spec.Identity{
		Name:      "demo",
		Owner:     "vipengele",
		Repo:      "demo",
		EnvPrefix: "DEMO",
		DirName:   "demo",
	}
}

// A program without a Supervisor holds the instance lock itself for as long as it works.
func ExampleAcquireInstance() {
	in, err := daemon.AcquireInstance(exampleIdentity(), paths.Options{})
	if err != nil {
		var running *daemon.AlreadyRunningError
		if errors.As(err, &running) {
			fmt.Println("another instance holds", running.Path, "pid", running.PID)
			return
		}
		fmt.Println("instance lock:", err)
		return
	}
	defer in.Release()

	fmt.Println("running as the only instance; lock", in.Path())
}

// A program run by a Supervisor has Run hold the instance lock from before the first Start until
// every Runner has been stopped.
func ExampleWithInstanceLock() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	worker := daemon.Func("worker", func(ctx context.Context) error {
		<-ctx.Done()
		return ctx.Err()
	})

	sup, err := daemon.New([]daemon.Runner{worker},
		daemon.WithInstanceLock(exampleIdentity(), paths.Options{}))
	if err != nil {
		fmt.Println("new:", err)
		return
	}

	err = sup.Run(ctx)
	if errors.Is(err, daemon.ErrAlreadyRunning) {
		fmt.Println("already running:", err)
		return
	}
	if err != nil {
		fmt.Println("run:", err)
	}
}

// A program run as a systemd service with Type=notify tells systemd when it is ready, that it is
// stopping and, with WatchdogSec=, that its liveness checks pass. A program on cli/core passes the
// LookupEnv of its spec.Env; os.LookupEnv stands in for it here. Outside systemd nothing is sent.
func ExampleWithSystemd() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	health := daemon.NewHealth()
	worker := daemon.Func("worker", func(ctx context.Context) error {
		<-ctx.Done()
		return ctx.Err()
	})

	sup, err := daemon.New([]daemon.Runner{worker},
		daemon.WithHealth(health),
		daemon.WithSystemd(os.LookupEnv))
	if err != nil {
		fmt.Println("new:", err)
		return
	}

	if err := sup.Run(ctx); err != nil {
		fmt.Println("run:", err)
	}
}

// reloadable is a Runner that applies a new configuration without restarting.
type reloadable struct {
	daemon.Runner
}

func (reloadable) Reload(_ context.Context, cfg string) error {
	fmt.Println("reloaded:", cfg)
	return nil
}

// A program reloads its configuration on SIGHUP: Run calls the load function and offers the result
// to every running Runner that implements Reloader for its type. A program on cli/core passes a
// closure over its loader; load stands in for it here.
func ExampleWithReload() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	load := func(context.Context) (string, error) { return "config", nil }

	worker := reloadable{daemon.Func("worker", func(ctx context.Context) error {
		<-ctx.Done()
		return ctx.Err()
	})}

	sup, err := daemon.New([]daemon.Runner{worker},
		daemon.WithReload(func(ctx context.Context) (string, error) { return load(ctx) }))
	if err != nil {
		fmt.Println("new:", err)
		return
	}

	if err := sup.Run(ctx); err != nil {
		fmt.Println("run:", err)
	}
}

// probe requests path on base and returns the status code and the body without its newline.
func probe(ctx context.Context, base, path string) (int, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+path, nil)
	if err != nil {
		return 0, "", err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, "", err
	}
	body, err := io.ReadAll(resp.Body)
	if closeErr := resp.Body.Close(); err == nil {
		err = closeErr
	}
	return resp.StatusCode, strings.TrimSpace(string(body)), err
}

// serveHealth runs a daemon whose HTTP server mounts the Health handler, probes it, and stops the
// daemon. The supervisor.ready check passes once every Runner has started, so the probe retries
// until /readyz answers 200.
func serveHealth(ctx context.Context) error {
	var lc net.ListenConfig
	l, err := lc.Listen(ctx, "tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	base := "http://" + l.Addr().String()

	var dbUp atomic.Bool
	dbUp.Store(true)

	health := daemon.NewHealth()
	err = health.Add("database", func(context.Context) error {
		if !dbUp.Load() {
			return errors.New("connection refused")
		}
		return nil
	}, daemon.Readiness)
	if err != nil {
		return err
	}

	srv := &http.Server{ReadHeaderTimeout: 5 * time.Second, Handler: health.Handler()}
	sup, err := daemon.New([]daemon.Runner{daemon.NewHTTPServerListener(srv, l)}, daemon.WithHealth(health))
	if err != nil {
		return err
	}

	runCtx, stop := context.WithCancel(ctx)
	defer stop()
	runErr := make(chan error, 1)
	go func() { runErr <- sup.Run(runCtx) }()

	probeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	show := func(path string) error {
		code, body, err := probe(probeCtx, base, path)
		if err != nil {
			return err
		}
		fmt.Println(path, code, body)
		return nil
	}

	for {
		code, _, err := probe(probeCtx, base, "/readyz")
		if err == nil && code == http.StatusOK {
			break
		}
		select {
		case <-probeCtx.Done():
			stop()
			<-runErr
			return probeCtx.Err()
		case <-time.After(5 * time.Millisecond):
		}
	}

	paths := []string{"/livez", "/readyz"}
	for _, path := range paths {
		if err := show(path); err != nil {
			stop()
			<-runErr
			return err
		}
	}

	// A failing readiness check takes the daemon out of traffic without failing liveness.
	dbUp.Store(false)
	for _, path := range paths {
		if err := show(path); err != nil {
			stop()
			<-runErr
			return err
		}
	}

	stop()
	return <-runErr
}

func ExampleHealth() {
	fmt.Println("serveHealth:", serveHealth(context.Background()))
	// Output:
	// /livez 200 ok
	// /readyz 200 ok
	// /livez 200 ok
	// /readyz 503 unready
	// serveHealth: <nil>
}
