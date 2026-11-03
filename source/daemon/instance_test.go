package daemon_test

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"go.vipengele.org/cli/spec"
	"go.vipengele.org/cli/spec/paths"
	"go.vipengele.org/daemon"
	"go.vipengele.org/filelock"
)

func testIdentity() spec.Identity {
	return spec.Identity{
		Name:      "demo",
		Owner:     "vipengele",
		Repo:      "demo",
		EnvPrefix: "DEMO",
		DirName:   "demo",
	}
}

func holderPath(in *daemon.Instance) string {
	return filepath.Join(filepath.Dir(in.Path()), "instance.holder")
}

func mustAcquire(t *testing.T, opts paths.Options) *daemon.Instance {
	t.Helper()

	in, err := daemon.AcquireInstance(testIdentity(), opts)
	if err != nil {
		t.Fatalf("AcquireInstance: %v", err)
	}

	t.Cleanup(in.Release)

	return in
}

func TestAcquireInstanceSecondAcquireReportsHolderPID(t *testing.T) {
	t.Parallel()

	opts := paths.Options{RuntimeDir: t.TempDir()}
	mustAcquire(t, opts)

	_, err := daemon.AcquireInstance(testIdentity(), opts)
	if !errors.Is(err, daemon.ErrAlreadyRunning) {
		t.Fatalf("second AcquireInstance = %v, want ErrAlreadyRunning", err)
	}

	if !errors.Is(err, filelock.ErrLocked) {
		t.Errorf("error %v does not wrap filelock.ErrLocked", err)
	}

	var are *daemon.AlreadyRunningError
	if !errors.As(err, &are) {
		t.Fatalf("error %T is not an *AlreadyRunningError", err)
	}

	if are.PID != os.Getpid() {
		t.Errorf("PID = %d, want %d", are.PID, os.Getpid())
	}

	if want := strconv.Itoa(os.Getpid()); !strings.Contains(err.Error(), want) {
		t.Errorf("message %q does not name pid %s", err.Error(), want)
	}
}

func TestAcquireInstanceReleaseThenReacquire(t *testing.T) {
	t.Parallel()

	opts := paths.Options{RuntimeDir: t.TempDir()}
	in := mustAcquire(t, opts)

	in.Release()
	in.Release()

	if _, err := os.Stat(holderPath(in)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("holder record after Release: %v, want not exist", err)
	}

	again := mustAcquire(t, opts)
	again.Release()
}

func TestAcquireInstanceUnreadableRecordGivesPIDZero(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		content *string
	}{
		{"missing", nil},
		{"garbage", ptr("not a pid")},
		{"empty", ptr("")},
		{"negative", ptr("-4\n")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			opts := paths.Options{RuntimeDir: t.TempDir()}
			in := mustAcquire(t, opts)

			rec := holderPath(in)
			if tt.content == nil {
				if err := os.Remove(rec); err != nil {
					t.Fatalf("Remove: %v", err)
				}
			} else if err := os.WriteFile(rec, []byte(*tt.content), 0o600); err != nil {
				t.Fatalf("WriteFile: %v", err)
			}

			_, err := daemon.AcquireInstance(testIdentity(), opts)

			var are *daemon.AlreadyRunningError
			if !errors.As(err, &are) {
				t.Fatalf("AcquireInstance = %v, want an *AlreadyRunningError", err)
			}

			if are.PID != 0 {
				t.Errorf("PID = %d, want 0", are.PID)
			}
		})
	}
}

func ptr(s string) *string { return &s }

func TestAcquireInstanceReplacesStaleRecord(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "instance.holder"), []byte("999999\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	in := mustAcquire(t, paths.Options{RuntimeDir: dir})

	got, err := os.ReadFile(holderPath(in))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	if want := strconv.Itoa(os.Getpid()) + "\n"; string(got) != want {
		t.Errorf("record = %q, want %q", got, want)
	}
}

func TestAcquireInstanceSurvivesRecordFailures(t *testing.T) {
	t.Parallel()

	errShare := errors.New("sharing violation")
	acquirers := map[string]func(paths.Options) (*daemon.Instance, error){
		"rename and remove": func(o paths.Options) (*daemon.Instance, error) {
			return daemon.AcquireInstanceFailingFiles(testIdentity(), o, errShare)
		},
		"write": func(o paths.Options) (*daemon.Instance, error) {
			return daemon.AcquireInstanceFailingWrite(testIdentity(), o, errShare)
		},
	}
	for name, acquire := range acquirers {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			opts := paths.Options{RuntimeDir: t.TempDir()}

			in, err := acquire(opts)
			if err != nil {
				t.Fatalf("acquire: %v", err)
			}

			if _, err := daemon.AcquireInstance(testIdentity(), opts); !errors.Is(err, daemon.ErrAlreadyRunning) {
				t.Errorf("second acquire = %v, want ErrAlreadyRunning", err)
			}

			in.Release()

			again := mustAcquire(t, opts)
			again.Release()
		})
	}
}

func TestAcquireInstanceWrapsResolveFailure(t *testing.T) {
	t.Parallel()

	_, err := daemon.AcquireInstance(spec.Identity{}, paths.Options{RuntimeDir: t.TempDir()})
	if err == nil {
		t.Fatal("AcquireInstance with an invalid identity succeeded")
	}

	if errors.Is(err, daemon.ErrAlreadyRunning) {
		t.Errorf("error %v matches ErrAlreadyRunning", err)
	}
}

func TestAlreadyRunningErrorMessageWithoutPID(t *testing.T) {
	t.Parallel()

	err := &daemon.AlreadyRunningError{Path: "/x/instance.lock"}
	if got, want := err.Error(), "daemon: already running (lock /x/instance.lock)"; got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}
