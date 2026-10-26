package update

import (
	"bytes"
	"context"
	"errors"
	"io"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"go.vipengele.org/commons/validate"
)

// rawTemplate is an AssetTemplate without {ext}: it names a raw binary.
const rawTemplate = "{name}_{os}_{arch}"

// newExtractArchive returns a verified Archive holding content, in a fresh temp dir, along with
// that dir. The Archive is closed when the test ends, which does nothing once Extract has closed
// it.
func newExtractArchive(t *testing.T, content []byte) (*Archive, string) {
	t.Helper()

	dir := t.TempDir()

	a, err := createArchive(dir, "atk-update-")
	if err != nil {
		t.Fatalf("createArchive: %v", err)
	}

	t.Cleanup(func() { _ = a.Close() })

	if _, err := a.f.Write(content); err != nil {
		t.Fatalf("write: %v", err)
	}

	if err := a.finish(); err != nil {
		t.Fatalf("finish: %v", err)
	}

	return a, dir
}

// rawConfig is a valid Config that names a raw binary.
func rawConfig(t *testing.T) Config {
	t.Helper()

	c := validConfig(t.TempDir())
	c.AssetTemplate = rawTemplate

	return c
}

// assertArchiveClosed fails the test unless a is closed and its temporary file, the only file of
// archiveDir, is gone.
func assertArchiveClosed(t *testing.T, a *Archive, archiveDir string) {
	t.Helper()

	if !a.closed {
		t.Error("archive is not closed")
	}

	if _, err := os.Stat(a.Name()); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("Stat(archive) = %v, want not exist", err)
	}

	assertDirEmpty(t, archiveDir)
}

// keepAtCleanup keeps s when the test ends, then removes its file.
func keepAtCleanup(t *testing.T, s *Staged) {
	t.Helper()

	t.Cleanup(func() {
		if err := s.Keep(); err != nil {
			t.Errorf("Keep: %v", err)
		}

		_ = os.Remove(s.Name())
	})
}

func TestExtractRaw(t *testing.T) {
	t.Parallel()

	content := bytes.Repeat([]byte("\x00\x01binary\xff"), 10_000)

	tests := []struct {
		name  string
		limit int64
	}{
		{name: "default limit", limit: 0},
		{name: "exactly the limit", limit: int64(len(content))},
		{name: "unbounded", limit: math.MaxInt64},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			a, archiveDir := newExtractArchive(t, content)
			stageDir := t.TempDir()
			c := rawConfig(t)
			c.MaxExtractBytes = tc.limit

			s, err := c.Extract(context.Background(), a, stageDir)
			if err != nil {
				t.Fatalf("Extract: %v", err)
			}

			keepAtCleanup(t, s)
			assertArchiveClosed(t, a, archiveDir)

			if got := filepath.Dir(s.Name()); got != stageDir {
				t.Errorf("Name dir = %q, want %q", got, stageDir)
			}

			if base := filepath.Base(s.Name()); !strings.HasPrefix(base, "atk-update-") {
				t.Errorf("Name = %q, want the prefix %q", base, "atk-update-")
			}

			got, err := os.ReadFile(s.Name())
			if err != nil {
				t.Fatalf("ReadFile: %v", err)
			}

			if !bytes.Equal(got, content) {
				t.Errorf("staged binary holds %d bytes, want the %d of the asset unchanged", len(got), len(content))
			}
		})
	}
}

func TestExtractUsesTempPrefix(t *testing.T) {
	t.Parallel()

	a, _ := newExtractArchive(t, []byte("bin"))
	c := rawConfig(t)
	c.TempPrefix = "custom-"

	s, err := c.Extract(context.Background(), a, t.TempDir())
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}

	keepAtCleanup(t, s)

	if base := filepath.Base(s.Name()); !strings.HasPrefix(base, "custom-") {
		t.Errorf("Name = %q, want the prefix %q", base, "custom-")
	}
}

func TestExtractFails(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		content string
		config  func(Config) Config
		check   func(t *testing.T, err error)
	}{
		{
			name:    "empty raw asset",
			content: "",
			check: func(t *testing.T, err error) {
				t.Helper()

				if !errors.Is(err, ErrEmptyAsset) {
					t.Errorf("err = %v, want ErrEmptyAsset", err)
				}
			},
		},
		{
			name:    "over the limit",
			content: "12345",
			config:  func(c Config) Config { c.MaxExtractBytes = 4; return c },
			check:   wantExtractLimit("atk", 4),
		},
		{
			name:    "over the limit on windows",
			content: "12345",
			config:  func(c Config) Config { c.MaxExtractBytes = 4; c.GOOS = "windows"; return c },
			check:   wantExtractLimit("atk.exe", 4),
		},
		{
			name:    "zip has no extractor",
			content: "archive",
			config:  func(c Config) Config { c.AssetTemplate = "{name}.{ext}"; c.AssetExt = "zip"; return c },
			check:   wantUnsupportedFormat("zip"),
		},
		{
			name:    "invalid config",
			content: "bin",
			config:  func(c Config) Config { c.Name = ""; return c },
			check: func(t *testing.T, err error) {
				t.Helper()

				if !errors.Is(err, validate.ErrEmpty) {
					t.Errorf("err = %v, want validate.ErrEmpty", err)
				}
			},
		},
		{
			name:    "negative limit",
			content: "bin",
			config:  func(c Config) Config { c.MaxExtractBytes = -1; return c },
			check: func(t *testing.T, err error) {
				t.Helper()

				if !errors.Is(err, validate.ErrInvalid) {
					t.Errorf("err = %v, want validate.ErrInvalid", err)
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			a, archiveDir := newExtractArchive(t, []byte(tc.content))
			stageDir := t.TempDir()

			c := rawConfig(t)
			if tc.config != nil {
				c = tc.config(c)
			}

			s, err := c.Extract(context.Background(), a, stageDir)
			if err == nil {
				keepAtCleanup(t, s)
				t.Fatal("Extract succeeded, want an error")
			}

			if s != nil {
				t.Errorf("Staged = %v, want nil", s)
			}

			tc.check(t, err)
			assertArchiveClosed(t, a, archiveDir)
			assertDirEmpty(t, stageDir)
		})
	}
}

func wantExtractLimit(name string, limit int64) func(*testing.T, error) {
	return func(t *testing.T, err error) {
		t.Helper()

		var limitErr *ExtractLimitError
		if !errors.As(err, &limitErr) {
			t.Fatalf("err = %v, want *ExtractLimitError", err)
		}

		if limitErr.Name != name || limitErr.Limit != limit {
			t.Errorf("ExtractLimitError = {%q, %d}, want {%q, %d}", limitErr.Name, limitErr.Limit, name, limit)
		}
	}
}

func wantUnsupportedFormat(ext string) func(*testing.T, error) {
	return func(t *testing.T, err error) {
		t.Helper()

		var formatErr *UnsupportedFormatError
		if !errors.As(err, &formatErr) {
			t.Fatalf("err = %v, want *UnsupportedFormatError", err)
		}

		if formatErr.Ext != ext {
			t.Errorf("Ext = %q, want %q", formatErr.Ext, ext)
		}
	}
}

func TestExtractNilArchive(t *testing.T) {
	t.Parallel()

	s, err := rawConfig(t).Extract(context.Background(), nil, t.TempDir())
	if !errors.Is(err, errNoArchive) || s != nil {
		t.Errorf("Extract(nil) = %v, %v, want nil, errNoArchive", s, err)
	}
}

func TestExtractEmptyDir(t *testing.T) {
	t.Parallel()

	a, archiveDir := newExtractArchive(t, []byte("bin"))

	s, err := rawConfig(t).Extract(context.Background(), a, "")
	if !errors.Is(err, errNoStagingDir) || s != nil {
		t.Errorf("Extract(dir \"\") = %v, %v, want nil, errNoStagingDir", s, err)
	}

	assertArchiveClosed(t, a, archiveDir)
}

func TestExtractMissingDir(t *testing.T) {
	t.Parallel()

	a, archiveDir := newExtractArchive(t, []byte("bin"))
	missing := filepath.Join(t.TempDir(), "missing")

	_, err := rawConfig(t).Extract(context.Background(), a, missing)
	if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("err = %v, want fs.ErrNotExist", err)
	}

	assertArchiveClosed(t, a, archiveDir)

	if _, err := os.Stat(missing); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("Stat(dir) = %v, want the directory not created", err)
	}
}

// countdownContext is a context that is live for the first live calls of Err and cancelled from
// then on. Done is never closed: Extract polls Err.
type countdownContext struct {
	live atomic.Int64
}

func newCountdownContext(live int64) *countdownContext {
	c := &countdownContext{}
	c.live.Store(live)

	return c
}

func (*countdownContext) Deadline() (time.Time, bool) { return time.Time{}, false }

func (*countdownContext) Done() <-chan struct{} { return nil }

func (c *countdownContext) Err() error {
	if c.live.Add(-1) >= 0 {
		return nil
	}

	return context.Canceled
}

func (*countdownContext) Value(any) any { return nil }

func TestExtractCancellation(t *testing.T) {
	t.Parallel()

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()

	tests := []struct {
		name string
		ctx  context.Context
	}{
		{name: "before extracting", ctx: cancelled},
		// The first check precedes creating the file and the second the first read; the third,
		// before the second read, finds ctx cancelled with the file partly written.
		{name: "mid-copy", ctx: newCountdownContext(2)},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			a, archiveDir := newExtractArchive(t, bytes.Repeat([]byte("x"), 256<<10))
			stageDir := t.TempDir()

			s, err := rawConfig(t).Extract(tc.ctx, a, stageDir)
			if !errors.Is(err, context.Canceled) || s != nil {
				t.Errorf("Extract = %v, %v, want nil, context.Canceled", s, err)
			}

			assertArchiveClosed(t, a, archiveDir)
			assertDirEmpty(t, stageDir)
		})
	}
}

func TestExtractJoinsArchiveCloseError(t *testing.T) {
	t.Parallel()

	removeErr := errors.New("remove refused")

	tests := []struct {
		name    string
		content string
		check   func(t *testing.T, err error)
	}{
		{
			name:    "after a failure",
			content: "",
			check: func(t *testing.T, err error) {
				t.Helper()

				if !errors.Is(err, ErrEmptyAsset) {
					t.Errorf("err = %v, want ErrEmptyAsset joined", err)
				}
			},
		},
		{
			name:    "after a success",
			content: "bin",
			check:   func(*testing.T, error) {},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			a, _ := newExtractArchive(t, []byte(tc.content))
			a.remove = func(name string) error { return errors.Join(os.Remove(name), removeErr) }
			stageDir := t.TempDir()

			s, err := rawConfig(t).Extract(context.Background(), a, stageDir)
			if !errors.Is(err, removeErr) || s != nil {
				t.Fatalf("Extract = %v, %v, want nil and the removal error", s, err)
			}

			tc.check(t, err)

			if !a.closed {
				t.Error("archive is not closed")
			}

			assertDirEmpty(t, stageDir)
		})
	}
}

// newStaged returns a Staged over a new file in a fresh temp dir, removed through remove, along
// with that dir.
func newStaged(t *testing.T, remove func(string) error) (*Staged, string) {
	t.Helper()

	dir := t.TempDir()

	f, err := os.CreateTemp(dir, "atk-update-*")
	if err != nil {
		t.Fatalf("CreateTemp: %v", err)
	}

	t.Cleanup(func() { _ = f.Close() })

	return &Staged{f: f, remove: remove}, dir
}

func TestStagedKeep(t *testing.T) {
	t.Parallel()

	s, _ := newStaged(t, os.Remove)

	if err := s.Keep(); err != nil {
		t.Fatalf("Keep: %v", err)
	}

	if err := s.Keep(); err != nil {
		t.Errorf("second Keep = %v, want nil", err)
	}

	if err := s.Discard(); err != nil {
		t.Errorf("Discard after Keep = %v, want nil", err)
	}

	if _, err := os.Stat(s.Name()); err != nil {
		t.Errorf("Stat after Keep and Discard = %v, want the file kept", err)
	}

	// Keep closed the file, so it can be renamed into place on every platform.
	if err := os.Rename(s.Name(), s.Name()+".moved"); err != nil {
		t.Errorf("Rename after Keep: %v", err)
	}
}

func TestStagedDiscard(t *testing.T) {
	t.Parallel()

	s, dir := newStaged(t, os.Remove)

	if err := s.Discard(); err != nil {
		t.Fatalf("Discard: %v", err)
	}

	assertDirEmpty(t, dir)

	if err := s.Discard(); err != nil {
		t.Errorf("second Discard = %v, want nil", err)
	}

	if err := s.Keep(); err != nil {
		t.Errorf("Keep after Discard = %v, want nil", err)
	}
}

func TestStagedDiscardJoinsRemoveError(t *testing.T) {
	t.Parallel()

	removeErr := errors.New("remove refused")
	s, _ := newStaged(t, func(string) error { return removeErr })

	err := s.Discard()
	if !errors.Is(err, removeErr) {
		t.Errorf("Discard = %v, want the removal error", err)
	}

	if err := s.Discard(); err != nil {
		t.Errorf("second Discard = %v, want nil", err)
	}
}

func TestStagedKeepReportsCloseError(t *testing.T) {
	t.Parallel()

	s, _ := newStaged(t, os.Remove)

	if err := s.f.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if err := s.Keep(); !errors.Is(err, os.ErrClosed) {
		t.Errorf("Keep = %v, want os.ErrClosed", err)
	}
}

func TestBinaryFileName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		goos   string
		binary string
		want   string
	}{
		{name: "linux", goos: "linux", want: "atk"},
		{name: "darwin", goos: "darwin", want: "atk"},
		{name: "windows", goos: "windows", want: "atk.exe"},
		{name: "windows with a binary name", goos: "windows", binary: "tool", want: "tool.exe"},
		{name: "binary name", goos: "linux", binary: "tool", want: "tool"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			c := validConfig(t.TempDir())
			c.GOOS = tc.goos
			c.BinaryName = tc.binary

			r, err := c.resolve()
			if err != nil {
				t.Fatalf("resolve: %v", err)
			}

			if got := binaryFileName(r); got != tc.want {
				t.Errorf("binaryFileName = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestUnsafeEntryName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		entry  string
		unsafe bool
	}{
		{name: "plain", entry: "atk"},
		{name: "nested", entry: "atk_linux_amd64/atk"},
		{name: "dot prefix", entry: "./atk"},
		{name: "directory", entry: "bin/"},
		{name: "dots inside a name", entry: "a..b/atk..exe"},
		{name: "colon past the drive position", entry: "ab:c"},
		{name: "empty", entry: "", unsafe: true},
		{name: "absolute", entry: "/etc/passwd", unsafe: true},
		{name: "parent", entry: "../atk", unsafe: true},
		{name: "parent inside", entry: "bin/../../atk", unsafe: true},
		{name: "parent at the end", entry: "bin/..", unsafe: true},
		{name: "only parent", entry: "..", unsafe: true},
		{name: "backslash", entry: `bin\atk`, unsafe: true},
		{name: "backslash parent", entry: `..\atk`, unsafe: true},
		{name: "drive with backslash", entry: `C:\atk`, unsafe: true},
		{name: "drive relative", entry: "C:atk", unsafe: true},
		{name: "lower-case drive", entry: "c:/atk", unsafe: true},
		{name: "NUL byte", entry: "atk\x00.txt", unsafe: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			reason, unsafe := unsafeEntryName(tc.entry)
			if unsafe != tc.unsafe {
				t.Errorf("unsafeEntryName(%q) = %q, %v, want unsafe %v", tc.entry, reason, unsafe, tc.unsafe)
			}

			if unsafe == (reason == "") {
				t.Errorf("unsafeEntryName(%q) reason = %q, want one exactly when unsafe", tc.entry, reason)
			}
		})
	}
}

func TestCopyBounded(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		src     string
		limit   int64
		wantErr bool
	}{
		{name: "empty", src: "", limit: 0},
		{name: "under", src: "abc", limit: 4},
		{name: "exactly", src: "abcd", limit: 4},
		{name: "over", src: "abcde", limit: 4, wantErr: true},
		{name: "zero limit", src: "a", limit: 0, wantErr: true},
		{name: "unbounded", src: "abcde", limit: math.MaxInt64},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var dst bytes.Buffer

			err := copyBounded(&dst, strings.NewReader(tc.src), "atk", tc.limit)
			if !tc.wantErr {
				if err != nil {
					t.Fatalf("copyBounded: %v", err)
				}

				if dst.String() != tc.src {
					t.Errorf("copied %q, want %q", dst.String(), tc.src)
				}

				return
			}

			wantExtractLimit("atk", tc.limit)(t, err)
		})
	}
}

func TestCopyBoundedWrapsReadError(t *testing.T) {
	t.Parallel()

	readErr := errors.New("read failed")

	err := copyBounded(io.Discard, io.MultiReader(strings.NewReader("ab"), errReader{readErr}), "atk", 10)
	if !errors.Is(err, readErr) || !strings.Contains(err.Error(), `"atk"`) {
		t.Errorf("copyBounded = %v, want the read error, naming the entry", err)
	}
}

// errReader fails every Read with err.
type errReader struct{ err error }

func (r errReader) Read([]byte) (int, error) { return 0, r.err }

func TestCtxReader(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	r := &ctxReader{ctx: ctx, r: strings.NewReader("abcdef")}

	buf := make([]byte, 3)
	if n, err := r.Read(buf); n != 3 || err != nil {
		t.Fatalf("Read = %d, %v, want 3, nil", n, err)
	}

	cancel()

	if n, err := r.Read(buf); n != 0 || !errors.Is(err, context.Canceled) {
		t.Errorf("Read after cancel = %d, %v, want 0, context.Canceled", n, err)
	}
}

func TestExtractErrors(t *testing.T) {
	t.Parallel()

	cause := errors.New("insecure path")

	tests := []struct {
		name string
		err  error
		want string
	}{
		{
			name: "limit",
			err:  &ExtractLimitError{Name: "atk", Limit: 4},
			want: `update: extracting "atk": exceeds the limit of 4 bytes`,
		},
		{
			name: "unsafe entry",
			err:  &UnsafeEntryError{Name: "../atk", Reason: `contains a ".." component`},
			want: `update: unsafe archive entry "../atk": contains a ".." component`,
		},
		{
			name: "binary not found",
			err:  &BinaryNotFoundError{Name: "atk"},
			want: `update: archive has no file named "atk"`,
		},
		{
			name: "multiple binaries",
			err:  &MultipleBinariesError{Name: "atk", Entries: []string{"a/atk", "b/atk"}},
			want: `update: archive has 2 files named "atk": ["a/atk" "b/atk"]`,
		},
		{
			name: "unsupported format",
			err:  &UnsupportedFormatError{Ext: "zip"},
			want: `update: no extractor for archive format "zip"`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := tc.err.Error(); got != tc.want {
				t.Errorf("Error() = %q, want %q", got, tc.want)
			}
		})
	}

	unsafeErr := error(&UnsafeEntryError{Name: "x", Reason: "insecure path", Err: cause})
	if !errors.Is(unsafeErr, cause) {
		t.Error("UnsafeEntryError does not unwrap to its cause")
	}

	sentinels := []error{ErrEmptyAsset, ErrNotGzip, ErrTruncatedGzip}
	for i, a := range sentinels {
		for j, b := range sentinels {
			if (i == j) != errors.Is(a, b) {
				t.Errorf("errors.Is(%v, %v) = %v", a, b, i != j)
			}
		}
	}
}
