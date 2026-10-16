package config

import (
	"errors"
	"io"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

type seedLimits struct {
	Burst  uint16  `doc:"Requests allowed at once."`
	Rate   float32 `doc:"Requests per second."`
	Window time.Duration
	Deep   struct {
		Level int8
	} `doc:"Three levels down."`
}

type seedConfig struct {
	Name     string `doc:"Name of the service."`
	Debug    bool   `doc:"Log every request.\n\nNoisy."`
	Count    int
	Small    int16
	Medium   int32
	Large    int64
	Unsigned uint
	Byte     uint8
	Word     uint32
	Huge     uint64
	Ratio    float64
	Timeout  time.Duration
	Limits   seedLimits `config:"rateLimit" doc:"Throttling of incoming requests."`
	Plain    struct {
		Enabled bool
	}
	Tags      []string
	Empty     []string
	Flags     []bool
	Ints      []int
	Int8s     []int8
	Int64s    []int64
	Uints     []uint
	Uint64s   []uint64
	Floats    []float64
	Float32s  []float32
	Durations []time.Duration
}

func seedDefaults() seedConfig {
	c := seedConfig{
		Name:      "api: v1 # main",
		Debug:     true,
		Count:     -3,
		Small:     math.MinInt16,
		Medium:    math.MaxInt32,
		Large:     math.MinInt64,
		Unsigned:  7,
		Byte:      math.MaxUint8,
		Word:      math.MaxUint32,
		Huge:      math.MaxUint64,
		Ratio:     0.1,
		Timeout:   90 * time.Second,
		Tags:      []string{"a,b", "yes", "", "123"},
		Flags:     []bool{true, false},
		Ints:      []int{1, -2},
		Int8s:     []int8{math.MinInt8},
		Int64s:    []int64{math.MaxInt64},
		Uints:     []uint{0},
		Uint64s:   []uint64{math.MaxUint64},
		Floats:    []float64{math.Inf(1), -1e-300, 2.5},
		Float32s:  []float32{math.MaxFloat32},
		Durations: []time.Duration{time.Millisecond, -time.Hour},
	}
	c.Limits = seedLimits{Burst: 10, Rate: 0.3, Window: 1500 * time.Millisecond}
	c.Limits.Deep.Level = -1
	c.Plain.Enabled = true
	return c
}

// seedPath returns a path for a file that does not exist yet, in a temporary directory.
func seedPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "config.yaml")
}

// readSeed returns the content of the file at path.
func readSeed(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	return string(data)
}

// assertRoundTrip loads the file at path over the zero value of T and checks that every key comes
// from the file with the value it has in defaults.
func assertRoundTrip[T any](t *testing.T, path string, defaults T) {
	t.Helper()
	want, err := Load(defaults)
	if err != nil {
		t.Fatalf("Load(defaults): %v", err)
	}
	var zero T
	got, err := Load(zero, File(path))
	if err != nil {
		t.Fatalf("Load(File): %v\nfile:\n%s", err, readSeed(t, path))
	}
	wantSources, gotSources := want.Sources(), got.Sources()
	if len(gotSources) != len(wantSources) {
		t.Fatalf("got %d sources, want %d", len(gotSources), len(wantSources))
	}
	for i, w := range wantSources {
		g := gotSources[i]
		if g.Key != w.Key || g.Value != w.Value {
			t.Errorf("key %q = %q, want %q = %q\nfile:\n%s", g.Key, g.Value, w.Key, w.Value, readSeed(t, path))
		}
		if !strings.HasPrefix(g.Origin, "config.yaml:") {
			t.Errorf("key %q has origin %q, want the file", g.Key, g.Origin)
		}
	}
}

func TestSeedRoundTrip(t *testing.T) {
	path := seedPath(t)
	created, err := Seed(path, seedDefaults())
	if err != nil || !created {
		t.Fatalf("Seed = %v, %v; want true, nil", created, err)
	}
	assertRoundTrip(t, path, seedDefaults())
}

func TestSeedRoundTripsAwkwardStrings(t *testing.T) {
	type config struct {
		Text  string
		Items []string
	}
	for _, s := range []string{
		"", " ", "yes", "no", "on", "true", "False", "null", "~", "123", "-1", "0x1F", "1e3", ".inf",
		"1m30s", "a: b", "a:b", "# not a comment", "a #b", " leading", "trailing ", "\ttab",
		"'single'", `"double"`, "- dash", "? question", "[list]", "{map}", "*alias", "&anchor",
		"!tag", "@at", "`tick", "%percent", "|", ">", "a,b", "<<", "---", "...",
		"line\nbreak", "trailing newline\n", "two\n\n", "\nleading newline", " indented\nblock",
		"unicode é ✓",
	} {
		path := seedPath(t)
		defaults := config{Text: s, Items: []string{s, s + "x"}}
		if _, err := Seed(path, defaults); err != nil {
			t.Fatalf("Seed(%q): %v", s, err)
		}
		assertRoundTrip(t, path, defaults)
		got, err := Load(config{}, File(path))
		if err != nil {
			t.Fatalf("Load(%q): %v", s, err)
		}
		if v := got.Value(); v.Text != s || len(v.Items) != 2 || v.Items[0] != s {
			t.Errorf("seeded %q, read back %q\nfile:\n%s", s, v, readSeed(t, path))
		}
	}
}

func TestSeedNilSliceIsAnEmptyList(t *testing.T) {
	type config struct {
		Tags []string
		Ints []int
	}
	path := seedPath(t)
	if _, err := Seed(path, config{Ints: []int{}}); err != nil {
		t.Fatalf("Seed: %v", err)
	}
	if got, want := readSeed(t, path), "tags: []\nints: []\n"; got != want {
		t.Errorf("file =\n%s\nwant\n%s", got, want)
	}
	assertRoundTrip(t, path, config{})
}

func TestSeedListLayout(t *testing.T) {
	type config struct {
		Tags  []string
		Empty []string
	}
	path := seedPath(t)
	if _, err := Seed(path, config{Tags: []string{"a", "b"}, Empty: []string{}}); err != nil {
		t.Fatalf("Seed: %v", err)
	}
	if got, want := readSeed(t, path), "tags:\n  - a\n  - b\nempty: []\n"; got != want {
		t.Errorf("file =\n%s\nwant\n%s", got, want)
	}
}

func TestSeedLayout(t *testing.T) {
	path := seedPath(t)
	if _, err := Seed(path, docConfig{Verbose: true}); err != nil {
		t.Fatalf("Seed: %v", err)
	}
	want := `# Print every step.
verbose: true
quiet: false
# The HTTP server.
http_server:
  # Port to listen on.
  port: 0
rate:
  burst: 0
`
	if got := readSeed(t, path); got != want {
		t.Errorf("file =\n%s\nwant\n%s", got, want)
	}
}

func TestSeedComments(t *testing.T) {
	path := seedPath(t)
	if _, err := Seed(path, seedDefaults()); err != nil {
		t.Fatalf("Seed: %v", err)
	}
	got := readSeed(t, path)
	for _, want := range []string{
		"# Name of the service.\nname: ",
		"# Log every request.\n#\n# Noisy.\ndebug: true\n",
		"count: -3\n",
		"# Throttling of incoming requests.\nrate_limit:\n",
		"  # Requests allowed at once.\n  burst: 10\n",
		"  # Requests per second.\n  rate: 0.3\n",
		"  window: 1.5s\n",
		"  # Three levels down.\n  deep:\n    level: -1\n",
		"plain:\n  enabled: true\n",
		"empty: []\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("file does not contain %q:\n%s", want, got)
		}
	}
}

func TestSeedMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows has no Unix permission bits")
	}
	path := seedPath(t)
	if _, err := Seed(path, docConfig{}); err != nil {
		t.Fatalf("Seed: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Errorf("mode = %v, want %v", mode, fs.FileMode(0o600))
	}
}

func TestSeedLeavesAnExistingFile(t *testing.T) {
	const content = "verbose: true\nnot: yaml: at all\n"
	path := writeFile(t, "config.yaml", content)
	created, err := Seed(path, docConfig{})
	if err != nil || created {
		t.Fatalf("Seed = %v, %v; want false, nil", created, err)
	}
	if got := readSeed(t, path); got != content {
		t.Errorf("file = %q, want it unchanged as %q", got, content)
	}
}

func TestSeedLeavesADanglingSymlink(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	target := filepath.Join(dir, "target.yaml")
	if err := os.Symlink(target, path); err != nil {
		t.Skipf("Symlink: %v", err)
	}
	created, err := Seed(path, docConfig{})
	if err != nil || created {
		t.Fatalf("Seed = %v, %v; want false, nil", created, err)
	}
	if _, err := os.Lstat(target); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("Seed wrote through the symlink: Lstat(target) = %v", err)
	}
}

func TestSeedMissingDirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing", "config.yaml")
	created, err := Seed(path, docConfig{})
	if created || err == nil {
		t.Fatalf("Seed = %v, %v; want false and an error", created, err)
	}
	if !errors.Is(err, fs.ErrNotExist) || !strings.Contains(err.Error(), path) {
		t.Errorf("error = %v, want fs.ErrNotExist naming %s", err, path)
	}
}

func TestSeedUnwritableDirectory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows has no Unix permission bits")
	}
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatalf("Chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	path := filepath.Join(dir, "config.yaml")
	if f, err := os.Create(filepath.Join(dir, "probe")); err == nil {
		_ = f.Close()
		t.Skip("the directory is writable anyway, as it is to root")
	}
	created, err := Seed(path, docConfig{})
	if created || err == nil {
		t.Fatalf("Seed = %v, %v; want false and an error", created, err)
	}
	if !errors.Is(err, fs.ErrPermission) || !strings.Contains(err.Error(), path) {
		t.Errorf("error = %v, want fs.ErrPermission naming %s", err, path)
	}
}

func TestSeedSchemaErrors(t *testing.T) {
	path := seedPath(t)
	tests := []struct {
		name string
		seed func() (bool, error)
	}{
		{"pointer", func() (bool, error) { return Seed(path, &docConfig{}) }},
		{"not a struct", func() (bool, error) { return Seed(path, 3) }},
		{"unsupported field", func() (bool, error) {
			return Seed(path, struct{ M map[string]int }{})
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			created, err := tc.seed()
			if created || !errors.Is(err, ErrSchema) {
				t.Errorf("Seed = %v, %v; want false and ErrSchema", created, err)
			}
			if _, err := os.Lstat(path); !errors.Is(err, fs.ErrNotExist) {
				t.Errorf("Seed left a file: Lstat = %v", err)
			}
		})
	}
}

func TestSeedUnencodableValue(t *testing.T) {
	path := seedPath(t)
	created, err := Seed(path, struct{ Name string }{Name: "\xff"})
	if created || err == nil || !strings.Contains(err.Error(), path) {
		t.Fatalf("Seed = %v, %v; want false and an error naming %s", created, err, path)
	}
	if !strings.Contains(err.Error(), "invalid UTF-8") {
		t.Errorf("Seed error = %v, want the encoder's report of the invalid UTF-8", err)
	}
	if _, err := os.Lstat(path); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("Seed left a file: Lstat = %v", err)
	}
}

// failingFile is a created file whose Write or Close fails with a fixed error after doing its work.
type failingFile struct {
	io.WriteCloser
	writeErr error
	closeErr error
}

func (f failingFile) Write(p []byte) (int, error) {
	if f.writeErr != nil {
		return 0, f.writeErr
	}
	return f.WriteCloser.Write(p)
}

func (f failingFile) Close() error {
	if err := f.WriteCloser.Close(); err != nil {
		return err
	}
	return f.closeErr
}

func TestSeedRemovesAFileItCannotFinish(t *testing.T) {
	errWrite := errors.New("disk full")
	errClose := errors.New("flush failed")
	tests := []struct {
		name string
		file failingFile
		want error
	}{
		{"write", failingFile{writeErr: errWrite}, errWrite},
		{"close", failingFile{closeErr: errClose}, errClose},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			path := seedPath(t)
			create := func(path string) (io.WriteCloser, error) {
				f, err := createExclusive(path)
				if err != nil {
					return nil, err
				}
				file := tc.file
				file.WriteCloser = f
				return file, nil
			}
			created, err := writeNew(path, []byte("name: x\n"), create)
			if created || !errors.Is(err, tc.want) || !strings.Contains(err.Error(), path) {
				t.Errorf("writeNew = %v, %v; want false and %v naming %s", created, err, tc.want, path)
			}
			if _, err := os.Lstat(path); !errors.Is(err, fs.ErrNotExist) {
				t.Errorf("writeNew left a file: Lstat = %v", err)
			}
		})
	}
}
