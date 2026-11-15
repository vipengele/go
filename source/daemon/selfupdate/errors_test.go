package selfupdate

import (
	"errors"
	"io/fs"
	"strings"
	"testing"
)

func TestNoBackupErrorNamesTheBackup(t *testing.T) {
	t.Parallel()

	var err error = &NoBackupError{Path: "/opt/tool.old"}

	if got := err.Error(); !strings.Contains(got, "/opt/tool.old") {
		t.Errorf("Error() = %q, want it to name the backup", got)
	}

	var target *NoBackupError
	if !errors.As(err, &target) || target.Path != "/opt/tool.old" {
		t.Errorf("errors.As = %v, want the NoBackupError", target)
	}
}

func TestBackupRemovalErrorNamesTheBackupAndWrapsTheCause(t *testing.T) {
	t.Parallel()

	var err error = &BackupRemovalError{Path: "/opt/tool.old", Err: fs.ErrPermission}

	got := err.Error()
	for _, want := range []string{"/opt/tool.old", fs.ErrPermission.Error()} {
		if !strings.Contains(got, want) {
			t.Errorf("Error() = %q, want it to contain %q", got, want)
		}
	}

	if !errors.Is(err, fs.ErrPermission) {
		t.Errorf("errors.Is(%v, fs.ErrPermission) = false", err)
	}

	var target *BackupRemovalError
	if !errors.As(err, &target) {
		t.Error("errors.As did not find the BackupRemovalError")
	}
}

func TestRestoreErrorNamesBothPathsAndWrapsTheCause(t *testing.T) {
	t.Parallel()

	var err error = &RestoreError{Backup: "/opt/tool.old", Target: "/opt/tool", Err: fs.ErrPermission}

	got := err.Error()
	for _, want := range []string{"/opt/tool.old", "/opt/tool", fs.ErrPermission.Error()} {
		if !strings.Contains(got, want) {
			t.Errorf("Error() = %q, want it to contain %q", got, want)
		}
	}

	if !errors.Is(err, fs.ErrPermission) {
		t.Errorf("errors.Is(%v, fs.ErrPermission) = false", err)
	}

	var target *RestoreError
	if !errors.As(err, &target) {
		t.Error("errors.As did not find the RestoreError")
	}
}
