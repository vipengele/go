package update

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"go.vipengele.org/cli/spec"
	"go.vipengele.org/update/updatetest"
)

// hookRun is the Invocation and buffers one run of the hooks writes to.
type hookRun struct {
	inv    *spec.Invocation
	stdout *bytes.Buffer
	stderr *bytes.Buffer
}

// newHookRun returns a run whose stderr is a terminal exactly when terminal is true.
func newHookRun(terminal bool) hookRun {
	r := hookRun{stdout: &bytes.Buffer{}, stderr: &bytes.Buffer{}}
	r.inv = spec.NewInvocation(&spec.Env{
		Stdout:   r.stdout,
		Stderr:   r.stderr,
		Terminal: func(spec.Stream) bool { return terminal },
	}, nil, nil)
	return r
}

// hookConfig is bgConfig with IsTerminal left nil, so the hooks answer it from the Env.
func hookConfig(t *testing.T, srv *updatetest.Server) Config {
	t.Helper()
	c := bgConfig(t, srv, &fakeStore{})
	c.IsTerminal = nil
	return c
}

func mustHooks(t *testing.T, c Config) (
	func(context.Context, *spec.Invocation, spec.CommandInfo) (context.Context, error),
	func(context.Context, *spec.Invocation, spec.CommandInfo, error) error,
) {
	t.Helper()
	pre, post, err := c.UpdateCheckHooks()
	if err != nil {
		t.Fatalf("UpdateCheckHooks() error = %v", err)
	}
	return pre, post
}

// runHooks calls pre, then post with runErr, and returns post's error.
func runHooks(t *testing.T, c Config, r hookRun, ctx context.Context, cmd spec.CommandInfo, runErr error) error {
	t.Helper()
	pre, post := mustHooks(t, c)
	ctx, err := pre(ctx, r.inv, cmd)
	if err != nil {
		t.Fatalf("pre() error = %v", err)
	}
	return post(ctx, r.inv, cmd, runErr)
}

func plainCommand() spec.CommandInfo { return spec.NewCommandInfo([]string{"atk", "run"}, nil) }

const wantLine = "atk v1.2.3 is available (you're on v1.0.0). Run 'atk update' to install.\n"

func TestNoticeString(t *testing.T) {
	t.Parallel()
	got := Notice{Name: "atk", Current: "v1.0.0", Latest: "v1.2.3"}.String()
	if want := strings.TrimSuffix(wantLine, "\n"); got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
}

func TestHooksSkipAnnotationExempts(t *testing.T) {
	t.Parallel()
	srv := newKitServer(t)
	r := newHookRun(true)
	cmd := spec.NewCommandInfo([]string{"atk", "version"}, map[string]string{spec.SkipUpdateCheck: "true"})
	if err := runHooks(t, hookConfig(t, srv), r, context.Background(), cmd, nil); err != nil {
		t.Fatalf("post() = %v, want nil", err)
	}
	if n := latestRequests(srv); n != 0 {
		t.Errorf("latest requests = %d, want 0", n)
	}
	if r.stderr.Len() != 0 || r.stdout.Len() != 0 {
		t.Errorf("output stderr=%q stdout=%q, want none", r.stderr, r.stdout)
	}
}

func TestHooksSilentWithoutATerminal(t *testing.T) {
	t.Parallel()
	srv := newKitServer(t)
	r := newHookRun(false)
	if err := runHooks(t, hookConfig(t, srv), r, context.Background(), plainCommand(), nil); err != nil {
		t.Fatalf("post() = %v, want nil", err)
	}
	if r.stderr.Len() != 0 || r.stdout.Len() != 0 {
		t.Errorf("output stderr=%q stdout=%q, want none", r.stderr, r.stdout)
	}
}

func TestHooksAnnounceOnceOnStderr(t *testing.T) {
	t.Parallel()
	srv := newKitServer(t)
	r := newHookRun(true)
	if err := runHooks(t, hookConfig(t, srv), r, context.Background(), plainCommand(), nil); err != nil {
		t.Fatalf("post() = %v, want nil", err)
	}
	if got := r.stderr.String(); got != wantLine {
		t.Errorf("stderr = %q, want %q", got, wantLine)
	}
	if r.stdout.Len() != 0 {
		t.Errorf("stdout = %q, want none", r.stdout)
	}
}

func TestHooksAnnounceAfterAFailingRun(t *testing.T) {
	t.Parallel()
	srv := newKitServer(t)
	r := newHookRun(true)
	runErr := errors.New("boom")
	if err := runHooks(t, hookConfig(t, srv), r, context.Background(), plainCommand(), runErr); err != nil {
		t.Fatalf("post() = %v, want nil", err)
	}
	if got := r.stderr.String(); got != wantLine {
		t.Errorf("stderr = %q, want %q", got, wantLine)
	}
}

func TestHooksCancelledContextWritesNothing(t *testing.T) {
	t.Parallel()
	gate := make(chan struct{})
	srv := newKitServer(t, updatetest.Slow(updatetest.TargetLatest, gate))
	r := newHookRun(true)
	c := hookConfig(t, srv)
	c.CheckWait = 2 * time.Second
	pre, post := mustHooks(t, c)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ctx, err := pre(ctx, r.inv, plainCommand())
	if err != nil {
		t.Fatalf("pre() error = %v", err)
	}
	select {
	case <-srv.Blocked(updatetest.TargetLatest):
	case <-time.After(10 * time.Second):
		t.Fatal("the check never reached the server")
	}
	cancel()

	begin := time.Now()
	err = post(ctx, r.inv, plainCommand(), nil)
	if elapsed := time.Since(begin); elapsed >= time.Second {
		t.Errorf("post() took %v, want under 1s", elapsed)
	}
	if err != nil {
		t.Errorf("post() = %v, want nil", err)
	}
	if r.stderr.Len() != 0 {
		t.Errorf("stderr = %q, want none", r.stderr)
	}
}

func TestHooksSlowServerReturnsWithinTheBound(t *testing.T) {
	t.Parallel()
	gate := make(chan struct{})
	srv := newKitServer(t, updatetest.Slow(updatetest.TargetLatest, gate))
	r := newHookRun(true)
	c := hookConfig(t, srv)
	c.CheckWait = 50 * time.Millisecond

	begin := time.Now()
	err := runHooks(t, c, r, context.Background(), plainCommand(), nil)
	if elapsed := time.Since(begin); elapsed >= time.Second {
		t.Errorf("hooks took %v, want under 1s", elapsed)
	}
	if err != nil {
		t.Errorf("post() = %v, want nil", err)
	}
	if r.stderr.Len() != 0 {
		t.Errorf("stderr = %q, want none", r.stderr)
	}
}

func TestUpdateCheckHooksReturnsAnInvalidConfigsError(t *testing.T) {
	t.Parallel()
	c := validConfig(t.TempDir())
	c.CheckWait = -time.Second
	pre, post, err := c.UpdateCheckHooks()
	if err == nil {
		t.Fatal("UpdateCheckHooks() error = nil, want one")
	}
	if pre != nil || post != nil {
		t.Error("UpdateCheckHooks() returned hooks with an error")
	}
}
