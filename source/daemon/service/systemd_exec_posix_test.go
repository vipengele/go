// The tests in this file run id, which a Windows host does not have.

//go:build !windows

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
