package daemon_test

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

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
