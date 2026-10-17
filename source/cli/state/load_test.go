package state_test

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"go.vipengele.org/cli/state"
)

type record struct {
	Name  string `json:"name"  yaml:"name"`
	Count int    `json:"count" yaml:"count"`
}

// format is one encoding the package loads and saves, with content of each kind a test needs.
type format struct {
	name      string
	load      func(path string) (record, state.LoadStatus, error)
	save      func(path string, v record) error
	valid     string
	malformed string
}

func formats() []format {
	return []format{
		{
			name:      "json",
			load:      state.LoadJSON[record],
			save:      state.SaveJSON[record],
			valid:     `{"name": "alpha", "count": 3}`,
			malformed: `{"name": "alpha",`,
		},
		{
			name:      "yaml",
			load:      state.LoadYAML[record],
			save:      state.SaveYAML[record],
			valid:     "name: alpha\ncount: 3\n",
			malformed: "name: [alpha\n",
		},
	}
}

// alpha is the record every valid fixture holds.
func alpha() record { return record{Name: "alpha", Count: 3} }

// assertLoad checks the status, that errors.Is(err, ErrCorrupt) agrees with it, that only Missing
// and Loaded come with a nil error, that every error starts "state:", and that the value is the
// zero record on any status but Loaded.
func assertLoad(t *testing.T, got record, status state.LoadStatus, err error, want state.LoadStatus) {
	t.Helper()

	if status != want {
		t.Fatalf("status = %v (error %v), want %v", status, err, want)
	}

	if corrupt := errors.Is(err, state.ErrCorrupt); corrupt != (status == state.Corrupt) {
		t.Errorf("errors.Is(%v, ErrCorrupt) = %t with status %v", err, corrupt, status)
	}

	switch status {
	case state.Missing, state.Loaded:
		if err != nil {
			t.Errorf("error = %v, want nil with status %v", err, status)
		}
	case state.Corrupt, state.Unreadable:
		if err == nil {
			t.Fatalf("error = nil, want an error with status %v", status)
		}

		if !strings.HasPrefix(err.Error(), "state: ") {
			t.Errorf("error %q does not start with %q", err, "state: ")
		}
	}

	wantValue := record{}
	if status == state.Loaded {
		wantValue = alpha()
	}

	if got != wantValue {
		t.Errorf("value = %+v, want %+v", got, wantValue)
	}
}

func TestLoadReportsWhatItFoundAtThePath(t *testing.T) {
	t.Parallel()

	for _, f := range formats() {
		tests := []struct {
			name    string
			content *string
			want    state.LoadStatus
		}{
			{name: "missing file", content: nil, want: state.Missing},
			{name: "zero-byte file", content: ptr(""), want: state.Corrupt},
			{name: "whitespace only", content: ptr(" \n\t\n"), want: state.Corrupt},
			{name: "malformed content", content: ptr(f.malformed), want: state.Corrupt},
			{name: "value that does not fit the type", content: ptr(mismatch(f.name)), want: state.Corrupt},
			{name: "valid content", content: ptr(f.valid), want: state.Loaded},
		}

		for _, tt := range tests {
			t.Run(f.name+"/"+tt.name, func(t *testing.T) {
				t.Parallel()

				path := filepath.Join(t.TempDir(), "state")
				if tt.content != nil {
					writeAt(t, path, *tt.content, 0o600)
				}

				got, status, err := f.load(path)
				assertLoad(t, got, status, err, tt.want)
			})
		}
	}
}

func mismatch(format string) string {
	if format == "json" {
		return `{"name": "alpha", "count": "three"}`
	}

	return "name: alpha\ncount: three\n"
}

func ptr(s string) *string { return &s }

func TestLoadJSONRefusesContentAfterTheValue(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		content string
	}{
		{name: "second value", content: `{"name": "alpha", "count": 3} {"name": "beta"}`},
		{name: "garbage", content: `{"name": "alpha", "count": 3}garbage`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			path := filepath.Join(t.TempDir(), "state.json")
			writeAt(t, path, tt.content, 0o600)

			got, status, err := state.LoadJSON[record](path)
			assertLoad(t, got, status, err, state.Corrupt)
		})
	}
}

func TestLoadYAMLRefusesAnythingButOneDocument(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		content string
	}{
		{name: "comments only", content: "# nothing here\n"},
		{name: "second document", content: "name: alpha\ncount: 3\n---\nname: beta\n"},
		{name: "malformed second document", content: "name: alpha\ncount: 3\n---\n[bad\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			path := filepath.Join(t.TempDir(), "state.yaml")
			writeAt(t, path, tt.content, 0o600)

			got, status, err := state.LoadYAML[record](path)
			assertLoad(t, got, status, err, state.Corrupt)
		})
	}
}

func TestLoadOfADirectoryIsUnreadable(t *testing.T) {
	t.Parallel()

	for _, f := range formats() {
		t.Run(f.name, func(t *testing.T) {
			t.Parallel()

			path := filepath.Join(t.TempDir(), "state")
			if err := os.Mkdir(path, 0o700); err != nil {
				t.Fatal(err)
			}

			got, status, err := f.load(path)
			assertLoad(t, got, status, err, state.Unreadable)
		})
	}
}

// TestLoadOfAFileWithoutReadPermissionIsUnreadable skips where a mode of 0000 does not stop the
// test from reading the file: as root, and on Windows, which has no such mode.
func TestLoadOfAFileWithoutReadPermissionIsUnreadable(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("Windows has no unreadable file mode")
	}

	for _, f := range formats() {
		t.Run(f.name, func(t *testing.T) {
			t.Parallel()

			path := filepath.Join(t.TempDir(), "state")
			writeAt(t, path, f.valid, 0o000)

			if _, err := os.ReadFile(path); err == nil {
				t.Skip("a file at mode 0000 is still readable here")
			}

			got, status, err := f.load(path)
			assertLoad(t, got, status, err, state.Unreadable)

			if !errors.Is(err, fs.ErrPermission) {
				t.Errorf("error %v does not match fs.ErrPermission", err)
			}
		})
	}
}

func TestSaveThenLoadRoundTrips(t *testing.T) {
	t.Parallel()

	for _, f := range formats() {
		t.Run(f.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			path := filepath.Join(dir, "state")

			if err := f.save(path, alpha()); err != nil {
				t.Fatalf("save: %v", err)
			}

			got, status, err := f.load(path)
			assertLoad(t, got, status, err, state.Loaded)
			assertMode(t, path, 0o600)
			assertEntries(t, dir, "state")
		})
	}
}

func TestSaveReplacesAnExistingFileAtMode0600(t *testing.T) {
	t.Parallel()

	for _, f := range formats() {
		t.Run(f.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			path := filepath.Join(dir, "state")
			writeAt(t, path, "old", 0o644)

			if err := f.save(path, alpha()); err != nil {
				t.Fatalf("save: %v", err)
			}

			got, status, err := f.load(path)
			assertLoad(t, got, status, err, state.Loaded)
			assertMode(t, path, 0o600)
			assertEntries(t, dir, "state")
		})
	}
}

func TestSaveJSONWritesIndentedJSONEndingInANewline(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "state.json")

	if err := state.SaveJSON(path, alpha()); err != nil {
		t.Fatalf("SaveJSON: %v", err)
	}

	assertFile(t, path, "{\n  \"name\": \"alpha\",\n  \"count\": 3\n}\n", 0o600)
}

func TestSaveYAMLWritesBlockYAML(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "state.yaml")

	if err := state.SaveYAML(path, alpha()); err != nil {
		t.Fatalf("SaveYAML: %v", err)
	}

	assertFile(t, path, "name: alpha\ncount: 3\n", 0o600)
}

func TestSaveIntoAMissingDirectoryFails(t *testing.T) {
	t.Parallel()

	for _, f := range formats() {
		t.Run(f.name, func(t *testing.T) {
			t.Parallel()

			dir := filepath.Join(t.TempDir(), "absent")
			path := filepath.Join(dir, "state")

			err := f.save(path, alpha())
			if !errors.Is(err, fs.ErrNotExist) {
				t.Fatalf("save = %v, want an error matching fs.ErrNotExist", err)
			}

			if _, err := os.Stat(dir); !errors.Is(err, fs.ErrNotExist) {
				t.Errorf("directory %s exists after a failed save (stat error %v)", dir, err)
			}
		})
	}
}

// unencodable fails to marshal in both formats.
type unencodable struct{}

var errUnencodable = errors.New("cannot encode")

func (unencodable) MarshalJSON() ([]byte, error) { return nil, errUnencodable }

func (unencodable) MarshalYAML() (any, error) { return nil, errUnencodable }

func TestSaveOfAValueThatDoesNotMarshalLeavesTheFileUntouched(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		save func(path string) error
		want error
	}{
		{
			name: "json marshaler error",
			save: func(path string) error { return state.SaveJSON(path, unencodable{}) },
			want: errUnencodable,
		},
		{
			name: "yaml marshaler error",
			save: func(path string) error { return state.SaveYAML(path, unencodable{}) },
			want: errUnencodable,
		},
		{
			name: "json channel",
			save: func(path string) error { return state.SaveJSON(path, make(chan int)) },
		},
		{
			name: "yaml channel",
			save: func(path string) error { return state.SaveYAML(path, make(chan int)) },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			path := filepath.Join(dir, "state")
			writeAt(t, path, "old", 0o644)

			err := tt.save(path)
			if err == nil {
				t.Fatal("save: want an error, got nil")
			}

			if tt.want != nil && !errors.Is(err, tt.want) {
				t.Errorf("save = %v, want an error matching %v", err, tt.want)
			}

			if want := "state: encode " + path + ": "; !strings.HasPrefix(err.Error(), want) {
				t.Errorf("error %q does not start with %q", err, want)
			}

			assertFile(t, path, "old", 0o644)
			assertEntries(t, dir, "state")
		})
	}
}

func TestLoadStatusString(t *testing.T) {
	t.Parallel()

	tests := []struct {
		status state.LoadStatus
		want   string
	}{
		{status: state.Missing, want: "missing"},
		{status: state.Loaded, want: "loaded"},
		{status: state.Corrupt, want: "corrupt"},
		{status: state.Unreadable, want: "unreadable"},
		{status: state.LoadStatus(9), want: "LoadStatus(9)"},
	}

	for _, tt := range tests {
		if got := tt.status.String(); got != tt.want {
			t.Errorf("LoadStatus(%d).String() = %q, want %q", int(tt.status), got, tt.want)
		}
	}
}

func assertMode(t *testing.T, path string, mode os.FileMode) {
	t.Helper()

	if runtime.GOOS == "windows" {
		return
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}

	if perm := info.Mode().Perm(); perm != mode {
		t.Errorf("mode of %s = %o, want %o", path, perm, mode)
	}
}
