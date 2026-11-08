package service

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
)

func TestExecRunnerRefusesAProgramOtherThanSystemctlAndID(t *testing.T) {
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

func TestExecRunnerReturnsStdout(t *testing.T) {
	out, err := execRunner(context.Background(), []string{"id", "-u"})
	if err != nil {
		t.Fatalf("execRunner: %v", err)
	}
	if got, want := strings.TrimSpace(string(out)), strconv.Itoa(os.Geteuid()); got != want {
		t.Fatalf("stdout = %q, want the effective user id %q", got, want)
	}
}

func TestExecRunnerCarriesStderrInTheError(t *testing.T) {
	const account = "no-such-account-vipengele-test"
	_, err := execRunner(context.Background(), []string{"id", "-u", "--", account})
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("err = %v, want it to wrap an *exec.ExitError", err)
	}
	msg := err.Error()
	stderr, ok := strings.CutPrefix(msg, "id -u -- "+account+": "+exitErr.Error()+": ")
	if !ok || !strings.Contains(stderr, account) {
		t.Fatalf("err = %q, want it to end with id's stderr naming the account", msg)
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
