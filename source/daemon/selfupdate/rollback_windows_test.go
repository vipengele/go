package selfupdate

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// runningImageEnv makes the test binary stand in for a running executable instead of running its
// tests: it writes "running" and a newline to stdout, then runs until stdin reaches end of file,
// so the test that starts it decides when the process, and the lock Windows keeps on its file,
// ends.
const runningImageEnv = "SELFUPDATE_TEST_RUNNING_IMAGE"

// runningLimit bounds every wait on the running image, so a process that never starts or never
// exits fails the test instead of hanging it.
const runningLimit = 30 * time.Second

func TestMain(m *testing.M) {
	if os.Getenv(runningImageEnv) == "1" {
		os.Exit(runAsImage())
	}

	os.Exit(m.Run())
}

func runAsImage() int {
	if _, err := io.WriteString(os.Stdout, "running\n"); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	if _, err := io.Copy(io.Discard, os.Stdin); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	return 0
}

// copyTestBinary copies the running test binary to path.
func copyTestBinary(t *testing.T, path string) {
	t.Helper()

	self, err := os.Executable()
	if err != nil {
		t.Fatalf("Executable: %v", err)
	}

	data, err := os.ReadFile(self)
	if err != nil {
		t.Fatalf("reading the test binary: %v", err)
	}

	if err := os.WriteFile(path, data, 0o755); err != nil {
		t.Fatalf("copying the test binary: %v", err)
	}
}

// startImage starts the copy of the test binary at path as a running image and returns once it
// has written its first line. Cleanup ends the process and waits for it, and is registered after
// every temporary directory the caller created before, so it runs before they are removed.
func startImage(t *testing.T, path string) {
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
	cmd.Env = append(os.Environ(), runningImageEnv+"=1")
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

	exited := make(chan struct{})

	go func() {
		_ = cmd.Wait()
		close(exited)
	}()

	t.Cleanup(func() {
		_ = stdinW.Close()
		_ = cmd.Process.Kill()
		<-exited
		_ = stdoutR.Close()
	})

	ready := make(chan error, 1)

	go func() {
		line, err := bufio.NewReader(stdoutR).ReadString('\n')
		if err == nil && line != "running\n" {
			err = errors.New("the image wrote " + line)
		}

		ready <- err
	}()

	select {
	case err := <-ready:
		if err != nil {
			t.Fatalf("waiting for the image to start: %v", err)
		}
	case <-time.After(runningLimit):
		t.Fatalf("the image did not start within %v", runningLimit)
	}
}

// TestRollbackOverARunningImage rolls back while the target is the image of a running process,
// which Windows lets be renamed but not replaced: the target ends up holding the backup, and
// <target>.failed the running image.
func TestRollbackOverARunningImage(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	rec := pendingRecord()
	rec.Target = filepath.Join(dir, "tool.exe")
	rec.Backup = filepath.Join(dir, "tool.exe.old")

	copyTestBinary(t, rec.Target)
	writeRaw(t, rec.Backup, "old")

	image := readRaw(t, rec.Target)

	store := storeWith(t, rec)
	startImage(t, rec.Target)

	m, err := New(store, newVersion, WithClock(fakeClock{now: testTime()}))
	if err != nil {
		t.Fatal(err)
	}

	rolledBack, err := m.MarkFailed(t.Context(), "broken")
	if err != nil || !rolledBack {
		t.Fatalf("MarkFailed over the running image = %v, %v, want true and no error", rolledBack, err)
	}

	if got := readRaw(t, rec.Target); got != "old" {
		t.Errorf("target holds %d bytes, want the backup's %q", len(got), "old")
	}

	if got := readRaw(t, rec.Target+failedSuffix); got != image {
		t.Errorf("%s holds %d bytes, want the running image's %d", rec.Target+failedSuffix, len(got), len(image))
	}

	if _, err := os.Lstat(rec.Backup); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("Lstat(%s) = %v, want it absent", rec.Backup, err)
	}

	if got := load(t, store); got.Status != StatusRolledBack {
		t.Errorf("status = %v, want rolled_back", got.Status)
	}
}
