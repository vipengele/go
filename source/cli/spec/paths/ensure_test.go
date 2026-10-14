package paths

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.vipengele.org/cli/spec"
)

func ensureIdentity() spec.Identity {
	return spec.Identity{
		Name:      "atk",
		Owner:     "pedromvgomes",
		Repo:      "agentic-toolkit",
		EnvPrefix: "ATK",
		DirName:   "agentic-toolkit",
	}
}

// newDirs resolves every kind as a subdirectory of root on the goos layout.
func newDirs(t *testing.T, goos, root string) Dirs {
	t.Helper()
	d, err := New(ensureIdentity(), Options{Home: root, GOOS: goos})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return d
}

// requireModes skips the test when the file system under t.TempDir cannot represent the
// permission bits the mode assertions read, or the umask narrows a created directory's 0700.
func requireModes(t *testing.T) {
	t.Helper()
	probe := filepath.Join(t.TempDir(), "probe")
	if err := os.MkdirAll(probe, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if modeOf(t, probe) != 0o700 {
		t.Skip("a directory created with mode 0700 does not have mode 0700 here")
	}
	for _, perm := range []fs.FileMode{0o755, 0o500, 0o700} {
		if err := os.Chmod(probe, perm); err != nil {
			t.Fatalf("Chmod: %v", err)
		}
		if got := modeOf(t, probe); got != perm {
			t.Skipf("file system cannot represent mode %#o: got %#o", perm, got)
		}
	}
}

func modeOf(t *testing.T, path string) fs.FileMode {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	return info.Mode().Perm()
}

func mkdirMode(t *testing.T, path string, perm fs.FileMode) {
	t.Helper()
	if err := os.MkdirAll(path, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.Chmod(path, perm); err != nil {
		t.Fatalf("Chmod: %v", err)
	}
}

func assertDir(t *testing.T, path string) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat(%s): %v", path, err)
	}
	if !info.IsDir() {
		t.Fatalf("%s is not a directory", path)
	}
}

func assertMissing(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("Stat(%s) = %v, want it not to exist", path, err)
	}
}

func allDirs(d Dirs) []string {
	return []string{d.Config(), d.State(), d.Cache(), d.Data(), d.Log()}
}

func TestEnsureWithNoKindsCreatesEveryKind(t *testing.T) {
	requireModes(t)
	root := filepath.Join(t.TempDir(), "home")
	d := newDirs(t, "linux", root)

	if err := d.Ensure(); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	for _, dir := range allDirs(d) {
		assertDir(t, dir)
		if got := modeOf(t, dir); got != 0o700 {
			t.Errorf("mode of %s = %#o, want 0700", dir, got)
		}
	}
	if got := modeOf(t, root); got != 0o700 {
		t.Errorf("mode of created parent %s = %#o, want 0700", root, got)
	}
}

func TestEnsureCreatesOnlyTheKindsGiven(t *testing.T) {
	d := newDirs(t, "linux", t.TempDir())

	if err := d.Ensure(Cache, Data); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	assertDir(t, d.Cache())
	assertDir(t, d.Data())
	for _, dir := range []string{d.Config(), d.State(), d.Log()} {
		assertMissing(t, dir)
	}
}

func TestEnsureRepeatedKindIsHarmless(t *testing.T) {
	d := newDirs(t, "linux", t.TempDir())

	if err := d.Ensure(Config, Config, Config); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	assertDir(t, d.Config())
	if err := d.Ensure(Config); err != nil {
		t.Fatalf("Ensure on an existing directory: %v", err)
	}
}

func TestEnsureClearsModeBitsBeyondOwner(t *testing.T) {
	requireModes(t)
	tests := []struct {
		perm, want fs.FileMode
	}{
		{0o755, 0o700},
		{0o775, 0o700},
		{0o777, 0o700},
		{0o750, 0o700},
		{0o705, 0o700},
		{0o751, 0o700},
		{0o555, 0o500},
		{0o640, 0o600},
	}
	for _, tt := range tests {
		t.Run(tt.perm.String(), func(t *testing.T) {
			d := newDirs(t, "linux", t.TempDir())
			mkdirMode(t, d.Config(), tt.perm)

			if err := d.Ensure(Config); err != nil {
				t.Fatalf("Ensure: %v", err)
			}
			if got := modeOf(t, d.Config()); got != tt.want {
				t.Errorf("mode = %#o, want %#o", got, tt.want)
			}
		})
	}
}

func TestEnsureKeepsNarrowerMode(t *testing.T) {
	requireModes(t)
	for _, perm := range []fs.FileMode{0o700, 0o600, 0o500, 0o300} {
		t.Run(perm.String(), func(t *testing.T) {
			d := newDirs(t, "linux", t.TempDir())
			mkdirMode(t, d.Config(), perm)
			t.Cleanup(func() { _ = os.Chmod(d.Config(), 0o700) })

			chmod := func(path string, _ fs.FileMode) error {
				t.Errorf("chmod(%s) called on a directory with mode %#o", path, perm)
				return nil
			}
			if err := d.ensure(chmod, []Kind{Config}); err != nil {
				t.Fatalf("ensure: %v", err)
			}
			if got := modeOf(t, d.Config()); got != perm {
				t.Errorf("mode = %#o, want %#o kept", got, perm)
			}
		})
	}
}

func TestEnsureLeavesExistingParentUntouched(t *testing.T) {
	requireModes(t)
	root := t.TempDir()
	parent := filepath.Join(root, ".config")
	mkdirMode(t, parent, 0o755)
	d, err := New(ensureIdentity(), Options{
		GOOS:        "linux",
		LookupEnv:   func(string) (string, bool) { return "", false },
		UserHomeDir: func() (string, error) { return root, nil },
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if filepath.Dir(d.Config()) != parent {
		t.Fatalf("Config() = %s, want a child of %s", d.Config(), parent)
	}

	if err := d.Ensure(Config); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	assertDir(t, d.Config())
	if got := modeOf(t, d.Config()); got != 0o700 {
		t.Errorf("mode of %s = %#o, want 0700", d.Config(), got)
	}
	if got := modeOf(t, parent); got != 0o755 {
		t.Errorf("mode of existing parent %s = %#o, want 0755 unchanged", parent, got)
	}
}

func TestEnsureChmodErrorNamesThePath(t *testing.T) {
	requireModes(t)
	d := newDirs(t, "linux", t.TempDir())
	mkdirMode(t, d.Config(), 0o755)
	errDenied := errors.New("operation not permitted")
	chmod := func(string, fs.FileMode) error { return errDenied }

	err := d.ensure(chmod, []Kind{Config})
	if !errors.Is(err, errDenied) {
		t.Fatalf("ensure = %v, want it to wrap %v", err, errDenied)
	}
	if !strings.Contains(err.Error(), d.Config()) {
		t.Errorf("error %q does not name %s", err, d.Config())
	}
}

func TestEnsureJoinsTheFailuresOfEveryKind(t *testing.T) {
	d := newDirs(t, "linux", t.TempDir())
	mkdirMode(t, filepath.Dir(d.Config()), 0o700)
	for _, path := range []string{d.Config(), d.Cache()} {
		if err := os.WriteFile(path, nil, 0o600); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
	}

	err := d.Ensure()
	if err == nil {
		t.Fatal("Ensure succeeded, want an error for config and cache")
	}
	for _, path := range []string{d.Config(), d.Cache()} {
		if !strings.Contains(err.Error(), path) {
			t.Errorf("error %q does not name %s", err, path)
		}
	}
	for _, dir := range []string{d.State(), d.Data(), d.Log()} {
		assertDir(t, dir)
	}
}

func TestEnsureRerunRepairsPartialTree(t *testing.T) {
	requireModes(t)
	d := newDirs(t, "linux", t.TempDir())
	mkdirMode(t, d.State(), 0o755)
	mkdirMode(t, filepath.Dir(d.Config()), 0o700)
	if err := os.WriteFile(d.Config(), nil, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	failChmod := func(string, fs.FileMode) error { return errors.New("operation not permitted") }

	if err := d.ensure(failChmod, nil); err == nil {
		t.Fatal("ensure succeeded, want the config file and the failed chmod reported")
	}
	if got := modeOf(t, d.State()); got != 0o755 {
		t.Fatalf("mode of state = %#o, want 0755 left by the failed chmod", got)
	}

	if err := os.Remove(d.Config()); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if err := d.Ensure(); err != nil {
		t.Fatalf("Ensure rerun: %v", err)
	}
	for _, dir := range allDirs(d) {
		assertDir(t, dir)
		if got := modeOf(t, dir); got != 0o700 {
			t.Errorf("mode of %s = %#o, want 0700", dir, got)
		}
	}
}

func TestEnsureSkipsModeChangeOnWindowsLayout(t *testing.T) {
	requireModes(t)
	d := newDirs(t, "windows", t.TempDir())
	mkdirMode(t, d.Config(), 0o755)
	chmod := func(path string, _ fs.FileMode) error {
		t.Errorf("chmod(%s) called on the windows layout", path)
		return nil
	}

	if err := d.ensure(chmod, []Kind{Config, State}); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if got := modeOf(t, d.Config()); got != 0o755 {
		t.Errorf("mode = %#o, want 0755 left alone", got)
	}
	assertDir(t, d.State())
}

func TestEnsureRejectsPathThatIsNotADirectory(t *testing.T) {
	d := newDirs(t, "linux", t.TempDir())
	mkdirMode(t, filepath.Dir(d.Log()), 0o700)
	if err := os.WriteFile(d.Log(), []byte("x"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	err := d.Ensure(Log)
	if err == nil || !strings.Contains(err.Error(), d.Log()) {
		t.Fatalf("Ensure = %v, want an error naming %s", err, d.Log())
	}
}

func TestEnsureRejectsInvalidKind(t *testing.T) {
	d := newDirs(t, "linux", t.TempDir())

	for _, k := range []Kind{0, Log + 1, -1} {
		if err := d.Ensure(k); err == nil {
			t.Errorf("Ensure(%v) succeeded, want an error", k)
		}
	}
	err := d.Ensure(Kind(0), Config)
	if err == nil || !strings.Contains(err.Error(), "Kind(0)") {
		t.Errorf("Ensure(Kind(0), Config) = %v, want an error naming Kind(0)", err)
	}
	assertDir(t, d.Config())
}

func TestEnsureZeroDirsCreatesNothing(t *testing.T) {
	wd := t.TempDir()
	t.Chdir(wd)

	err := Dirs{}.Ensure()
	if err == nil {
		t.Fatal("Ensure on the zero Dirs succeeded, want an error")
	}
	for _, k := range []Kind{Config, State, Cache, Data, Log} {
		if !strings.Contains(err.Error(), k.String()) {
			t.Errorf("error %q does not name kind %v", err, k)
		}
	}
	entries, err := os.ReadDir(wd)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("working directory holds %d entries, want none", len(entries))
	}
}

func TestEnsureReportsAKindResolveWasNotAskedFor(t *testing.T) {
	root := t.TempDir()
	d, err := Resolve(ensureIdentity(), Options{Home: root, GOOS: "linux"}, State)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	err = d.Ensure(State, Config)
	if err == nil || !strings.Contains(err.Error(), "config: no directory resolved") {
		t.Fatalf("Ensure(State, Config) = %v, want an error naming the unresolved config kind", err)
	}
	assertDir(t, d.State())
	assertMissing(t, filepath.Join(root, "config"))
}
