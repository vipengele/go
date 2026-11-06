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
