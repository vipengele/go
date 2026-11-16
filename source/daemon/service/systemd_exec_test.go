package service

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"
)

func TestExecRunnerRunsOnlySystemctlLaunchctlAndID(t *testing.T) {
	out, err := execRunner(context.Background(), []string{"sh", "-c", "exit 0"})
	if !errors.Is(err, exec.ErrNotFound) {
		t.Fatalf("err = %v, want exec.ErrNotFound", err)
	}
	if !strings.Contains(err.Error(), "sh") {
		t.Fatalf("err = %q, want it to name the refused program", err)
	}
	if out != nil {
		t.Fatalf("stdout = %q, want none", out)
	}
}

// On a host without launchctl the program lookup fails with an *exec.Error;
// the allowlist's refusal of a program carries none, and a host with launchctl
// runs it.
func TestExecRunnerAcceptsLaunchctl(t *testing.T) {
	_, err := execRunner(context.Background(), []string{"launchctl", "version"})
	var lookupErr *exec.Error
	if errors.Is(err, exec.ErrNotFound) && !errors.As(err, &lookupErr) {
		t.Fatalf("err = %v, want launchctl let through the allowlist", err)
	}
	_, refused := execRunner(context.Background(), []string{"sh", "-c", "exit 0"})
	if errors.As(refused, &lookupErr) {
		t.Fatalf("refusal err = %v, want no *exec.Error, which would hide a refused launchctl", refused)
	}
}

// A cancelled context fails each program before it writes anything, whether
// or not it is installed, so the error carries no stderr.
func TestExecRunnerErrorWithoutStderrNamesTheCommand(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, argv := range [][]string{{"systemctl", "--version"}, {"id", "-u"}} {
		t.Run(argv[0], func(t *testing.T) {
			_, err := execRunner(ctx, argv)
			if !errors.Is(err, context.Canceled) && !errors.Is(err, exec.ErrNotFound) {
				t.Fatalf("err = %v, want context.Canceled or exec.ErrNotFound", err)
			}
			want := strings.Join(argv, " ") + ": " + errors.Unwrap(err).Error()
			if err.Error() != want {
				t.Fatalf("err = %q, want %q, the command and its error alone", err, want)
			}
		})
	}
}
