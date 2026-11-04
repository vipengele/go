package daemoncmd_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"go.vipengele.org/cli/spec"
	"go.vipengele.org/cli/spec/speccheck"
	"go.vipengele.org/daemon"
	"go.vipengele.org/daemon/daemoncmd"
)

// recorder serves a Health's handler and keeps the last /status body it served, so a test
// compares the command's output with the very bytes the daemon answered, uptime and durations
// included.
type recorder struct {
	handler http.Handler

	mu   sync.Mutex
	body []byte
}

func (rec *recorder) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	rw := httptest.NewRecorder()
	rec.handler.ServeHTTP(rw, r)
	if r.URL.Path == "/status" {
		rec.mu.Lock()
		rec.body = rw.Body.Bytes()
		rec.mu.Unlock()
	}
	for k, v := range rw.Header() {
		w.Header()[k] = v
	}
	w.WriteHeader(rw.Code)
	_, _ = w.Write(rw.Body.Bytes())
}

func (rec *recorder) last() []byte {
	rec.mu.Lock()
	defer rec.mu.Unlock()
	return rec.body
}

// serveHealth starts a server answering with h's handler and returns it with its recorder.
func serveHealth(t *testing.T, h *daemon.Health) (*httptest.Server, *recorder) {
	t.Helper()
	rec := &recorder{handler: h.Handler()}
	srv := httptest.NewServer(rec)
	t.Cleanup(srv.Close)
	return srv, rec
}

// newHealth returns a Health holding the checks state asks for: all passing for ok, a failing
// readiness check for unready, and a failing liveness check as well for down.
func newHealth(t *testing.T, state daemon.State) *daemon.Health {
	t.Helper()
	h := daemon.NewHealth()
	add := func(name string, check daemon.Check, kinds ...daemon.Kind) {
		if err := h.Add(name, check, kinds...); err != nil {
			t.Fatalf("Add(%q): %v", name, err)
		}
	}
	pass := func(context.Context) error { return nil }
	add("loop", pass, daemon.Liveness, daemon.Readiness)
	add("cache", pass)
	switch state {
	case daemon.StateUnready:
		add("database", func(context.Context) error { return errors.New("connection refused") })
	case daemon.StateDown:
		add("database", func(context.Context) error { return errors.New("connection refused") })
		add("heartbeat", func(context.Context) error { return errors.New("stalled") }, daemon.Liveness)
	}
	return h
}

// at returns a resolver that answers base.
func at(base string) daemoncmd.Resolver {
	return func(context.Context, *spec.Invocation) (string, error) { return base, nil }
}

// run runs cmd with text or JSON output and returns what it wrote to stdout and stderr.
func run(t *testing.T, cmd *spec.Command, asJSON bool) (stdout, stderr string, err error) {
	t.Helper()
	var out, errOut bytes.Buffer
	inv := spec.NewInvocation(&spec.Env{Stdout: &out, Stderr: &errOut}, nil, nil)
	inv.Output = spec.NewOutput(spec.OutputOptions{JSON: asJSON, NoColorStdout: true, NoColorStderr: true})
	err = cmd.Run(t.Context(), inv)
	return out.String(), errOut.String(), err
}

// cannedBody is a /status envelope with fixed uptime and durations.
const cannedBody = `{
  "schema": 1,
  "status": "unready",
  "live": true,
  "ready": false,
  "started_at": "2026-11-04T03:00:00Z",
  "uptime_seconds": 3723,
  "checks": [
    {
      "name": "database",
      "kinds": [
        "readiness"
      ],
      "status": "fail",
      "error": "daemon: health check timed out after 2s",
      "duration_ms": 2000
    },
    {
      "name": "loop",
      "kinds": [
        "liveness",
        "readiness"
      ],
      "status": "pass",
      "duration_ms": 3
    }
  ]
}
`

// serveBody starts a server answering every request with code and body.
func serveBody(t *testing.T, code int, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestStatusIsAValidCommandWithNoFlags(t *testing.T) {
	cmd := daemoncmd.NewStatus(at("http://127.0.0.1:1"))
	if err := cmd.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if cmd.Name != "status" {
		t.Errorf("Name = %q, want status", cmd.Name)
	}
	if len(cmd.Flags) != 0 {
		t.Errorf("Flags = %v, want none: the App's --json and --output drive the format", cmd.Flags)
	}
}

func TestNewStatusPanicsOnNilResolver(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("NewStatus(nil) did not panic")
		}
	}()
	daemoncmd.NewStatus(nil)
}

func TestStatusTextRendersOverallAndChecks(t *testing.T) {
	srv := serveBody(t, http.StatusOK, cannedBody)
	stdout, stderr, err := run(t, daemoncmd.NewStatus(at(srv.URL)), false)
	if !errors.Is(err, daemoncmd.ErrNotOK) {
		t.Fatalf("err = %v, want ErrNotOK", err)
	}
	want := "" +
		"STATUS     LIVE  READY  UPTIME\n" +
		"⚠ unready  true  false  1h2m3s\n" +
		"\n" +
		"CHECK     KINDS               STATUS  DURATION  ERROR\n" +
		"database  readiness           ✗ fail        2s  daemon: health check timed out after 2s\n" +
		"loop      liveness,readiness  ✓ pass       3ms\n"
	if stdout != want {
		t.Errorf("stdout:\n%s\nwant:\n%s", stdout, want)
	}
	if stderr != "" {
		t.Errorf("stderr = %q, want empty", stderr)
	}
}

func TestStatusTextAgainstHandlerListsEveryCheck(t *testing.T) {
	srv, _ := serveHealth(t, newHealth(t, daemon.StateOK))
	stdout, _, err := run(t, daemoncmd.NewStatus(at(srv.URL)), false)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	lines := strings.Split(strings.TrimSuffix(stdout, "\n"), "\n")
	if len(lines) != 6 {
		t.Fatalf("stdout has %d lines, want 6:\n%s", len(lines), stdout)
	}
	if got := strings.Fields(lines[1])[:4]; strings.Join(got, " ") != "✓ ok true true" {
		t.Errorf("overall row = %q, want it to start with ✓ ok true true", lines[1])
	}
	for i, want := range []string{"cache readiness ✓ pass", "loop liveness,readiness ✓ pass"} {
		fields := strings.Fields(lines[4+i])
		if got := strings.Join(fields[:4], " "); got != want {
			t.Errorf("check row %d = %q, want it to start with %q", i, lines[4+i], want)
		}
	}
}

func TestStatusJSONIsTheHandlerBody(t *testing.T) {
	for _, state := range []daemon.State{daemon.StateOK, daemon.StateUnready, daemon.StateDown} {
		t.Run(string(state), func(t *testing.T) {
			srv, rec := serveHealth(t, newHealth(t, state))
			stdout, stderr, _ := run(t, daemoncmd.NewStatus(at(srv.URL)), true)
			if body := rec.last(); stdout != string(body) {
				t.Errorf("stdout differs from the /status body:\n%s\nbody:\n%s", stdout, body)
			}
			if stderr != "" {
				t.Errorf("stderr = %q, want empty", stderr)
			}
		})
	}
}

func TestStatusJSONKeys(t *testing.T) {
	srv, _ := serveHealth(t, newHealth(t, daemon.StateUnready))
	stdout, _, _ := run(t, daemoncmd.NewStatus(at(srv.URL)), true)
	speccheck.AssertJSONKeys(t, json.RawMessage(stdout), "testdata/status.keys")
}

func TestStatusOKReturnsNil(t *testing.T) {
	for _, asJSON := range []bool{false, true} {
		srv, _ := serveHealth(t, newHealth(t, daemon.StateOK))
		stdout, _, err := run(t, daemoncmd.NewStatus(at(srv.URL)), asJSON)
		if err != nil {
			t.Errorf("json=%v: Run = %v, want nil", asJSON, err)
		}
		if stdout == "" {
			t.Errorf("json=%v: stdout is empty, want the snapshot", asJSON)
		}
	}
}

func TestStatusNotOKIsReportedAfterPrinting(t *testing.T) {
	for _, state := range []daemon.State{daemon.StateUnready, daemon.StateDown} {
		for _, asJSON := range []bool{false, true} {
			name := string(state) + "/text"
			if asJSON {
				name = string(state) + "/json"
			}
			t.Run(name, func(t *testing.T) {
				srv, rec := serveHealth(t, newHealth(t, state))
				stdout, stderr, err := run(t, daemoncmd.NewStatus(at(srv.URL)), asJSON)
				var reported *spec.ReportedError
				if !errors.As(err, &reported) {
					t.Fatalf("err = %v, want a *spec.ReportedError", err)
				}
				if !errors.Is(err, daemoncmd.ErrNotOK) {
					t.Errorf("err = %v, want it to wrap ErrNotOK", err)
				}
				if !strings.Contains(err.Error(), string(state)) {
					t.Errorf("err = %q, want it to name %s", err, state)
				}
				if asJSON && stdout != string(rec.last()) {
					t.Errorf("stdout differs from the /status body:\n%s", stdout)
				}
				if !asJSON && !strings.Contains(stdout, string(state)) {
					t.Errorf("stdout does not show %s:\n%s", state, stdout)
				}
				if stderr != "" {
					t.Errorf("stderr = %q, want empty", stderr)
				}
			})
		}
	}
}

// assertOrdinary fails t unless err is an error the App reports itself, and nothing was printed.
func assertOrdinary(t *testing.T, err error, stdout string) {
	t.Helper()
	if err == nil {
		t.Fatal("Run returned nil, want an error")
	}
	var reported *spec.ReportedError
	if errors.As(err, &reported) {
		t.Errorf("err = %v is a *spec.ReportedError, want an ordinary error", err)
	}
	if errors.Is(err, daemoncmd.ErrNotOK) {
		t.Errorf("err = %v wraps ErrNotOK", err)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want empty", stdout)
	}
}

func TestStatusUnreachableIsAnOrdinaryError(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	base := srv.URL
	srv.Close()

	stdout, _, err := run(t, daemoncmd.NewStatus(at(base)), false)
	assertOrdinary(t, err, stdout)
}

func TestStatusNon200IsAnOrdinaryError(t *testing.T) {
	srv := serveBody(t, http.StatusServiceUnavailable, cannedBody)
	stdout, _, err := run(t, daemoncmd.NewStatus(at(srv.URL)), true)
	assertOrdinary(t, err, stdout)
	if !errors.Is(err, daemoncmd.ErrBadStatus) {
		t.Errorf("err = %v, want it to wrap ErrBadStatus", err)
	}
	if !strings.Contains(err.Error(), "503") {
		t.Errorf("err = %q, want it to name the 503", err)
	}
}

func TestStatusUnreadableBodyIsAnOrdinaryError(t *testing.T) {
	for name, body := range map[string]string{
		"not JSON":       "ok\n",
		"no schema":      `{"status":"ok","live":true,"ready":true,"started_at":"2026-11-04T03:00:00Z","checks":[]}`,
		"unknown schema": `{"schema":2,"status":"ok","live":true,"ready":true,"started_at":"2026-11-04T03:00:00Z","checks":[]}`,
		"unknown status": `{"schema":1,"status":"degraded","live":true,"ready":true,"started_at":"2026-11-04T03:00:00Z","checks":[]}`,
		"bad started_at": `{"schema":1,"status":"ok","live":true,"ready":true,"started_at":"yesterday","checks":[]}`,
	} {
		t.Run(name, func(t *testing.T) {
			srv := serveBody(t, http.StatusOK, body)
			stdout, _, err := run(t, daemoncmd.NewStatus(at(srv.URL)), true)
			assertOrdinary(t, err, stdout)
			if !errors.Is(err, daemoncmd.ErrBadStatus) {
				t.Errorf("err = %v, want it to wrap ErrBadStatus", err)
			}
		})
	}
}

func TestStatusResolverErrorIsReturned(t *testing.T) {
	errNoURL := errors.New("no listen address configured")
	cmd := daemoncmd.NewStatus(func(context.Context, *spec.Invocation) (string, error) {
		return "", errNoURL
	})
	stdout, _, err := run(t, cmd, false)
	assertOrdinary(t, err, stdout)
	if !errors.Is(err, errNoURL) {
		t.Errorf("err = %v, want it to wrap the resolver's error", err)
	}
}

func TestStatusRequestsStatusBelowTheBaseURLPath(t *testing.T) {
	h := newHealth(t, daemon.StateOK)
	mux := http.NewServeMux()
	mux.Handle("/health/", http.StripPrefix("/health", h.Handler()))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	for _, base := range []string{srv.URL + "/health", srv.URL + "/health/"} {
		if _, _, err := run(t, daemoncmd.NewStatus(at(base)), true); err != nil {
			t.Errorf("base %q: Run = %v, want nil", base, err)
		}
	}
}

func TestStatusUsesTheGivenClient(t *testing.T) {
	srv, _ := serveHealth(t, newHealth(t, daemon.StateOK))
	var calls int
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		return http.DefaultTransport.RoundTrip(r)
	})}
	if _, _, err := run(t, daemoncmd.NewStatus(at(srv.URL), daemoncmd.WithHTTPClient(client)), false); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if calls != 1 {
		t.Errorf("client made %d requests, want 1", calls)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestStatusTimeoutIsAnOrdinaryError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	t.Cleanup(srv.Close)

	cmd := daemoncmd.NewStatus(at(srv.URL), daemoncmd.WithTimeout(10*time.Millisecond))
	stdout, _, err := run(t, cmd, false)
	assertOrdinary(t, err, stdout)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("err = %v, want it to wrap context.DeadlineExceeded", err)
	}
}

// serveOK starts a server answering /status with an ok snapshot, and calls seen with each request.
func serveOK(t *testing.T, seen func(*http.Request)) *httptest.Server {
	t.Helper()
	handler := newHealth(t, daemon.StateOK).Handler()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen(r)
		handler.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestStatusTimeoutThatIsNotPositiveKeepsTheDefault(t *testing.T) {
	for _, timeout := range []time.Duration{0, -time.Second} {
		srv := serveOK(t, func(*http.Request) {})
		cmd := daemoncmd.NewStatus(at(srv.URL), daemoncmd.WithTimeout(timeout))
		if _, _, err := run(t, cmd, true); err != nil {
			t.Errorf("WithTimeout(%v): Run = %v, want nil", timeout, err)
		}
	}
}

func TestStatusAcceptsJSON(t *testing.T) {
	var accept string
	srv := serveOK(t, func(r *http.Request) { accept = r.Header.Get("Accept") })
	if _, _, err := run(t, daemoncmd.NewStatus(at(srv.URL)), true); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if accept != "application/json" {
		t.Errorf("Accept = %q, want application/json", accept)
	}
}

func TestStatusReadsABodyOfExactlyTheLimit(t *testing.T) {
	body := cannedBody + strings.Repeat(" ", daemoncmd.MaxBody-len(cannedBody))
	srv := serveBody(t, http.StatusOK, body)
	stdout, _, err := run(t, daemoncmd.NewStatus(at(srv.URL)), false)
	if !errors.Is(err, daemoncmd.ErrNotOK) {
		t.Fatalf("err = %v, want ErrNotOK for the unready snapshot", err)
	}
	if !strings.Contains(stdout, "unready") {
		t.Errorf("stdout does not show the snapshot:\n%s", stdout)
	}
}

func TestStatusBodyOverTheLimitIsAnOrdinaryError(t *testing.T) {
	body := cannedBody + strings.Repeat(" ", daemoncmd.MaxBody+1-len(cannedBody))
	srv := serveBody(t, http.StatusOK, body)
	stdout, _, err := run(t, daemoncmd.NewStatus(at(srv.URL)), false)
	assertOrdinary(t, err, stdout)
	if !errors.Is(err, daemoncmd.ErrBadStatus) {
		t.Errorf("err = %v, want it to wrap ErrBadStatus", err)
	}
	if !strings.Contains(err.Error(), "more than") {
		t.Errorf("err = %q, want it to say the body is more than the limit", err)
	}
}

func TestStatusInvalidBaseURLIsAnOrdinaryError(t *testing.T) {
	stdout, _, err := run(t, daemoncmd.NewStatus(at("http://127.0.0.1:1/%zz")), false)
	assertOrdinary(t, err, stdout)
	if !strings.Contains(err.Error(), "daemon URL") {
		t.Errorf("err = %q, want it to name the daemon URL", err)
	}
}

// failingReader fails every Read with err.
type failingReader struct{ err error }

func (r failingReader) Read([]byte) (int, error) { return 0, r.err }

func TestStatusBodyReadErrorIsAnOrdinaryError(t *testing.T) {
	errRead := errors.New("connection reset")
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			Status:     "200 OK",
			StatusCode: http.StatusOK,
			Header:     http.Header{},
			Body:       io.NopCloser(failingReader{errRead}),
			Request:    r,
		}, nil
	})}
	cmd := daemoncmd.NewStatus(at("http://127.0.0.1:1"), daemoncmd.WithHTTPClient(client))
	stdout, _, err := run(t, cmd, false)
	assertOrdinary(t, err, stdout)
	if !errors.Is(err, errRead) {
		t.Errorf("err = %v, want it to wrap the read error", err)
	}
}

// limitWriter accepts n bytes and fails every write past them with err.
type limitWriter struct {
	n   int
	err error
}

func (w *limitWriter) Write(p []byte) (int, error) {
	if len(p) <= w.n {
		w.n -= len(p)
		return len(p), nil
	}
	written := w.n
	w.n = 0
	return written, w.err
}

// runTo runs cmd with text or JSON output written to stdout.
func runTo(t *testing.T, cmd *spec.Command, asJSON bool, stdout io.Writer) error {
	t.Helper()
	inv := spec.NewInvocation(&spec.Env{Stdout: stdout, Stderr: io.Discard}, nil, nil)
	inv.Output = spec.NewOutput(spec.OutputOptions{JSON: asJSON, NoColorStdout: true, NoColorStderr: true})
	return cmd.Run(t.Context(), inv)
}

func TestStatusWriteErrorIsReturned(t *testing.T) {
	srv := serveBody(t, http.StatusOK, cannedBody)
	text, _, _ := run(t, daemoncmd.NewStatus(at(srv.URL)), false)
	overall := strings.Index(text, "\n\n") + 1
	tests := []struct {
		name   string
		asJSON bool
		limit  int
	}{
		{name: "json", asJSON: true},
		{name: "overall table", limit: 0},
		{name: "blank line", limit: overall},
		{name: "checks table", limit: overall + 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errWrite := errors.New("broken pipe")
			err := runTo(t, daemoncmd.NewStatus(at(srv.URL)), tt.asJSON, &limitWriter{n: tt.limit, err: errWrite})
			if !errors.Is(err, errWrite) {
				t.Errorf("err = %v, want it to wrap the write error", err)
			}
			if errors.Is(err, daemoncmd.ErrNotOK) {
				t.Errorf("err = %v wraps ErrNotOK, want the write error alone", err)
			}
		})
	}
}
