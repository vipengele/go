// The tag matches lock_unix.go: a holder in another process, and the release of its lock when that
// process is killed, are flock's behaviour on the systems it lists.

//go:build linux || darwin || freebsd || netbsd || openbsd || dragonfly

package filelock_test

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"testing"
	"time"

	"go.vipengele.org/filelock"
)

const (
	// childModeEnv set to childModeHold makes the test binary a lock holder instead of a test run.
	childModeEnv  = "FILELOCK_TEST_CHILD"
	childModeHold = "hold"

	// childPathEnv names the lock file the holder locks.
	childPathEnv = "FILELOCK_TEST_PATH"

	// childReady is the line the holder writes to stdout once it holds the lock.
	childReady = "locked\n"

	// childLimit bounds every wait on a child, so a hung child fails the test instead of hanging
	// it.
	childLimit = 10 * time.Second
)

// TestMain runs the binary as a lock holder when childModeEnv says so, and never runs the tests in
// that mode.
func TestMain(m *testing.M) {
	if mode, ok := os.LookupEnv(childModeEnv); ok {
		os.Exit(runChild(mode))
	}

	os.Exit(m.Run())
}

// runChild takes the lock on childPathEnv, writes childReady and holds the lock until stdin
// reaches end of file. It returns the exit code.
func runChild(mode string) int {
	if mode != childModeHold {
		fmt.Fprintf(os.Stderr, "unknown %s %q\n", childModeEnv, mode)

		return 2
	}

	unlock, err := filelock.New(os.Getenv(childPathEnv)).Lock(context.Background())
	if err != nil {
		fmt.Fprintln(os.Stderr, err)

		return 1
	}

	defer unlock()

	if _, err := io.WriteString(os.Stdout, childReady); err != nil {
		fmt.Fprintln(os.Stderr, err)

		return 1
	}

	if _, err := io.Copy(io.Discard, os.Stdin); err != nil {
		fmt.Fprintln(os.Stderr, err)

		return 1
	}

	return 0
}

// holder is a child process holding the lock. One goroutine calls cmd.Wait and closes exited
// once err holds its result, so the test and its cleanup both wait on exited and never call Wait
// concurrently.
type holder struct {
	cmd    *exec.Cmd
	stdin  *os.File
	exited chan struct{}
	err    error
}

// startHolder starts a child that locks path and returns once the child holds the lock. Cleanup
// kills and reaps the child whatever the test's outcome.
func startHolder(t *testing.T, path string) *holder {
	t.Helper()

	stdinR, stdinW, err := os.Pipe()
	if err != nil {
		t.Fatalf("Pipe: %v", err)
	}

	stdoutR, stdoutW, err := os.Pipe()
	if err != nil {
		t.Fatalf("Pipe: %v", err)
	}

	t.Cleanup(func() {
		_ = stdinW.Close()
		_ = stdoutR.Close()
	})

	cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^$")
	cmd.Env = append(os.Environ(), childModeEnv+"="+childModeHold, childPathEnv+"="+path)
	cmd.Stdin = stdinR
	cmd.Stdout = stdoutW
	cmd.Stderr = os.Stderr

	err = cmd.Start()

	_ = stdinR.Close()
	_ = stdoutW.Close()

	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	h := &holder{cmd: cmd, stdin: stdinW, exited: make(chan struct{})}

	go func() {
		h.err = cmd.Wait()
		close(h.exited)
	}()

	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		<-h.exited
	})

	ready := make(chan error, 1)

	go func() {
		line, err := bufio.NewReader(stdoutR).ReadString('\n')
		if err == nil && line != childReady {
			err = fmt.Errorf("child wrote %q, want %q", line, childReady)
		}

		ready <- err
	}()

	select {
	case err := <-ready:
		if err != nil {
			t.Fatalf("waiting for the child to lock: %v", err)
		}
	case <-time.After(childLimit):
		t.Fatalf("child did not lock within %v", childLimit)
	}

	return h
}

// wait waits up to childLimit for the child to exit and returns its exit error.
func (h *holder) wait(t *testing.T) error {
	t.Helper()

	select {
	case <-h.exited:
		return h.err
	case <-time.After(childLimit):
		t.Fatalf("child did not exit within %v", childLimit)

		return nil
	}
}

// lockWithin takes l within childLimit and fails the test when it cannot.
func lockWithin(t *testing.T, l filelock.Locker) {
	t.Helper()

	ctx, cancel := context.WithTimeout(t.Context(), childLimit)
	defer cancel()

	lockOrFatal(t, ctx, l)()
}

func TestProcessHolderExcludesParentUntilItExits(t *testing.T) {
	t.Parallel()

	path := lockPath(t)
	h := startHolder(t, path)
	l := filelock.New(path)

	assertHeld(t, l)

	if err := h.stdin.Close(); err != nil {
		t.Fatalf("closing the child's stdin: %v", err)
	}

	if err := h.wait(t); err != nil {
		t.Fatalf("child exit: %v", err)
	}

	lockWithin(t, l)
}

func TestProcessKilledHolderReleasesLock(t *testing.T) {
	t.Parallel()

	path := lockPath(t)
	h := startHolder(t, path)
	l := filelock.New(path)

	assertHeld(t, l)

	if err := h.cmd.Process.Kill(); err != nil {
		t.Fatalf("Kill: %v", err)
	}

	var exitErr *exec.ExitError
	if err := h.wait(t); !errors.As(err, &exitErr) {
		t.Fatalf("killed child exit = %v, want an *exec.ExitError", err)
	}

	lockWithin(t, l)

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("lock file after the holder was killed: %v", err)
	}
}
