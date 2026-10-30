package update

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

// stageBinary creates a staged binary holding content in dir, as Extract leaves one: an open file.
func stageBinary(t *testing.T, dir, content string) *Staged {
	t.Helper()

	f, err := os.CreateTemp(dir, "atk-update-*")
	if err != nil {
		t.Fatalf("CreateTemp: %v", err)
	}

	if _, err := f.WriteString(content); err != nil {
		t.Fatalf("WriteString: %v", err)
	}

	s := &Staged{f: f, remove: os.Remove}
	t.Cleanup(func() { _ = s.Discard() })

	return s
}

// readFile returns the content of path.
func readFile(t *testing.T, path string) string {
	t.Helper()

	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	return string(b)
}

// assertMissing fails t unless path does not exist.
func assertMissing(t *testing.T, path string) {
	t.Helper()

	if _, err := os.Lstat(path); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("Lstat(%q) = %v, want it not to exist", path, err)
	}
}

// installFixture is an executable holding "old" in its own resolved directory, and a staged binary
// holding "new" beside it.
type installFixture struct {
	dir    string
	exe    string
	staged *Staged
	config Config
}

func newInstallFixture(t *testing.T) installFixture {
	t.Helper()

	dir := resolvedDir(t, t.TempDir())
	exe := filepath.Join(dir, "atk")

	if err := os.WriteFile(exe, []byte("old"), 0o755); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	return installFixture{
		dir:    dir,
		exe:    exe,
		staged: stageBinary(t, dir, "new"),
		config: executableConfig(t, exe),
	}
}

// stepLog records the destructive steps an fsSteps took.
type stepLog struct {
	mu    sync.Mutex
	calls []string
}

func (l *stepLog) add(call string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.calls = append(l.calls, call)
}

func (l *stepLog) list() []string {
	l.mu.Lock()
	defer l.mu.Unlock()

	return slices.Clone(l.calls)
}

// loggedSteps returns the real fsSteps, recording every chmod, rename and remove in log.
func loggedSteps(log *stepLog) fsSteps {
	s := defaultSteps()
	chmod, rename, remove := s.chmod, s.rename, s.remove

	s.chmod = func(name string, mode os.FileMode) error {
		log.add("chmod " + filepath.Base(name))
		return chmod(name, mode)
	}
	s.rename = func(oldpath, newpath string) error {
		log.add("rename " + filepath.Base(oldpath) + " " + filepath.Base(newpath))
		return rename(oldpath, newpath)
	}
	s.remove = func(name string) error {
		log.add("remove " + filepath.Base(name))
		return remove(name)
	}

	return s
}

func TestInstallReplacesTheExecutableAndKeepsABackup(t *testing.T) {
	t.Parallel()

	fx := newInstallFixture(t)
	name := fx.staged.Name()

	inst, err := fx.config.Install(fx.staged)
	if err != nil {
		t.Fatalf("Install: %v", err)
	}

	if inst.Path() != fx.exe {
		t.Errorf("Path = %q, want %q", inst.Path(), fx.exe)
	}

	if want := fx.exe + ".old"; inst.Backup() != want {
		t.Errorf("Backup = %q, want %q", inst.Backup(), want)
	}

	if got := readFile(t, fx.exe); got != "new" {
		t.Errorf("executable holds %q, want %q", got, "new")
	}

	if got := readFile(t, inst.Backup()); got != "old" {
		t.Errorf("backup holds %q, want %q", got, "old")
	}

	assertMissing(t, name)
}

func TestInstallReplacesAStaleBackup(t *testing.T) {
	t.Parallel()

	fx := newInstallFixture(t)

	if err := os.WriteFile(fx.exe+".old", []byte("stale"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	inst, err := fx.config.Install(fx.staged)
	if err != nil {
		t.Fatalf("Install: %v", err)
	}

	if got := readFile(t, inst.Backup()); got != "old" {
		t.Errorf("backup holds %q, want %q", got, "old")
	}
}

func TestInstallNilStaged(t *testing.T) {
	t.Parallel()

	fx := newInstallFixture(t)

	if _, err := fx.config.Install(nil); !errors.Is(err, errNoStaged) {
		t.Errorf("Install(nil) = %v, want errNoStaged", err)
	}
}

func TestInstallInvalidConfigRemovesTheStagedFileAndLeavesTheExecutable(t *testing.T) {
	t.Parallel()

	fx := newInstallFixture(t)
	name := fx.staged.Name()

	c := Config{Executable: func() (string, error) {
		t.Error("Executable called for an invalid Config")
		return fx.exe, nil
	}}

	inst, err := c.Install(fx.staged)
	if err == nil {
		t.Error("Install error = nil, want the validation error")
	}

	if inst != nil {
		t.Errorf("Install = %v, want a nil *Installed on error", inst)
	}

	assertMissing(t, name)
	assertMissing(t, fx.exe+".old")

	if got := readFile(t, fx.exe); got != "old" {
		t.Errorf("executable holds %q, want %q", got, "old")
	}
}

func TestInstallStagedInAnotherDirectoryFailsBeforeAnyDestructiveStep(t *testing.T) {
	t.Parallel()

	fx := newInstallFixture(t)
	elsewhere := stageBinary(t, t.TempDir(), "new")
	name := elsewhere.Name()

	var log stepLog

	_, err := fx.config.install(elsewhere, loggedSteps(&log))
	if err == nil || !strings.Contains(err.Error(), "is not in the executable's directory") {
		t.Fatalf("install = %v, want a directory mismatch", err)
	}

	if got, want := log.list(), []string{"remove " + filepath.Base(name)}; !slices.Equal(got, want) {
		t.Errorf("steps = %q, want %q", got, want)
	}

	if got := readFile(t, fx.exe); got != "old" {
		t.Errorf("executable holds %q, want %q", got, "old")
	}

	assertMissing(t, name)
	assertMissing(t, fx.exe+".old")
}

func TestInstallStagedInTheExecutablesDirectoryThroughASymlink(t *testing.T) {
	t.Parallel()

	fx := newInstallFixture(t)
	link := filepath.Join(t.TempDir(), "bin")

	if err := os.Symlink(fx.dir, link); err != nil {
		t.Skipf("Symlink: %v", err)
	}

	staged := stageBinary(t, link, "linked")

	if _, err := fx.config.Install(staged); err != nil {
		t.Fatalf("Install: %v", err)
	}

	if got := readFile(t, fx.exe); got != "linked" {
		t.Errorf("executable holds %q, want %q", got, "linked")
	}
}

func TestInstallFailingRenameIntoPlaceRestoresTheBackup(t *testing.T) {
	t.Parallel()

	fx := newInstallFixture(t)
	name := fx.staged.Name()
	errRename := errors.New("rename refused")

	s := defaultSteps()
	s.rename = func(oldpath, newpath string) error {
		if oldpath == name {
			return errRename
		}

		return os.Rename(oldpath, newpath)
	}

	_, err := fx.config.install(fx.staged, s)
	if !errors.Is(err, errRename) {
		t.Fatalf("install = %v, want it to wrap the rename error", err)
	}

	var rerr *RestoreError
	if errors.As(err, &rerr) {
		t.Errorf("install = %v, want no *RestoreError after a successful restore", err)
	}

	if got := readFile(t, fx.exe); got != "old" {
		t.Errorf("executable holds %q, want %q", got, "old")
	}

	assertMissing(t, fx.exe+".old")
	assertMissing(t, name)
}

func TestInstallFailingRenameAndRestoreReturnsRestoreError(t *testing.T) {
	t.Parallel()

	fx := newInstallFixture(t)
	name := fx.staged.Name()
	backup := fx.exe + ".old"
	errInstall, errRestore := errors.New("install refused"), errors.New("restore refused")

	s := defaultSteps()
	s.rename = func(oldpath, newpath string) error {
		switch oldpath {
		case name:
			return errInstall
		case backup:
			return errRestore
		default:
			return os.Rename(oldpath, newpath)
		}
	}

	_, err := fx.config.install(fx.staged, s)

	var rerr *RestoreError
	if !errors.As(err, &rerr) {
		t.Fatalf("install = %v, want a *RestoreError", err)
	}

	if rerr.Path != fx.exe || rerr.Backup != backup {
		t.Errorf("RestoreError Path, Backup = %q, %q, want %q, %q", rerr.Path, rerr.Backup, fx.exe, backup)
	}

	if !errors.Is(err, errInstall) || !errors.Is(err, errRestore) {
		t.Errorf("install = %v, want it to wrap both rename errors", err)
	}

	if got := readFile(t, backup); got != "old" {
		t.Errorf("backup holds %q, want %q", got, "old")
	}

	assertMissing(t, name)
}

func TestInstallPermissionRefused(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		steps func(s *fsSteps, staged, exe string)
		// op and path are the step the PermissionError names; path is "staged" or "exe".
		op, path string
		// renamed is whether the executable is moved aside and back.
		renamed bool
	}{
		{
			name: "chmod",
			steps: func(s *fsSteps, _, _ string) {
				s.chmod = func(string, os.FileMode) error { return fs.ErrPermission }
			},
			op:   "chmod",
			path: "staged",
		},
		{
			name: "rename to backup",
			steps: func(s *fsSteps, _, exe string) {
				s.rename = func(oldpath, newpath string) error {
					if oldpath == exe {
						return &os.LinkError{Op: "rename", Old: oldpath, New: newpath, Err: fs.ErrPermission}
					}

					return os.Rename(oldpath, newpath)
				}
			},
			op:   "rename",
			path: "exe",
		},
		{
			name: "rename into place",
			steps: func(s *fsSteps, staged, _ string) {
				s.rename = func(oldpath, newpath string) error {
					if oldpath == staged {
						return &os.LinkError{Op: "rename", Old: oldpath, New: newpath, Err: fs.ErrPermission}
					}

					return os.Rename(oldpath, newpath)
				}
			},
			op:      "rename",
			path:    "staged",
			renamed: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			fx := newInstallFixture(t)
			name := fx.staged.Name()

			s := defaultSteps()
			tt.steps(&s, name, fx.exe)

			_, err := fx.config.install(fx.staged, s)

			var perr *PermissionError
			if !errors.As(err, &perr) {
				t.Fatalf("install = %v, want a *PermissionError", err)
			}

			want := map[string]string{"staged": name, "exe": fx.exe}[tt.path]
			if perr.Op != tt.op || perr.Path != want || perr.Hint == "" {
				t.Errorf("PermissionError = %+v, want Op %q, Path %q and a Hint", perr, tt.op, want)
			}

			if got := readFile(t, fx.exe); got != "old" {
				t.Errorf("executable holds %q, want %q", got, "old")
			}

			assertMissing(t, fx.exe+".old")
			assertMissing(t, name)
		})
	}
}

func TestInstallStatFailure(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		// permission is whether the result is a *PermissionError.
		permission bool
	}{
		{"plain error", errors.New("stat refused"), false},
		{"permission refused", &fs.PathError{Op: "stat", Path: "atk", Err: fs.ErrPermission}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			fx := newInstallFixture(t)
			name := fx.staged.Name()

			s := defaultSteps()
			s.stat = func(string) (os.FileInfo, error) { return nil, tt.err }

			_, err := fx.config.install(fx.staged, s)
			if !errors.Is(err, tt.err) {
				t.Fatalf("install = %v, want it to wrap %v", err, tt.err)
			}

			var perr *PermissionError

			if got := errors.As(err, &perr); got != tt.permission {
				t.Fatalf("install = %v, *PermissionError %t, want %t", err, got, tt.permission)
			}

			if tt.permission && (perr.Op != "stat" || perr.Path != fx.exe || perr.Hint == "") {
				t.Errorf("PermissionError = %+v, want Op %q, Path %q and a Hint", perr, "stat", fx.exe)
			}

			if strings.Contains(err.Error(), "removing staged binary") {
				t.Errorf("install = %v, want no mention of removing the staged binary", err)
			}

			if got := readFile(t, fx.exe); got != "old" {
				t.Errorf("executable holds %q, want %q", got, "old")
			}

			assertMissing(t, fx.exe+".old")
			assertMissing(t, name)
		})
	}
}

func TestInstallFailedCleanupJoinsTheRemoveError(t *testing.T) {
	t.Parallel()

	fx := newInstallFixture(t)
	errStat, errRemove := errors.New("stat refused"), errors.New("remove refused")

	s := defaultSteps()
	s.stat = func(string) (os.FileInfo, error) { return nil, errStat }
	s.remove = func(string) error { return errRemove }

	_, err := fx.config.install(fx.staged, s)
	if !errors.Is(err, errStat) || !errors.Is(err, errRemove) {
		t.Fatalf("install = %v, want it to wrap both the step and the remove error", err)
	}

	if !strings.Contains(err.Error(), "removing staged binary") {
		t.Errorf("install = %v, want it to mention removing the staged binary", err)
	}
}

func TestInstallSymlinkedExecutable(t *testing.T) {
	t.Parallel()

	fx := newInstallFixture(t)
	link := filepath.Join(t.TempDir(), "atk")

	if err := os.Symlink(fx.exe, link); err != nil {
		t.Skipf("Symlink: %v", err)
	}

	c := executableConfig(t, link)

	inst, err := c.Install(fx.staged)
	if err != nil {
		t.Fatalf("Install: %v", err)
	}

	if inst.Path() != fx.exe {
		t.Errorf("Path = %q, want the resolved %q", inst.Path(), fx.exe)
	}

	info, err := os.Lstat(link)
	if err != nil {
		t.Fatalf("Lstat: %v", err)
	}

	if info.Mode()&fs.ModeSymlink == 0 {
		t.Errorf("link mode = %v, want a symbolic link", info.Mode())
	}

	if dest, err := os.Readlink(link); err != nil || dest != fx.exe {
		t.Errorf("Readlink = %q, %v, want %q", dest, err, fx.exe)
	}

	if got := readFile(t, link); got != "new" {
		t.Errorf("link reads %q, want %q", got, "new")
	}
}

func TestInstallDanglingSymlink(t *testing.T) {
	t.Parallel()

	dir := resolvedDir(t, t.TempDir())
	link := filepath.Join(dir, "atk")

	if err := os.Symlink(filepath.Join(dir, "missing"), link); err != nil {
		t.Skipf("Symlink: %v", err)
	}

	staged := stageBinary(t, dir, "new")
	name := staged.Name()

	_, err := executableConfig(t, link).Install(staged)
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("Install = %v, want one matching fs.ErrNotExist", err)
	}

	assertMissing(t, name)
}

func TestInstalledDiscardRemovesTheBackup(t *testing.T) {
	t.Parallel()

	fx := newInstallFixture(t)

	inst, err := fx.config.Install(fx.staged)
	if err != nil {
		t.Fatalf("Install: %v", err)
	}

	if err := inst.Discard(); err != nil {
		t.Fatalf("Discard: %v", err)
	}

	assertMissing(t, inst.Backup())

	if got := readFile(t, fx.exe); got != "new" {
		t.Errorf("executable holds %q, want %q", got, "new")
	}
}

func TestInstalledRollbackRestoresThePreviousBinary(t *testing.T) {
	t.Parallel()

	fx := newInstallFixture(t)

	inst, err := fx.config.Install(fx.staged)
	if err != nil {
		t.Fatalf("Install: %v", err)
	}

	if err := inst.Rollback(); err != nil {
		t.Fatalf("Rollback: %v", err)
	}

	if got := readFile(t, fx.exe); got != "old" {
		t.Errorf("executable holds %q, want %q", got, "old")
	}

	assertMissing(t, inst.Backup())
}

func TestInstalledDiscardLockedBackup(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		goos   string
		err    error
		locked bool
	}{
		{"sharing violation on windows", "windows", windowsSharingViolation, true},
		{"access denied on windows", "windows", &fs.PathError{Op: "remove", Err: windowsAccessDenied}, true},
		{"same errno elsewhere", "linux", windowsSharingViolation, false},
		{"another error on windows", "windows", errors.New("disk on fire"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			fx := newInstallFixture(t)
			fx.config.GOOS = tt.goos

			s := defaultSteps()
			backup := fx.exe + ".old"
			s.remove = func(name string) error {
				if name == backup {
					return tt.err
				}

				return os.Remove(name)
			}

			inst, err := fx.config.install(fx.staged, s)
			if err != nil {
				t.Fatalf("install: %v", err)
			}

			err = inst.Discard()
			if !errors.Is(err, tt.err) {
				t.Errorf("Discard = %v, want it to wrap %v", err, tt.err)
			}

			var lerr *BackupLockedError
			if got := errors.As(err, &lerr); got != tt.locked {
				t.Fatalf("Discard = %v, *BackupLockedError %t, want %t", err, got, tt.locked)
			}

			if tt.locked && lerr.Path != backup {
				t.Errorf("BackupLockedError Path = %q, want %q", lerr.Path, backup)
			}
		})
	}
}

func TestInstalledDiscardPermissionRefused(t *testing.T) {
	t.Parallel()

	fx := newInstallFixture(t)
	backup := fx.exe + ".old"

	s := defaultSteps()
	s.remove = func(name string) error {
		if name == backup {
			return &fs.PathError{Op: "remove", Path: name, Err: fs.ErrPermission}
		}

		return os.Remove(name)
	}

	inst, err := fx.config.install(fx.staged, s)
	if err != nil {
		t.Fatalf("install: %v", err)
	}

	var perr *PermissionError
	if err := inst.Discard(); !errors.As(err, &perr) || perr.Path != backup {
		t.Errorf("Discard = %v, want a *PermissionError for %q", err, backup)
	}
}

func TestInstalledSettlesOnce(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		first func(*Installed) error
		// holds is what the executable holds once the first call settled the Installed.
		holds string
	}{
		{"discard first", (*Installed).Discard, "new"},
		{"rollback first", (*Installed).Rollback, "old"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			fx := newInstallFixture(t)

			var log stepLog

			inst, err := fx.config.install(fx.staged, loggedSteps(&log))
			if err != nil {
				t.Fatalf("install: %v", err)
			}

			if err := tt.first(inst); err != nil {
				t.Fatalf("first call: %v", err)
			}

			settled := len(log.list())

			for _, call := range []func() error{inst.Discard, inst.Rollback, inst.Discard, inst.Rollback} {
				if err := call(); err != nil {
					t.Errorf("later call = %v, want nil", err)
				}
			}

			if got := log.list(); len(got) != settled {
				t.Errorf("steps after settling = %q, want none", got[settled:])
			}

			if got := readFile(t, fx.exe); got != tt.holds {
				t.Errorf("executable holds %q, want %q", got, tt.holds)
			}
		})
	}
}

func TestInstalledSettlesOnceAcrossGoroutines(t *testing.T) {
	t.Parallel()

	fx := newInstallFixture(t)

	var removes atomic.Int32

	s := defaultSteps()
	s.remove = func(name string) error {
		removes.Add(1)
		return os.Remove(name)
	}

	inst, err := fx.config.install(fx.staged, s)
	if err != nil {
		t.Fatalf("install: %v", err)
	}

	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)

		go func() {
			defer wg.Done()

			if err := inst.Discard(); err != nil {
				t.Errorf("Discard: %v", err)
			}
		}()
	}

	wg.Wait()

	if got := removes.Load(); got != 1 {
		t.Errorf("removes = %d, want 1", got)
	}
}

func TestInstalledPathFeedsPostUpdate(t *testing.T) {
	t.Parallel()

	fx := newInstallFixture(t)

	inst, err := fx.config.Install(fx.staged)
	if err != nil {
		t.Fatalf("Install: %v", err)
	}

	var seen string

	hook := func(path string) error {
		b, err := os.ReadFile(path)
		seen = string(b)

		return err
	}

	if err := RunPostUpdate([]PostUpdate{hook}, inst.Path()); err != nil {
		t.Fatalf("RunPostUpdate: %v", err)
	}

	if seen != "new" {
		t.Errorf("hook read %q, want %q", seen, "new")
	}
}
