package update

import (
	"bufio"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// runningLimit bounds every wait on the running executable, so a helper that never starts or
// never exits fails the test instead of hanging it.
const runningLimit = 30 * time.Second

// runningExecutable is a process started from an executable file, which Windows keeps from being
// removed or overwritten while the process runs. One goroutine calls Wait and closes exited once
// err holds its result, so the test and its cleanup both wait on exited and never call Wait
// concurrently.
type runningExecutable struct {
	stdin  *os.File
	exited chan struct{}
	err    error
}

// buildWaitForStdin builds testdata/waitforstdin to path.
func buildWaitForStdin(t *testing.T, path string) {
	t.Helper()

	build := exec.CommandContext(t.Context(), "go", "build", "-o", path, "./testdata/waitforstdin")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("building the helper: %v\n%s", err, out)
	}
}

// startRunning starts the executable at path, built from testdata/waitforstdin, and returns once
// it has written its first line. Cleanup ends the process and waits for it, and is registered
// after every temporary directory the caller created before, so it runs before they are removed.
func startRunning(t *testing.T, path string) *runningExecutable {
	t.Helper()

	stdinR, stdinW, err := os.Pipe()
	if err != nil {
		t.Fatalf("Pipe: %v", err)
	}

	stdoutR, stdoutW, err := os.Pipe()
	if err != nil {
		t.Fatalf("Pipe: %v", err)
	}

	cmd := exec.Command(path)
	cmd.Stdin = stdinR
	cmd.Stdout = stdoutW
	cmd.Stderr = os.Stderr

	err = cmd.Start()

	_ = stdinR.Close()
	_ = stdoutW.Close()

	if err != nil {
		_ = stdinW.Close()
		_ = stdoutR.Close()

		t.Fatalf("Start: %v", err)
	}

	r := &runningExecutable{stdin: stdinW, exited: make(chan struct{})}

	go func() {
		r.err = cmd.Wait()
		close(r.exited)
	}()

	t.Cleanup(func() {
		_ = stdinW.Close()
		_ = cmd.Process.Kill()
		<-r.exited
		_ = stdoutR.Close()
	})

	ready := make(chan error, 1)

	go func() {
		line, err := bufio.NewReader(stdoutR).ReadString('\n')
		if err == nil && line != "running\n" {
			err = errors.New("the helper wrote " + line)
		}

		ready <- err
	}()

	select {
	case err := <-ready:
		if err != nil {
			t.Fatalf("waiting for the helper to start: %v", err)
		}
	case <-time.After(runningLimit):
		t.Fatalf("the helper did not start within %v", runningLimit)
	}

	return r
}

// stop closes the process's stdin, which ends it, and fails t unless it exits cleanly within
// runningLimit.
func (r *runningExecutable) stop(t *testing.T) {
	t.Helper()

	if err := r.stdin.Close(); err != nil {
		t.Fatalf("closing the helper's stdin: %v", err)
	}

	select {
	case <-r.exited:
		if r.err != nil {
			t.Fatalf("helper exit: %v", r.err)
		}
	case <-time.After(runningLimit):
		t.Fatalf("the helper did not exit within %v", runningLimit)
	}
}

// TestInstallReplacesARunningExecutable installs over the file of a running process, in a
// directory whose path holds a space. Windows lets the running file be renamed to the backup but
// not removed, so Discard reports the backup locked while the process runs, and the next Install,
// once it has exited, replaces that backup with one Discard removes.
func TestInstallReplacesARunningExecutable(t *testing.T) {
	t.Parallel()

	dir := filepath.Join(resolvedDir(t, t.TempDir()), "Program Files")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}

	exe := filepath.Join(dir, "atk.exe")
	buildWaitForStdin(t, exe)

	original := readFile(t, exe)
	config := executableConfig(t, exe)
	running := startRunning(t, exe)

	inst, err := config.Install(stageBinary(t, dir, "new"))
	if err != nil {
		t.Fatalf("Install over the running executable: %v", err)
	}

	if inst.Path() != exe {
		t.Errorf("Path = %q, want %q", inst.Path(), exe)
	}

	backup := exe + ".old"
	if inst.Backup() != backup {
		t.Errorf("Backup = %q, want %q", inst.Backup(), backup)
	}

	if got := readFile(t, exe); got != "new" {
		t.Errorf("executable holds %q, want %q", got, "new")
	}

	if got := readFile(t, backup); got != original {
		t.Errorf("backup holds %d bytes, want the running executable's %d", len(got), len(original))
	}

	var lerr *BackupLockedError
	if err := inst.Discard(); !errors.As(err, &lerr) {
		t.Fatalf("Discard while the executable runs = %v, want a *BackupLockedError", err)
	} else if lerr.Path != backup {
		t.Errorf("BackupLockedError Path = %q, want %q", lerr.Path, backup)
	}

	if got := readFile(t, backup); got != original {
		t.Errorf("backup after a locked Discard holds %d bytes, want the running executable's %d", len(got), len(original))
	}

	running.stop(t)

	next, err := config.Install(stageBinary(t, dir, "newer"))
	if err != nil {
		t.Fatalf("Install once the executable has exited: %v", err)
	}

	if got := readFile(t, next.Backup()); got != "new" {
		t.Errorf("backup holds %q, want %q", got, "new")
	}

	if err := next.Discard(); err != nil {
		t.Fatalf("Discard once the executable has exited: %v", err)
	}

	assertMissing(t, backup)

	if got := readFile(t, exe); got != "newer" {
		t.Errorf("executable holds %q, want %q", got, "newer")
	}
}
