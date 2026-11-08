package update

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

// staleFixture is an executable holding "new" in its own resolved directory, with a backup
// holding "old" beside it when withBackup is true.
func staleFixture(t *testing.T, withBackup bool) (exe, backup string, config Config) {
	t.Helper()

	dir := resolvedDir(t, t.TempDir())
	exe = filepath.Join(dir, "atk")
	backup = exe + ".old"

	if err := os.WriteFile(exe, []byte("new"), 0o755); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if withBackup {
		if err := os.WriteFile(backup, []byte("old"), 0o755); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
	}

	return exe, backup, executableConfig(t, exe)
}

func TestRemoveStaleBackupWithoutABackupReturnsNil(t *testing.T) {
	t.Parallel()

	exe, backup, config := staleFixture(t, false)

	if err := config.RemoveStaleBackup(); err != nil {
		t.Fatalf("RemoveStaleBackup = %v, want nil", err)
	}

	assertMissing(t, backup)

	if got := readFile(t, exe); got != "new" {
		t.Errorf("executable holds %q, want %q", got, "new")
	}
}

func TestRemoveStaleBackupRemovesTheBackup(t *testing.T) {
	t.Parallel()

	exe, backup, config := staleFixture(t, true)

	if err := config.RemoveStaleBackup(); err != nil {
		t.Fatalf("RemoveStaleBackup = %v, want nil", err)
	}

	assertMissing(t, backup)

	if got := readFile(t, exe); got != "new" {
		t.Errorf("executable holds %q, want %q", got, "new")
	}
}

func TestRemoveStaleBackupOfASymlinkedExecutableRemovesTheTargetsBackup(t *testing.T) {
	t.Parallel()

	_, backup, config := staleFixture(t, true)
	exe, err := config.Executable()
	if err != nil {
		t.Fatalf("Executable: %v", err)
	}

	link := filepath.Join(resolvedDir(t, t.TempDir()), "atk")
	if err := os.Symlink(exe, link); err != nil {
		t.Skipf("Symlink: %v", err)
	}

	linkBackup := link + ".old"
	if err := os.WriteFile(linkBackup, []byte("other"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	config.Executable = func() (string, error) { return link, nil }

	if err := config.RemoveStaleBackup(); err != nil {
		t.Fatalf("RemoveStaleBackup = %v, want nil", err)
	}

	assertMissing(t, backup)

	if got := readFile(t, linkBackup); got != "other" {
		t.Errorf("file beside the symbolic link holds %q, want %q", got, "other")
	}
}

func TestRemoveStaleBackupFailures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		goos       string
		err        error
		locked     bool
		permission bool
	}{
		{"sharing violation on windows", "windows", windowsSharingViolation, true, false},
		{"access denied on windows", "windows", &fs.PathError{Op: "remove", Err: windowsAccessDenied}, true, false},
		{"same errno elsewhere", "linux", windowsSharingViolation, false, false},
		{"permission refused", "linux", &fs.PathError{Op: "remove", Err: fs.ErrPermission}, false, true},
		{"another error on windows", "windows", errors.New("disk on fire"), false, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, backup, config := staleFixture(t, true)
			config.GOOS = tt.goos

			s := defaultSteps()
			s.remove = func(name string) error {
				if name == backup {
					return tt.err
				}

				return os.Remove(name)
			}

			err := config.removeStaleBackup(s)
			if !errors.Is(err, tt.err) {
				t.Errorf("RemoveStaleBackup = %v, want it to wrap %v", err, tt.err)
			}

			var lerr *BackupLockedError
			if got := errors.As(err, &lerr); got != tt.locked {
				t.Errorf("RemoveStaleBackup = %v, *BackupLockedError %t, want %t", err, got, tt.locked)
			} else if tt.locked && lerr.Path != backup {
				t.Errorf("BackupLockedError Path = %q, want %q", lerr.Path, backup)
			}

			var perr *PermissionError
			if got := errors.As(err, &perr); got != tt.permission {
				t.Errorf("RemoveStaleBackup = %v, *PermissionError %t, want %t", err, got, tt.permission)
			} else if tt.permission && perr.Path != backup {
				t.Errorf("PermissionError Path = %q, want %q", perr.Path, backup)
			}

			if got := readFile(t, backup); got != "old" {
				t.Errorf("backup holds %q, want %q", got, "old")
			}
		})
	}
}

func TestRemoveStaleBackupMissingExecutableFailsWithoutRemoving(t *testing.T) {
	t.Parallel()

	dir := resolvedDir(t, t.TempDir())
	config := executableConfig(t, filepath.Join(dir, "atk"))

	s := defaultSteps()
	s.remove = func(name string) error {
		t.Errorf("remove(%q) called, want no removal", name)
		return nil
	}

	if err := config.removeStaleBackup(s); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("RemoveStaleBackup = %v, want it to wrap fs.ErrNotExist", err)
	}
}

func TestRemoveStaleBackupInvalidConfig(t *testing.T) {
	t.Parallel()

	_, backup, config := staleFixture(t, true)
	config.Name = ""

	if err := config.RemoveStaleBackup(); err == nil {
		t.Error("RemoveStaleBackup = nil, want the Validate error")
	}

	if got := readFile(t, backup); got != "old" {
		t.Errorf("backup holds %q, want %q", got, "old")
	}
}

func TestRemoveStaleBackupOnHostRemovesOnlyOnWindows(t *testing.T) {
	t.Parallel()

	tests := []struct {
		goos   string
		remove bool
	}{
		{"windows", true},
		{"linux", false},
		{"darwin", false},
	}

	for _, tt := range tests {
		t.Run(tt.goos, func(t *testing.T) {
			t.Parallel()

			exe, backup, config := staleFixture(t, true)

			if err := config.removeStaleBackupOnHost(tt.goos); err != nil {
				t.Fatalf("removeStaleBackupOnHost(%q) = %v, want nil", tt.goos, err)
			}

			if tt.remove {
				assertMissing(t, backup)
			} else if got := readFile(t, backup); got != "old" {
				t.Errorf("backup holds %q, want %q", got, "old")
			}

			if got := readFile(t, exe); got != "new" {
				t.Errorf("executable holds %q, want %q", got, "new")
			}
		})
	}
}

func TestRemoveStaleBackupOnHostOffWindowsLeavesAFailingConfigAlone(t *testing.T) {
	t.Parallel()

	_, _, config := staleFixture(t, true)
	config.Executable = func() (string, error) { return "", errors.New("no executable") }

	if err := config.removeStaleBackupOnHost("linux"); err != nil {
		t.Errorf("removeStaleBackupOnHost(%q) = %v, want nil", "linux", err)
	}

	if err := config.removeStaleBackupOnHost("windows"); err == nil {
		t.Errorf("removeStaleBackupOnHost(%q) = nil, want the Executable error", "windows")
	}
}
