// The tag matches filelock's unix implementation: a holder in another process, and the release of
// its lock when that process is killed, are flock's behaviour on the systems it lists.

//go:build linux || darwin || freebsd || netbsd || openbsd || dragonfly

package daemon_test

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"testing"
	"time"

	"go.vipengele.org/cli/spec/paths"
	"go.vipengele.org/daemon"
)

const (
	// childRuntimeEnv names the runtime directory the child acquires the instance in. Its presence
	// makes the test binary an instance holder instead of a test run.
	childRuntimeEnv = "DAEMON_TEST_CHILD_RUNTIME"

	// childReady is the line the child writes to stdout once it holds the instance.
	childReady = "acquired\n"

	// childLimit bounds every wait on a child, so a hung child fails the test instead of hanging it.
	childLimit = 10 * time.Second
)

// TestMain runs the binary as an instance holder when childRuntimeEnv is set, and never runs the
// tests in that mode.
func TestMain(m *testing.M) {
	if dir, ok := os.LookupEnv(childRuntimeEnv); ok {
		os.Exit(runChild(dir))
	}

	os.Exit(m.Run())
}

// runChild acquires the instance in dir, writes childReady and holds it until stdin reaches end of
// file. It returns the exit code.
func runChild(dir string) int {
	in, err := daemon.AcquireInstance(testIdentity(), paths.Options{RuntimeDir: dir})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)

		return 1
	}
	defer in.Release()

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

func TestProcessHolderReportsItsPIDUntilKilled(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	opts := paths.Options{RuntimeDir: dir}

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
	cmd.Env = append(os.Environ(), childRuntimeEnv+"="+dir)
	cmd.Stdin = stdinR
	cmd.Stdout = stdoutW
	cmd.Stderr = os.Stderr

	err = cmd.Start()

	_ = stdinR.Close()
	_ = stdoutW.Close()

	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	exited := make(chan struct{})

	go func() {
		_ = cmd.Wait()
		close(exited)
	}()

	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		<-exited
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
			t.Fatalf("waiting for the child to acquire: %v", err)
		}
	case <-time.After(childLimit):
		t.Fatalf("child did not acquire within %v", childLimit)
	}

	_, err = daemon.AcquireInstance(testIdentity(), opts)

	var are *daemon.AlreadyRunningError
	if !errors.As(err, &are) {
		t.Fatalf("AcquireInstance while the child holds it = %v, want an *AlreadyRunningError", err)
	}

	if are.PID != cmd.Process.Pid {
		t.Errorf("PID = %d, want the child's %d", are.PID, cmd.Process.Pid)
	}

	if err := cmd.Process.Kill(); err != nil {
		t.Fatalf("Kill: %v", err)
	}

	select {
	case <-exited:
	case <-time.After(childLimit):
		t.Fatalf("child did not exit within %v", childLimit)
	}

	in, err := daemon.AcquireInstance(testIdentity(), opts)
	if err != nil {
		t.Fatalf("AcquireInstance after the child was killed: %v", err)
	}

	in.Release()
}
