package update

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// preflightTag is the tag of the Selection the preflight tests check a staged binary against.
const preflightTag = "v1.2.3"

// preflightConfig is a valid Config for the platform linux/amd64.
func preflightConfig(t *testing.T) Config {
	t.Helper()

	c := validConfig(t.TempDir())
	c.GOOS = "linux"
	c.GOARCH = "amd64"

	return c
}

// newStagedFile returns a Staged over a new file in a fresh temp dir, its write descriptor open as
// Extract leaves it, and discards it when the test ends.
func newStagedFile(t *testing.T) *Staged {
	t.Helper()

	f, err := os.CreateTemp(t.TempDir(), "atk-update-*")
	if err != nil {
		t.Fatalf("CreateTemp: %v", err)
	}

	s := &Staged{f: f, remove: os.Remove}
	t.Cleanup(func() {
		if err := s.Discard(); err != nil {
			t.Errorf("Discard: %v", err)
		}
	})

	return s
}

// versionJSON is a schema-1 "version --json" answer for version, goos and goarch.
func versionJSON(version, goos, goarch string) string {
	return `{
  "schema": 1,
  "version": "` + version + `",
  "commit": "0123abcdef0123abcdef0123abcdef0123abcdef",
  "date": "2026-01-02T03:04:05Z",
  "go": "go1.27.2",
  "os": "` + goos + `",
  "arch": "` + goarch + `",
  "dev": false
}
`
}

// answering returns fsSteps whose run writes stdout and stderr and returns err, recording the
// name and args it was called with in calls.
func answering(stdout, stderr string, err error, calls *[][]string) fsSteps {
	s := defaultSteps()
	s.run = func(_ context.Context, name string, args []string, out, errOut io.Writer) error {
		if calls != nil {
			*calls = append(*calls, append([]string{name}, args...))
		}
		_, _ = io.WriteString(out, stdout)
		_, _ = io.WriteString(errOut, stderr)
		return err
	}

	return s
}

func TestCheckStagedAcceptsTheSelectedRelease(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		stdout string
		want   StagedBinary
	}{
		{
			name:   "schema 1",
			stdout: versionJSON("v1.2.3", "linux", "amd64"),
			want:   StagedBinary{Version: "v1.2.3", Schema: 1, OS: "linux", Arch: "amd64"},
		},
		{
			name:   "version without the leading v",
			stdout: versionJSON("1.2.3", "linux", "amd64"),
			want:   StagedBinary{Version: "1.2.3", Schema: 1, OS: "linux", Arch: "amd64"},
		},
		{
			name:   "schema 2 with an unknown key",
			stdout: `{"schema": 2, "version": "v1.2.3", "os": "linux", "arch": "amd64", "extra": [1, 2]}`,
			want:   StagedBinary{Version: "v1.2.3", Schema: 2, OS: "linux", Arch: "amd64"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			s := newStagedFile(t)
			var calls [][]string

			got, err := preflightConfig(t).checkStaged(context.Background(), s, Selection{Tag: preflightTag}, answering(tc.stdout, "", nil, &calls))
			if err != nil {
				t.Fatalf("checkStaged: %v", err)
			}

			tc.want.Path = s.Name()
			if got != tc.want {
				t.Errorf("StagedBinary = %+v, want %+v", got, tc.want)
			}

			wantCall := []string{s.Name(), "version", "--json"}
			if len(calls) != 1 || !reflect.DeepEqual(calls[0], wantCall) {
				t.Errorf("run calls = %q, want one %q", calls, wantCall)
			}

			if !s.closed {
				t.Error("staged file is open, want it closed before the run")
			}

			if s.settled {
				t.Error("staged file is settled, want it left to the caller")
			}
		})
	}
}

func TestCheckStagedRefusesAWrongAnswer(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		stdout     string
		wantReason string
		wantCause  bool
	}{
		{name: "version mismatch", stdout: versionJSON("v1.2.2", "linux", "amd64"), wantReason: `"v1.2.2"`},
		{name: "version differs only in suffix", stdout: versionJSON("v1.2.3-rc.1", "linux", "amd64"), wantReason: `"v1.2.3-rc.1"`},
		{name: "os mismatch", stdout: versionJSON("v1.2.3", "darwin", "amd64"), wantReason: `os "darwin"`},
		{name: "arch mismatch", stdout: versionJSON("v1.2.3", "linux", "arm64"), wantReason: `arch "arm64"`},
		{name: "schema missing", stdout: `{"version": "v1.2.3", "os": "linux", "arch": "amd64"}`, wantReason: "schema 0"},
		{name: "schema 0", stdout: `{"schema": 0, "version": "v1.2.3", "os": "linux", "arch": "amd64"}`, wantReason: "schema 0"},
		{name: "version empty", stdout: `{"schema": 1, "version": "", "os": "linux", "arch": "amd64"}`, wantReason: "no version"},
		{name: "version missing", stdout: `{"schema": 1, "os": "linux", "arch": "amd64"}`, wantReason: "no version", wantCause: true},
		{name: "version null", stdout: `{"schema": 1, "version": null, "os": "linux", "arch": "amd64"}`, wantReason: "no version"},
		{name: "version not a string", stdout: `{"schema": 1, "version": 123, "os": "linux", "arch": "amd64"}`, wantReason: "no version", wantCause: true},
		{name: "not JSON", stdout: "v1.2.3\n", wantReason: "no JSON", wantCause: true},
		{name: "no output", stdout: "", wantReason: "no JSON", wantCause: true},
		{name: "trailing text", stdout: versionJSON("v1.2.3", "linux", "amd64") + "more", wantReason: "no JSON", wantCause: true},
		{name: "over 64 KiB", stdout: versionJSON("v1.2.3", "linux", "amd64") + strings.Repeat(" ", 64<<10), wantReason: "more than 65536 bytes"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			s := newStagedFile(t)

			got, err := preflightConfig(t).checkStaged(context.Background(), s, Selection{Tag: preflightTag}, answering(tc.stdout, "", nil, nil))

			var pe *PreflightError
			if !errors.As(err, &pe) {
				t.Fatalf("checkStaged error = %v, want a *PreflightError", err)
			}

			if pe.Path != s.Name() {
				t.Errorf("Path = %q, want %q", pe.Path, s.Name())
			}

			if !strings.Contains(pe.Reason, tc.wantReason) {
				t.Errorf("Reason = %q, want it to contain %q", pe.Reason, tc.wantReason)
			}

			if (pe.Err != nil) != tc.wantCause {
				t.Errorf("Err = %v, want a cause: %v", pe.Err, tc.wantCause)
			}

			if got != (StagedBinary{}) {
				t.Errorf("StagedBinary = %+v, want the zero value", got)
			}
		})
	}
}

func TestCheckStagedAcceptsOutputOfExactly64KiB(t *testing.T) {
	t.Parallel()

	body := versionJSON("v1.2.3", "linux", "amd64")
	stdout := body + strings.Repeat(" ", 64<<10-len(body))

	if _, err := preflightConfig(t).checkStaged(context.Background(), newStagedFile(t), Selection{Tag: preflightTag}, answering(stdout, "", nil, nil)); err != nil {
		t.Errorf("checkStaged: %v, want 64 KiB of output accepted", err)
	}
}

func TestCheckStagedRefusesAFailedRunWithItsStderr(t *testing.T) {
	t.Parallel()

	runErr := errors.New("exit status 3")
	stderr := strings.Repeat("a", maxErrorBody-1) + "é" + strings.Repeat("b", 4000)

	s := newStagedFile(t)

	_, err := preflightConfig(t).checkStaged(context.Background(), s, Selection{Tag: preflightTag}, answering(versionJSON("v1.2.3", "linux", "amd64"), stderr, runErr, nil))

	var pe *PreflightError
	if !errors.As(err, &pe) {
		t.Fatalf("checkStaged error = %v, want a *PreflightError", err)
	}

	if !errors.Is(err, runErr) {
		t.Errorf("checkStaged error = %v, want one wrapping the run's error", err)
	}

	if want := strings.Repeat("a", maxErrorBody-1); pe.Stderr != want {
		t.Errorf("Stderr is %d bytes, want the %d before the cut character", len(pe.Stderr), len(want))
	}
}

func TestCheckStagedTimeoutUnwrapsToDeadlineExceeded(t *testing.T) {
	t.Parallel()

	c := preflightConfig(t)
	c.PreflightTimeout = time.Millisecond

	steps := defaultSteps()
	steps.run = func(ctx context.Context, _ string, _ []string, _, _ io.Writer) error {
		<-ctx.Done()
		return errors.New("signal: killed")
	}

	_, err := c.checkStaged(context.Background(), newStagedFile(t), Selection{Tag: preflightTag}, steps)

	var pe *PreflightError
	if !errors.As(err, &pe) {
		t.Fatalf("checkStaged error = %v, want a *PreflightError", err)
	}

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("checkStaged error = %v, want one wrapping context.DeadlineExceeded", err)
	}
}

func TestCheckStagedPassesTheParentContextErrorThrough(t *testing.T) {
	t.Parallel()

	t.Run("cancelled during the run", func(t *testing.T) {
		t.Parallel()

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		steps := defaultSteps()
		steps.run = func(runCtx context.Context, _ string, _ []string, _, _ io.Writer) error {
			cancel()
			<-runCtx.Done()
			return errors.New("signal: killed")
		}

		_, err := preflightConfig(t).checkStaged(ctx, newStagedFile(t), Selection{Tag: preflightTag}, steps)
		assertParentCancel(t, err)
	})

	t.Run("cancelled before", func(t *testing.T) {
		t.Parallel()

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		var calls [][]string
		_, err := preflightConfig(t).checkStaged(ctx, newStagedFile(t), Selection{Tag: preflightTag}, answering(versionJSON("v1.2.3", "linux", "amd64"), "", nil, &calls))
		assertParentCancel(t, err)

		if len(calls) != 0 {
			t.Errorf("run calls = %q, want none", calls)
		}
	})

	t.Run("cancelled by a hook", func(t *testing.T) {
		t.Parallel()

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		c := preflightConfig(t)
		c.Preflight = []Preflight{func(ctx context.Context, _ StagedBinary) error {
			cancel()
			return ctx.Err()
		}}

		_, err := c.checkStaged(ctx, newStagedFile(t), Selection{Tag: preflightTag}, answering(versionJSON("v1.2.3", "linux", "amd64"), "", nil, nil))
		assertParentCancel(t, err)
	})
}

// assertParentCancel fails the test unless err is the parent context's cancellation, unwrapped.
func assertParentCancel(t *testing.T, err error) {
	t.Helper()

	if !errors.Is(err, context.Canceled) {
		t.Errorf("checkStaged error = %v, want context.Canceled", err)
	}

	var pe *PreflightError
	if errors.As(err, &pe) {
		t.Errorf("checkStaged error = %v, want no *PreflightError", err)
	}
}

func TestCheckStagedRunsHooksInOrder(t *testing.T) {
	t.Parallel()

	veto := errors.New("database schema too new")

	tests := []struct {
		name      string
		stdout    string
		failAt    int // index of the hook that vetoes; -1 for none
		wantCalls []int
	}{
		{name: "all pass", stdout: versionJSON("v1.2.3", "linux", "amd64"), failAt: -1, wantCalls: []int{0, 1, 2}},
		{name: "first veto stops the rest", stdout: versionJSON("v1.2.3", "linux", "amd64"), failAt: 1, wantCalls: []int{0, 1}},
		{name: "skipped when the built-in check fails", stdout: versionJSON("v9.9.9", "linux", "amd64"), failAt: -1, wantCalls: nil},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			s := newStagedFile(t)

			var calls []int
			hook := func(i int) Preflight {
				return func(_ context.Context, b StagedBinary) error {
					calls = append(calls, i)
					want := StagedBinary{Path: s.Name(), Version: "v1.2.3", Schema: 1, OS: "linux", Arch: "amd64"}
					if b != want {
						t.Errorf("hook %d got %+v, want %+v", i, b, want)
					}
					if i == tc.failAt {
						return veto
					}
					return nil
				}
			}

			c := preflightConfig(t)
			c.Preflight = []Preflight{hook(0), nil, hook(1), hook(2)}

			_, err := c.checkStaged(context.Background(), s, Selection{Tag: preflightTag}, answering(tc.stdout, "", nil, nil))

			if !reflect.DeepEqual(calls, tc.wantCalls) {
				t.Errorf("hooks called = %v, want %v", calls, tc.wantCalls)
			}

			switch {
			case tc.failAt >= 0:
				var pe *PreflightError
				if !errors.As(err, &pe) || !errors.Is(err, veto) {
					t.Fatalf("checkStaged error = %v, want a *PreflightError wrapping the hook's error", err)
				}
				if pe.Path != s.Name() || !strings.Contains(pe.Reason, "Preflight[2]") {
					t.Errorf("PreflightError = %+v, want Path %q and a Reason naming Preflight[2]", pe, s.Name())
				}
			case tc.wantCalls == nil:
				var pe *PreflightError
				if !errors.As(err, &pe) {
					t.Errorf("checkStaged error = %v, want a *PreflightError", err)
				}
			default:
				if err != nil {
					t.Errorf("checkStaged error = %v, want nil", err)
				}
			}
		})
	}
}

func TestCheckStagedRefusesBadInput(t *testing.T) {
	t.Parallel()

	t.Run("nil staged", func(t *testing.T) {
		t.Parallel()

		if _, err := preflightConfig(t).CheckStaged(context.Background(), nil, Selection{Tag: preflightTag}); !errors.Is(err, errNoStaged) {
			t.Errorf("CheckStaged error = %v, want %v", err, errNoStaged)
		}
	})

	t.Run("invalid config", func(t *testing.T) {
		t.Parallel()

		c := preflightConfig(t)
		c.PreflightTimeout = -time.Second

		var calls [][]string
		s := newStagedFile(t)
		if _, err := c.checkStaged(context.Background(), s, Selection{Tag: preflightTag}, answering("", "", nil, &calls)); err == nil {
			t.Error("checkStaged error = nil, want the invalid Config rejected")
		}

		if len(calls) != 0 || s.closed {
			t.Errorf("run calls = %q, closed = %v, want nothing done", calls, s.closed)
		}
	})
}

func TestCheckStagedRunsAnAbsolutePath(t *testing.T) {
	t.Chdir(t.TempDir())

	f, err := os.CreateTemp(".", "atk-update-*")
	if err != nil {
		t.Fatalf("CreateTemp: %v", err)
	}

	s := &Staged{f: f, remove: os.Remove}
	t.Cleanup(func() { _ = s.Discard() })

	var calls [][]string
	if _, err := preflightConfig(t).checkStaged(context.Background(), s, Selection{Tag: preflightTag}, answering(versionJSON("v1.2.3", "linux", "amd64"), "", nil, &calls)); err != nil {
		t.Fatalf("checkStaged: %v", err)
	}

	if len(calls) != 1 || !filepath.IsAbs(calls[0][0]) {
		t.Errorf("run calls = %q, want one with an absolute path", calls)
	}
}
