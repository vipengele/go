package daemoncmd_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"go.vipengele.org/cli/spec"
	"go.vipengele.org/cli/spec/speccheck"
	"go.vipengele.org/daemon"
	"go.vipengele.org/daemon/daemoncmd"
)

// fullUpdate is an update status with every field set.
func fullUpdate(state daemon.UpdateState) daemon.UpdateStatus {
	return daemon.UpdateStatus{
		State:        state,
		FromVersion:  "v1.4.0",
		ToVersion:    "v1.5.0",
		BootAttempts: 2,
		Reason:       "awaiting confirmation",
		StartedAt:    time.Date(2026, 11, 4, 3, 20, 0, 0, time.UTC),
		UpdatedAt:    time.Date(2026, 11, 4, 3, 21, 30, 0, time.UTC),
		Channel:      "stable",
		Watermark:    "v1.5.0",
		Error:        "first boot exited 1",
	}
}

// serveUpdate starts a daemon whose /status carries update, or no update when update is nil.
func serveUpdate(t *testing.T, update *daemon.UpdateStatus) *httptest.Server {
	t.Helper()
	var opts []daemon.HealthOption
	if update != nil {
		u := *update
		opts = append(opts, daemon.WithUpdateReporter(func(context.Context) daemon.UpdateStatus { return u }))
	}
	h := daemon.NewHealth(opts...)
	if err := h.Add("loop", func(context.Context) error { return nil }, daemon.Liveness, daemon.Readiness); err != nil {
		t.Fatalf("Add: %v", err)
	}
	srv, _ := serveHealth(t, h)
	return srv
}

// store is a fake fallback that returns status and counts its calls.
type store struct {
	status daemon.UpdateStatus
	calls  int
}

func (s *store) report(context.Context) daemon.UpdateStatus {
	s.calls++
	return s.status
}

// decodeReport decodes the command's JSON output into a map.
func decodeReport(t *testing.T, stdout string) map[string]any {
	t.Helper()
	var got map[string]any
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, stdout)
	}
	return got
}

// assertReported fails t unless err is a *spec.ReportedError wrapping ErrUpdateNotOK.
func assertReported(t *testing.T, err error) {
	t.Helper()
	var reported *spec.ReportedError
	if !errors.As(err, &reported) {
		t.Fatalf("err = %v, want a *spec.ReportedError", err)
	}
	if !errors.Is(err, daemoncmd.ErrUpdateNotOK) {
		t.Errorf("err = %v, want it to wrap ErrUpdateNotOK", err)
	}
}

func TestUpdateStatusIsAValidCommandWithNoFlags(t *testing.T) {
	cmd := daemoncmd.NewUpdateStatus(at("http://127.0.0.1:1"), nil)
	if err := cmd.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if cmd.Name != "update-status" {
		t.Errorf("Name = %q, want update-status", cmd.Name)
	}
	if len(cmd.Flags) != 0 {
		t.Errorf("Flags = %v, want none: the App's --json and --output drive the format", cmd.Flags)
	}
}

func TestNewUpdateStatusPanicsOnNilResolver(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("NewUpdateStatus(nil, nil) did not panic")
		}
	}()
	daemoncmd.NewUpdateStatus(nil, nil)
}

func TestUpdateStatusReadsTheDaemonFirst(t *testing.T) {
	update := daemon.UpdateStatus{State: daemon.UpdateConfirmed, FromVersion: "v1.4.0", ToVersion: "v1.5.0"}
	srv := serveUpdate(t, &update)
	fallback := &store{status: daemon.UpdateStatus{State: daemon.UpdateIdle}}

	stdout, stderr, err := run(t, daemoncmd.NewUpdateStatus(at(srv.URL), fallback.report), true)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	got := decodeReport(t, stdout)
	if got["source"] != "daemon" || got["status"] != "confirmed" || got["to_version"] != "v1.5.0" {
		t.Errorf("report = %v, want source daemon, status confirmed, to_version v1.5.0", got)
	}
	if _, ok := got["daemon_error"]; ok {
		t.Errorf("report has a daemon_error: %v", got)
	}
	if fallback.calls != 0 {
		t.Errorf("fallback called %d times, want 0", fallback.calls)
	}
	if stderr != "" {
		t.Errorf("stderr = %q, want empty", stderr)
	}
}

func TestUpdateStatusJSONIsFlatWithSchemaFirst(t *testing.T) {
	update := daemon.UpdateStatus{State: daemon.UpdatePending, ToVersion: "v1.5.0"}
	srv := serveUpdate(t, &update)
	stdout, _, err := run(t, daemoncmd.NewUpdateStatus(at(srv.URL), nil), true)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	want := "{\n  \"schema\": 1,\n  \"source\": \"daemon\",\n  \"status\": \"pending\",\n  \"to_version\": \"v1.5.0\"\n}\n"
	if stdout != want {
		t.Errorf("stdout:\n%s\nwant:\n%s", stdout, want)
	}
}

func TestUpdateStatusDaemonWithoutUpdateReadsTheStore(t *testing.T) {
	srv := serveUpdate(t, nil)
	fallback := &store{status: daemon.UpdateStatus{State: daemon.UpdateIdle, Channel: "stable"}}

	stdout, stderr, err := run(t, daemoncmd.NewUpdateStatus(at(srv.URL), fallback.report), true)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	got := decodeReport(t, stdout)
	if got["source"] != "store" || got["status"] != "idle" || got["channel"] != "stable" {
		t.Errorf("report = %v, want source store, status idle, channel stable", got)
	}
	if _, ok := got["daemon_error"]; ok {
		t.Errorf("report has a daemon_error for a reachable daemon: %v", got)
	}
	if fallback.calls != 1 {
		t.Errorf("fallback called %d times, want 1", fallback.calls)
	}
	if stderr != "" {
		t.Errorf("stderr = %q, want empty for a reachable daemon", stderr)
	}
}

func TestUpdateStatusDaemonWithoutUpdateAndNoFallbackIsNone(t *testing.T) {
	srv := serveUpdate(t, nil)

	stdout, stderr, err := run(t, daemoncmd.NewUpdateStatus(at(srv.URL), nil), true)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if want := "{\n  \"schema\": 1,\n  \"source\": \"none\"\n}\n"; stdout != want {
		t.Errorf("stdout:\n%s\nwant:\n%s", stdout, want)
	}
	if stderr != "" {
		t.Errorf("stderr = %q, want empty", stderr)
	}

	stdout, _, err = run(t, daemoncmd.NewUpdateStatus(at(srv.URL), nil), false)
	if err != nil {
		t.Fatalf("text: Run: %v", err)
	}
	if want := "self-update is not configured\n"; stdout != want {
		t.Errorf("text stdout = %q, want %q", stdout, want)
	}
}

// failingDaemons returns the ways a daemon fails to give an update, each as the command to run
// with fallback, and what the daemon_error says.
func failingDaemons(t *testing.T) map[string]struct {
	cmd  func(fallback daemon.UpdateReporter) *spec.Command
	want string
} {
	t.Helper()
	closed := httptest.NewServer(http.NotFoundHandler())
	refused := closed.URL
	closed.Close()

	hang := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	t.Cleanup(hang.Close)

	unavailable := serveBody(t, http.StatusServiceUnavailable, cannedBody)
	bad := serveBody(t, http.StatusOK, "ok\n")

	return map[string]struct {
		cmd  func(fallback daemon.UpdateReporter) *spec.Command
		want string
	}{
		"connection refused": {
			cmd:  func(f daemon.UpdateReporter) *spec.Command { return daemoncmd.NewUpdateStatus(at(refused), f) },
			want: "reach the daemon",
		},
		"timeout": {
			cmd: func(f daemon.UpdateReporter) *spec.Command {
				return daemoncmd.NewUpdateStatus(at(hang.URL), f, daemoncmd.WithTimeout(10*time.Millisecond))
			},
			want: "deadline exceeded",
		},
		"non-200": {
			cmd:  func(f daemon.UpdateReporter) *spec.Command { return daemoncmd.NewUpdateStatus(at(unavailable.URL), f) },
			want: "503",
		},
		"bad body": {
			cmd:  func(f daemon.UpdateReporter) *spec.Command { return daemoncmd.NewUpdateStatus(at(bad.URL), f) },
			want: "bad status response",
		},
		"resolve error": {
			cmd: func(f daemon.UpdateReporter) *spec.Command {
				return daemoncmd.NewUpdateStatus(func(context.Context, *spec.Invocation) (string, error) {
					return "", errors.New("no listen address configured")
				}, f)
			},
			want: "no listen address configured",
		},
	}
}

func TestUpdateStatusFailingDaemonReadsTheStore(t *testing.T) {
	for name, tt := range failingDaemons(t) {
		t.Run(name+"/json", func(t *testing.T) {
			fallback := &store{status: daemon.UpdateStatus{State: daemon.UpdateConfirmed}}
			stdout, stderr, err := run(t, tt.cmd(fallback.report), true)
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
			if fallback.calls != 1 {
				t.Errorf("fallback called %d times, want 1", fallback.calls)
			}
			got := decodeReport(t, stdout)
			if got["source"] != "store" || got["status"] != "confirmed" {
				t.Errorf("report = %v, want source store, status confirmed", got)
			}
			daemonErr, _ := got["daemon_error"].(string)
			if !strings.Contains(daemonErr, tt.want) {
				t.Errorf("daemon_error = %q, want it to contain %q", daemonErr, tt.want)
			}
			if !strings.Contains(stderr, "read the update status from the store") {
				t.Errorf("stderr = %q, want the note that the store was read", stderr)
			}
		})
		t.Run(name+"/text", func(t *testing.T) {
			fallback := &store{status: daemon.UpdateStatus{State: daemon.UpdateConfirmed}}
			stdout, stderr, err := run(t, tt.cmd(fallback.report), false)
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
			want := "" +
				"FIELD   VALUE\n" +
				"source  store\n" +
				"status  confirmed\n"
			if stdout != want {
				t.Errorf("stdout:\n%s\nwant:\n%s", stdout, want)
			}
			if !strings.Contains(stderr, "read the update status from the store") || !strings.Contains(stderr, tt.want) {
				t.Errorf("stderr = %q, want the note naming %q", stderr, tt.want)
			}
		})
	}
}

func TestUpdateStatusFailingDaemonWithoutFallbackIsAnOrdinaryError(t *testing.T) {
	for name, tt := range failingDaemons(t) {
		t.Run(name, func(t *testing.T) {
			stdout, stderr, err := run(t, tt.cmd(nil), false)
			if err == nil {
				t.Fatal("Run returned nil, want an error")
			}
			var reported *spec.ReportedError
			if errors.As(err, &reported) {
				t.Errorf("err = %v is a *spec.ReportedError, want an ordinary error", err)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("err = %q, want it to contain %q", err, tt.want)
			}
			if stdout != "" || stderr != "" {
				t.Errorf("stdout = %q, stderr = %q, want both empty", stdout, stderr)
			}
		})
	}
}

func TestUpdateStatusCancelledContextDoesNotReadTheStore(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		cancel()
		<-r.Context().Done()
	}))
	t.Cleanup(srv.Close)
	fallback := &store{status: daemon.UpdateStatus{State: daemon.UpdateIdle}}

	var out, errOut strings.Builder
	inv := spec.NewInvocation(&spec.Env{Stdout: &out, Stderr: &errOut}, nil, nil)
	inv.Output = spec.NewOutput(spec.OutputOptions{NoColorStdout: true, NoColorStderr: true})
	err := daemoncmd.NewUpdateStatus(at(srv.URL), fallback.report).Run(ctx, inv)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want it to wrap context.Canceled", err)
	}
	var reported *spec.ReportedError
	if errors.As(err, &reported) {
		t.Errorf("err = %v is a *spec.ReportedError, want an ordinary error", err)
	}
	if fallback.calls != 0 {
		t.Errorf("fallback called %d times, want 0", fallback.calls)
	}
	if out.String() != "" || errOut.String() != "" {
		t.Errorf("stdout = %q, stderr = %q, want both empty", out.String(), errOut.String())
	}
}

func TestUpdateStatusCancelledContextBeforeResolveDoesNotReadTheStore(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	resolve := func(ctx context.Context, _ *spec.Invocation) (string, error) { return "", ctx.Err() }
	fallback := &store{status: daemon.UpdateStatus{State: daemon.UpdateIdle}}

	inv := spec.NewInvocation(&spec.Env{Stdout: &strings.Builder{}, Stderr: &strings.Builder{}}, nil, nil)
	err := daemoncmd.NewUpdateStatus(resolve, fallback.report).Run(ctx, inv)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want it to wrap context.Canceled", err)
	}
	if fallback.calls != 0 {
		t.Errorf("fallback called %d times, want 0", fallback.calls)
	}
}

func TestUpdateStatusFailedUpdateIsReportedAfterPrinting(t *testing.T) {
	tests := map[string]daemon.UpdateStatus{
		"rolled_back":   {State: daemon.UpdateRolledBack, Reason: "boot attempts exhausted"},
		"unknown":       {State: daemon.UpdateUnknown},
		"error on idle": {State: daemon.UpdateIdle, Error: "check for updates: connection refused"},
	}
	for name, update := range tests {
		for _, asJSON := range []bool{false, true} {
			label := name + "/text"
			if asJSON {
				label = name + "/json"
			}
			t.Run(label, func(t *testing.T) {
				srv := serveUpdate(t, &update)
				stdout, stderr, err := run(t, daemoncmd.NewUpdateStatus(at(srv.URL), nil), asJSON)
				assertReported(t, err)
				if !strings.Contains(err.Error(), string(update.State)) {
					t.Errorf("err = %q, want it to name %s", err, update.State)
				}
				if update.Error != "" && !strings.Contains(err.Error(), update.Error) {
					t.Errorf("err = %q, want it to carry the update's error", err)
				}
				if !strings.Contains(stdout, string(update.State)) {
					t.Errorf("stdout does not show %s:\n%s", update.State, stdout)
				}
				if stderr != "" {
					t.Errorf("stderr = %q, want empty", stderr)
				}
			})
		}
	}
}

func TestUpdateStatusFailedUpdateFromTheStoreIsReported(t *testing.T) {
	srv := serveUpdate(t, nil)
	fallback := &store{status: daemon.UpdateStatus{State: daemon.UpdateRolledBack}}
	stdout, _, err := run(t, daemoncmd.NewUpdateStatus(at(srv.URL), fallback.report), false)
	assertReported(t, err)
	if !strings.Contains(stdout, "rolled_back") {
		t.Errorf("stdout does not show rolled_back:\n%s", stdout)
	}
}

func TestUpdateStatusHealthyUpdateReturnsNil(t *testing.T) {
	for _, state := range []daemon.UpdateState{daemon.UpdateIdle, daemon.UpdatePending, daemon.UpdateConfirmed} {
		for _, asJSON := range []bool{false, true} {
			update := daemon.UpdateStatus{State: state}
			srv := serveUpdate(t, &update)
			stdout, _, err := run(t, daemoncmd.NewUpdateStatus(at(srv.URL), nil), asJSON)
			if err != nil {
				t.Errorf("%s json=%v: Run = %v, want nil", state, asJSON, err)
			}
			if !strings.Contains(stdout, string(state)) {
				t.Errorf("%s json=%v: stdout does not show the state:\n%s", state, asJSON, stdout)
			}
		}
	}
}

func TestUpdateStatusTextShowsEverySetField(t *testing.T) {
	update := fullUpdate(daemon.UpdatePending)
	srv := serveUpdate(t, &update)
	stdout, _, err := run(t, daemoncmd.NewUpdateStatus(at(srv.URL), nil), false)
	assertReported(t, err)
	want := "" +
		"FIELD          VALUE\n" +
		"source         daemon\n" +
		"status         pending\n" +
		"from version   v1.4.0\n" +
		"to version     v1.5.0\n" +
		"boot attempts  2\n" +
		"reason         awaiting confirmation\n" +
		"started at     2026-11-04T03:20:00Z\n" +
		"updated at     2026-11-04T03:21:30Z\n" +
		"channel        stable\n" +
		"watermark      v1.5.0\n" +
		"error          first boot exited 1\n"
	if stdout != want {
		t.Errorf("stdout:\n%s\nwant:\n%s", stdout, want)
	}
}

func TestUpdateStatusJSONKeys(t *testing.T) {
	closed := httptest.NewServer(http.NotFoundHandler())
	base := closed.URL
	closed.Close()
	fallback := &store{status: fullUpdate(daemon.UpdateRolledBack)}

	stdout, _, _ := run(t, daemoncmd.NewUpdateStatus(at(base), fallback.report), true)
	got := decodeReport(t, stdout)
	if _, ok := got["daemon_error"]; !ok {
		t.Fatalf("report has no daemon_error, so the keys below would not pin it:\n%s", stdout)
	}
	speccheck.AssertJSONKeys(t, json.RawMessage(stdout), "testdata/update-status.keys")
}

func TestUpdateStatusWriteErrorIsReturned(t *testing.T) {
	update := daemon.UpdateStatus{State: daemon.UpdateConfirmed}
	srv := serveUpdate(t, &update)
	for _, asJSON := range []bool{false, true} {
		errWrite := errors.New("broken pipe")
		err := runTo(t, daemoncmd.NewUpdateStatus(at(srv.URL), nil), asJSON, &limitWriter{err: errWrite})
		if !errors.Is(err, errWrite) {
			t.Errorf("json=%v: err = %v, want it to wrap the write error", asJSON, err)
		}
	}
}

func TestUpdateStatusNoneWriteErrorIsReturned(t *testing.T) {
	srv := serveUpdate(t, nil)
	errWrite := errors.New("broken pipe")
	err := runTo(t, daemoncmd.NewUpdateStatus(at(srv.URL), nil), false, &limitWriter{err: errWrite})
	if !errors.Is(err, errWrite) {
		t.Errorf("err = %v, want it to wrap the write error", err)
	}
}

func TestUpdateStatusUsesTheGivenClient(t *testing.T) {
	update := daemon.UpdateStatus{State: daemon.UpdateConfirmed}
	srv := serveUpdate(t, &update)
	var calls int
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		return http.DefaultTransport.RoundTrip(r)
	})}
	cmd := daemoncmd.NewUpdateStatus(at(srv.URL), nil, nil, daemoncmd.WithHTTPClient(client))
	if _, _, err := run(t, cmd, false); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if calls != 1 {
		t.Errorf("client made %d requests, want 1", calls)
	}
}

func TestUpdateStatusQuietDropsTheNoteButKeepsTheReport(t *testing.T) {
	for _, asJSON := range []bool{false, true} {
		fallback := &store{status: daemon.UpdateStatus{State: daemon.UpdateConfirmed}}
		cmd := daemoncmd.NewUpdateStatus(func(context.Context, *spec.Invocation) (string, error) {
			return "", errors.New("no listen address configured")
		}, fallback.report)
		stdout, stderr, err := runOutput(t, cmd, spec.OutputOptions{Quiet: true, JSON: asJSON}, nil)
		if err != nil {
			t.Fatalf("json=%v: Run: %v", asJSON, err)
		}
		if stderr != "" {
			t.Errorf("json=%v: stderr = %q, want empty under quiet output", asJSON, stderr)
		}
		if !strings.Contains(stdout, "confirmed") || !strings.Contains(stdout, "store") {
			t.Errorf("json=%v: stdout does not carry the report:\n%s", asJSON, stdout)
		}
	}
}
