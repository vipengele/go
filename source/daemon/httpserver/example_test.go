package httpserver_test

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"

	"go.vipengele.org/daemon"
	"go.vipengele.org/daemon/httpserver"
)

// get requests url with the bearer token, when there is one, and returns the status.
func get(ctx context.Context, url, token string) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, err
	}
	_, err = io.Copy(io.Discard, resp.Body)
	if closeErr := resp.Body.Close(); err == nil {
		err = closeErr
	}
	return resp.StatusCode, err
}

func serve(ctx context.Context) error {
	var lc net.ListenConfig
	l, err := lc.Listen(ctx, "tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	base := "http://" + l.Addr().String()

	mux := http.NewServeMux()
	mux.HandleFunc("/livez", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "ok")
	})
	mux.HandleFunc("/jobs", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "[]")
	})

	srv, err := httpserver.New(mux,
		httpserver.WithBearerTokenFunc(func() string { return "s3cret" }),
		httpserver.WithUnauthenticatedPaths("/livez"),
	)
	if err != nil {
		return err
	}
	server := daemon.NewHTTPServerListener(srv, l)

	client := daemon.Func("client", func(ctx context.Context) error {
		for _, c := range []struct{ path, token string }{
			{"/livez", ""},
			{"/jobs", ""},
			{"/jobs", "s3cret"},
		} {
			status, err := get(ctx, base+c.path, c.token)
			if err != nil {
				return err
			}
			fmt.Printf("GET %s with token %t: %d\n", c.path, c.token != "", status)
		}
		return daemon.ErrRestartRequired
	}, daemon.WithOrder(daemon.DefaultHTTPOrder+1))

	sup, err := daemon.New([]daemon.Runner{client, server})
	if err != nil {
		return err
	}
	if err := sup.Run(ctx); !daemon.RestartRequested(err) {
		return err
	}
	return nil
}

func Example() {
	// The probe path is open; every other path needs the bearer token.
	fmt.Println("serve:", serve(context.Background()))
	// Output:
	// GET /livez with token false: 200
	// GET /jobs with token false: 401
	// GET /jobs with token true: 200
	// serve: <nil>
}
