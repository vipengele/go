package update

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"go.vipengele.org/cli/spec"
	"go.vipengele.org/cli/spec/paths"
	"go.vipengele.org/commons/validate"
	"go.vipengele.org/filelock"
	"go.vipengele.org/update/asset"
	"go.vipengele.org/update/internal/minisign"
	"go.vipengele.org/update/internal/minisigntest"
)

// stubSource is a Source that reports no release.
type stubSource struct{}

func (*stubSource) Latest(context.Context, Channel) (Release, error) {
	return Release{}, errors.New("stubSource: no release")
}

func validIdentity() spec.Identity {
	return spec.Identity{
		Name:      "atk",
		Owner:     "pedromvgomes",
		Repo:      "agentic-toolkit",
		EnvPrefix: "ATK",
		DirName:   "agentic-toolkit",
	}
}

// testKey and testKey2 return minisign public key lines that parse and carry different key ids.
func testKey() string  { return seededKeyLine(1) }
func testKey2() string { return seededKeyLine(2) }

// seededKeyLine returns the key line of the minisign key GenerateKey draws from a stream of seed
// bytes, the same line on every call. The stream is longer than the key id and Ed25519 seed
// GenerateKey reads, so it never runs dry.
func seededKeyLine(seed byte) string {
	key, err := minisigntest.GenerateKey(bytes.NewReader(bytes.Repeat([]byte{seed}, 64)))
	if err != nil {
		panic(err)
	}
	return key.PublicKeyLine()
}

// validConfig is a Config with every required field set and every optional field at its zero
// value, except the two seams the default StateDir is resolved through: Getenv reports every
// variable unset and UserHomeDir returns home, so that no test reads the host's environment or
// home directory.
func validConfig(home string) Config {
	return Config{
		Identity:    validIdentity(),
		TrustedKeys: []string{testKey()},
		Source:      &stubSource{},
		Getenv:      envOf(nil),
		UserHomeDir: func() (string, error) { return home, nil },
	}
}

// envOf returns a Getenv that reads vars, and "" for a variable vars does not hold.
func envOf(vars map[string]string) func(string) string {
	return func(name string) string { return vars[name] }
}

// plainFields is the comparable part of a Config: every field that is not a func.
type plainFields struct {
	Identity       spec.Identity
	BinaryName     string
	TempPrefix     string
	AssetTemplate  string
	AssetExt       string
	ChecksumsAsset string
	StateDir       string
	Source         Source
	HTTPClient     *http.Client
	GOOS           string
	GOARCH         string
}

func plainFieldsOf(c Config) plainFields {
	return plainFields{
		Identity:       c.Identity,
		BinaryName:     c.BinaryName,
		TempPrefix:     c.TempPrefix,
		AssetTemplate:  c.AssetTemplate,
		AssetExt:       c.AssetExt,
		ChecksumsAsset: c.ChecksumsAsset,
		StateDir:       c.StateDir,
		Source:         c.Source,
		HTTPClient:     c.HTTPClient,
		GOOS:           c.GOOS,
		GOARCH:         c.GOARCH,
	}
}

func TestResolveDefaultsEveryZeroField(t *testing.T) {
	const envName = "VIPENGELE_UPDATE_TEST_DEFAULT_GETENV"
	t.Setenv(envName, "from-the-environment")
	home := filepath.Join(t.TempDir(), "atk-home")
	t.Setenv("ATK_HOME", home)

	before := time.Now()
	r, err := Config{Identity: validIdentity(), TrustedKeys: []string{testKey()}, Source: &stubSource{}}.resolve()
	if err != nil {
		t.Fatalf("resolve() error = %v, want nil", err)
	}

	if r.HTTPClient == nil {
		t.Error("HTTPClient = nil, want a client")
	}
	if r.HTTPClient == http.DefaultClient {
		t.Error("HTTPClient = http.DefaultClient, want a client of the Config's own")
	}
	if r.Now == nil {
		t.Fatal("Now = nil, want time.Now")
	}
	if now := r.Now(); now.Before(before) || now.After(time.Now()) {
		t.Errorf("Now() = %v, want the current time", now)
	}
	if r.GOOS != runtime.GOOS {
		t.Errorf("GOOS = %q, want %q", r.GOOS, runtime.GOOS)
	}
	if r.GOARCH != runtime.GOARCH {
		t.Errorf("GOARCH = %q, want %q", r.GOARCH, runtime.GOARCH)
	}
	if r.Executable == nil {
		t.Error("Executable = nil, want os.Executable")
	} else if path, err := r.Executable(); err != nil || path == "" {
		t.Errorf("Executable() = %q, %v, want the test binary's path", path, err)
	}
	if r.Getenv == nil {
		t.Fatal("Getenv = nil, want os.Getenv")
	}
	if got := r.Getenv(envName); got != "from-the-environment" {
		t.Errorf("Getenv(%q) = %q, want %q", envName, got, "from-the-environment")
	}
	if r.UserHomeDir == nil {
		t.Fatal("UserHomeDir = nil, want os.UserHomeDir")
	}
	wantHome, wantErr := os.UserHomeDir()
	if got, err := r.UserHomeDir(); got != wantHome || (err == nil) != (wantErr == nil) {
		t.Errorf("UserHomeDir() = %q, %v, want %q, %v", got, err, wantHome, wantErr)
	}
	if r.BinaryName != "atk" {
		t.Errorf("BinaryName = %q, want Identity.Name %q", r.BinaryName, "atk")
	}
	if r.TempPrefix != "atk-update-" {
		t.Errorf("TempPrefix = %q, want %q", r.TempPrefix, "atk-update-")
	}
	if r.AssetTemplate != asset.DefaultTemplate {
		t.Errorf("AssetTemplate = %q, want %q", r.AssetTemplate, asset.DefaultTemplate)
	}
	if r.AssetExt != asset.DefaultExt {
		t.Errorf("AssetExt = %q, want %q", r.AssetExt, asset.DefaultExt)
	}
	if r.ChecksumsAsset != asset.DefaultChecksumsTemplate {
		t.Errorf("ChecksumsAsset = %q, want %q", r.ChecksumsAsset, asset.DefaultChecksumsTemplate)
	}
	if want := filepath.Join(home, "state"); r.StateDir != want {
		t.Errorf("StateDir = %q, want %q", r.StateDir, want)
	}
	if want := os.TempDir(); r.TempDir != want {
		t.Errorf("TempDir = %q, want %q", r.TempDir, want)
	}
	if want := int64(256 << 20); r.MaxDownloadBytes != want {
		t.Errorf("MaxDownloadBytes = %d, want %d", r.MaxDownloadBytes, want)
	}
	if want := int64(512 << 20); r.MaxExtractBytes != want {
		t.Errorf("MaxExtractBytes = %d, want %d", r.MaxExtractBytes, want)
	}
	if want := 5 * time.Minute; r.DownloadTimeout != want {
		t.Errorf("DownloadTimeout = %v, want %v", r.DownloadTimeout, want)
	}
	if want := 10 * time.Second; r.PreflightTimeout != want {
		t.Errorf("PreflightTimeout = %v, want %v", r.PreflightTimeout, want)
	}
	if r.EvalSymlinks == nil {
		t.Fatal("EvalSymlinks = nil, want filepath.EvalSymlinks")
	}
	dir := t.TempDir()
	wantDir, wantErr := filepath.EvalSymlinks(dir)
	if got, err := r.EvalSymlinks(dir); got != wantDir || (err == nil) != (wantErr == nil) {
		t.Errorf("EvalSymlinks(%q) = %q, %v, want %q, %v", dir, got, err, wantDir, wantErr)
	}
	if _, err := r.EvalSymlinks(filepath.Join(dir, "missing")); err == nil {
		t.Error("EvalSymlinks(missing path) error = nil, want the filesystem's error")
	}
	if r.InContainer == nil {
		t.Error("InContainer = nil, want paths.InContainer")
	}
	if r.Detectors != nil {
		t.Errorf("Detectors = %d entries, want nil", len(r.Detectors))
	}
	if r.CheckInterval != 24*time.Hour {
		t.Errorf("CheckInterval = %v, want %v", r.CheckInterval, 24*time.Hour)
	}
	if r.IsTerminal == nil {
		t.Error("IsTerminal = nil, want the stderr terminal probe")
	} else {
		// The default probes the real stderr, so its value depends on how the tests run.
		_ = r.IsTerminal()
	}
	fs, ok := r.StateStore.(*fileStore)
	if !ok {
		t.Fatalf("StateStore = %T, want *fileStore", r.StateStore)
	}
	if want := filepath.Join(r.StateDir, "update-state.json"); fs.path() != want {
		t.Errorf("StateStore path = %q, want %q", fs.path(), want)
	}
	if r.Version != "" || r.DisableCheck {
		t.Errorf("Version = %q, DisableCheck = %v, want the zero values kept", r.Version, r.DisableCheck)
	}
}

// memStore is a StateStore that keeps nothing.
type memStore struct{}

func (memStore) Load(context.Context) (State, error) { return State{}, nil }
func (memStore) Save(context.Context, State) error   { return nil }

func TestResolveKeepsTheCheckFields(t *testing.T) {
	t.Parallel()
	store := &memStore{}
	c := validConfig(t.TempDir())
	c.Version = "v1.2.3"
	c.DisableCheck = true
	c.CheckInterval = 2 * time.Hour
	c.StateStore = store
	c.IsTerminal = func() bool { return true }

	r, err := c.resolve()
	if err != nil {
		t.Fatalf("resolve() error = %v, want nil", err)
	}
	if r.Version != "v1.2.3" {
		t.Errorf("Version = %q, want %q", r.Version, "v1.2.3")
	}
	if !r.DisableCheck {
		t.Error("DisableCheck = false, want true")
	}
	if r.CheckInterval != 2*time.Hour {
		t.Errorf("CheckInterval = %v, want %v", r.CheckInterval, 2*time.Hour)
	}
	if r.StateStore != StateStore(store) {
		t.Errorf("StateStore = %v, want the supplied store", r.StateStore)
	}
	if !r.IsTerminal() {
		t.Error("IsTerminal() = false, want the Config's true")
	}
}

func TestValidateChecksCheckInterval(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		interval time.Duration
		wantErr  bool
	}{
		{"zero", 0, false},
		{"exactly one hour", time.Hour, false},
		{"two hours", 2 * time.Hour, false},
		{"negative", -time.Second, true},
		{"under one hour", 59 * time.Minute, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c := validConfig("")
			c.Name = ""
			c.CheckInterval = tc.interval
			err := c.Validate()
			if !errors.Is(err, validate.ErrEmpty) {
				t.Errorf("Validate() = %v, want the other problem reported too", err)
			}
			var fe *validate.FieldError
			found := false
			for errs := []error{err}; len(errs) > 0 && !found; {
				e := errs[0]
				errs = errs[1:]
				if j, ok := e.(interface{ Unwrap() []error }); ok {
					errs = append(errs, j.Unwrap()...)
					continue
				}
				if errors.As(e, &fe) && fe.Field == "CheckInterval" {
					found = true
				}
			}
			if found != tc.wantErr {
				t.Errorf("Validate() = %v, CheckInterval FieldError: %v, want %v", err, found, tc.wantErr)
			}
			if tc.wantErr && !errors.Is(err, validate.ErrInvalid) {
				t.Errorf("Validate() = %v, want one wrapping %v", err, validate.ErrInvalid)
			}
		})
	}
}

func TestResolveDefaultInContainerFollowsTheSeams(t *testing.T) {
	t.Parallel()
	kubernetes := map[string]string{"KUBERNETES_SERVICE_HOST": "10.0.0.1"}
	tests := []struct {
		name string
		goos string
		env  map[string]string
		want bool
	}{
		{"linux with a container variable", "linux", kubernetes, true},
		{"linux with a podman container variable", "linux", map[string]string{"container": "podman"}, true},
		{"darwin with a container variable", "darwin", kubernetes, false},
		{"windows with a container variable", "windows", kubernetes, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c := validConfig(t.TempDir())
			c.GOOS = tc.goos
			c.Getenv = envOf(tc.env)

			r, err := c.resolve()
			if err != nil {
				t.Fatalf("resolve() error = %v, want nil", err)
			}
			if got := r.InContainer(); got != tc.want {
				t.Errorf("InContainer() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestResolveDefaultStateDirReadsTheProcessEnvironment(t *testing.T) {
	if runtime.GOOS == "darwin" || runtime.GOOS == "windows" {
		t.Skipf("the XDG layout does not serve %s", runtime.GOOS)
	}
	home := filepath.Join(t.TempDir(), "home")
	stateHome := filepath.Join(t.TempDir(), "state-home")
	t.Setenv("ATK_HOME", "")
	t.Setenv("HOME", home)

	tests := []struct {
		name      string
		stateHome string
		want      string
	}{
		{"XDG_STATE_HOME set", stateHome, filepath.Join(stateHome, "agentic-toolkit")},
		{"XDG_STATE_HOME unset", "", filepath.Join(home, ".local", "state", "agentic-toolkit")},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("XDG_STATE_HOME", tc.stateHome)

			r, err := Config{Identity: validIdentity(), TrustedKeys: []string{testKey()}, Source: &stubSource{}}.resolve()
			if err != nil {
				t.Fatalf("resolve() error = %v, want nil", err)
			}
			if r.StateDir != tc.want {
				t.Errorf("StateDir = %q, want %q", r.StateDir, tc.want)
			}
		})
	}
}

func TestResolveDerivesTempPrefixFromBinaryName(t *testing.T) {
	t.Parallel()
	c := validConfig(t.TempDir())
	c.BinaryName = "toolkit"

	r, err := c.resolve()
	if err != nil {
		t.Fatalf("resolve() error = %v, want nil", err)
	}
	if r.TempPrefix != "toolkit-update-" {
		t.Errorf("TempPrefix = %q, want %q", r.TempPrefix, "toolkit-update-")
	}
}

func TestResolveHonoursEveryOverride(t *testing.T) {
	t.Parallel()
	fixed := time.Date(2026, time.March, 4, 5, 6, 7, 0, time.UTC)
	client := &http.Client{}
	home := filepath.Join(t.TempDir(), "elsewhere")
	stateHome := filepath.Join(t.TempDir(), "state-home")

	c := validConfig("")
	c.BinaryName = "toolkit"
	c.TempPrefix = "tk-tmp-"
	c.TempDir = filepath.Join(t.TempDir(), "scratch")
	c.MaxDownloadBytes = 1024
	c.MaxExtractBytes = 2048
	c.DownloadTimeout = 7 * time.Second
	c.PreflightTimeout = 3 * time.Second
	c.AssetTemplate = "{name}-{os}-{arch}.{ext}"
	c.AssetExt = "zip"
	c.ChecksumsAsset = "SHA256SUMS"
	c.HTTPClient = client
	c.Now = func() time.Time { return fixed }
	c.GOOS = "plan9"
	c.GOARCH = "mips64"
	c.Executable = func() (string, error) { return "/opt/toolkit/bin/toolkit", nil }
	c.Getenv = envOf(map[string]string{"XDG_STATE_HOME": stateHome})
	c.UserHomeDir = func() (string, error) { return home, nil }
	c.EvalSymlinks = func(path string) (string, error) { return "/resolved" + path, nil }
	c.InContainer = func() bool { return true }
	c.Detectors = []Detector{
		func(string) (Install, bool) { return Install{Manager: "first"}, true },
		func(string) (Install, bool) { return Install{Manager: "second"}, true },
	}

	r, err := c.resolve()
	if err != nil {
		t.Fatalf("resolve() error = %v, want nil", err)
	}

	if r.BinaryName != "toolkit" {
		t.Errorf("BinaryName = %q, want %q", r.BinaryName, "toolkit")
	}
	if r.TempPrefix != "tk-tmp-" {
		t.Errorf("TempPrefix = %q, want %q", r.TempPrefix, "tk-tmp-")
	}
	if r.TempDir != c.TempDir {
		t.Errorf("TempDir = %q, want %q", r.TempDir, c.TempDir)
	}
	if r.MaxDownloadBytes != 1024 {
		t.Errorf("MaxDownloadBytes = %d, want %d", r.MaxDownloadBytes, 1024)
	}
	if r.MaxExtractBytes != 2048 {
		t.Errorf("MaxExtractBytes = %d, want %d", r.MaxExtractBytes, 2048)
	}
	if r.DownloadTimeout != 7*time.Second {
		t.Errorf("DownloadTimeout = %v, want %v", r.DownloadTimeout, 7*time.Second)
	}
	if r.PreflightTimeout != 3*time.Second {
		t.Errorf("PreflightTimeout = %v, want %v", r.PreflightTimeout, 3*time.Second)
	}
	if r.AssetTemplate != "{name}-{os}-{arch}.{ext}" {
		t.Errorf("AssetTemplate = %q, want %q", r.AssetTemplate, "{name}-{os}-{arch}.{ext}")
	}
	if r.AssetExt != "zip" {
		t.Errorf("AssetExt = %q, want %q", r.AssetExt, "zip")
	}
	if r.ChecksumsAsset != "SHA256SUMS" {
		t.Errorf("ChecksumsAsset = %q, want %q", r.ChecksumsAsset, "SHA256SUMS")
	}
	if r.HTTPClient != client {
		t.Errorf("HTTPClient = %p, want the Config's client %p", r.HTTPClient, client)
	}
	if got := r.Now(); !got.Equal(fixed) {
		t.Errorf("Now() = %v, want %v", got, fixed)
	}
	if r.GOOS != "plan9" {
		t.Errorf("GOOS = %q, want %q", r.GOOS, "plan9")
	}
	if r.GOARCH != "mips64" {
		t.Errorf("GOARCH = %q, want %q", r.GOARCH, "mips64")
	}
	if path, err := r.Executable(); err != nil || path != "/opt/toolkit/bin/toolkit" {
		t.Errorf("Executable() = %q, %v, want %q, nil", path, err, "/opt/toolkit/bin/toolkit")
	}
	if got := r.Getenv("XDG_STATE_HOME"); got != stateHome {
		t.Errorf("Getenv(%q) = %q, want %q", "XDG_STATE_HOME", got, stateHome)
	}
	if dir, err := r.UserHomeDir(); err != nil || dir != home {
		t.Errorf("UserHomeDir() = %q, %v, want %q, nil", dir, err, home)
	}
	if want := filepath.Join(stateHome, "agentic-toolkit"); r.StateDir != want {
		t.Errorf("StateDir = %q, want %q", r.StateDir, want)
	}
	if got, err := r.EvalSymlinks("/bin/toolkit"); err != nil || got != "/resolved/bin/toolkit" {
		t.Errorf("EvalSymlinks(%q) = %q, %v, want %q, nil", "/bin/toolkit", got, err, "/resolved/bin/toolkit")
	}
	if !r.InContainer() {
		t.Error("InContainer() = false, want the Config's true")
	}
	if len(r.Detectors) != 2 {
		t.Fatalf("Detectors = %d entries, want 2", len(r.Detectors))
	}
	for i, want := range []string{"first", "second"} {
		if got, ok := r.Detectors[i](""); !ok || got.Manager != want {
			t.Errorf("Detectors[%d]() = %+v, %v, want Manager %q, true", i, got, ok, want)
		}
	}
}

func TestResolveDefaultStateDirFollowsTheSeams(t *testing.T) {
	t.Parallel()
	home := filepath.Join(t.TempDir(), "home")
	elsewhere := filepath.Join(t.TempDir(), "elsewhere")
	const app = "agentic-toolkit"

	tests := []struct {
		name string
		goos string
		env  map[string]string
		want string
	}{
		{"linux", "linux", nil, filepath.Join(home, ".local", "state", app)},
		{
			"linux with XDG_STATE_HOME", "linux",
			map[string]string{"XDG_STATE_HOME": elsewhere},
			filepath.Join(elsewhere, app),
		},
		{"darwin", "darwin", nil, filepath.Join(home, "Library", "Application Support", app, "state")},
		{
			"darwin ignores XDG_STATE_HOME", "darwin",
			map[string]string{"XDG_STATE_HOME": elsewhere},
			filepath.Join(home, "Library", "Application Support", app, "state"),
		},
		{"windows", "windows", nil, filepath.Join(home, "AppData", "Local", app, "state")},
		{
			"windows with LocalAppData", "windows",
			map[string]string{"LocalAppData": elsewhere},
			filepath.Join(elsewhere, app, "state"),
		},
		{
			"ATK_HOME on linux", "linux",
			map[string]string{"ATK_HOME": elsewhere, "XDG_STATE_HOME": home},
			filepath.Join(elsewhere, "state"),
		},
		{
			"ATK_HOME on darwin", "darwin",
			map[string]string{"ATK_HOME": elsewhere},
			filepath.Join(elsewhere, "state"),
		},
		{
			"ATK_HOME on windows", "windows",
			map[string]string{"ATK_HOME": elsewhere},
			filepath.Join(elsewhere, "state"),
		},
		{
			"ATK_HOME set to the empty string", "linux",
			map[string]string{"ATK_HOME": ""},
			filepath.Join(home, ".local", "state", app),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c := validConfig(home)
			c.GOOS = tc.goos
			c.Getenv = envOf(tc.env)

			r, err := c.resolve()
			if err != nil {
				t.Fatalf("resolve() error = %v, want nil", err)
			}
			if r.StateDir != tc.want {
				t.Errorf("StateDir = %q, want %q", r.StateDir, tc.want)
			}
		})
	}
}

func TestResolveKeepsExplicitStateDir(t *testing.T) {
	t.Parallel()
	stateDir := filepath.Join(t.TempDir(), "state")
	c := validConfig("")
	c.StateDir = stateDir
	calls := 0
	c.UserHomeDir = func() (string, error) {
		calls++
		return "", errors.New("UserHomeDir called")
	}

	r, err := c.resolve()
	if err != nil {
		t.Fatalf("resolve() error = %v, want nil", err)
	}
	if r.StateDir != stateDir {
		t.Errorf("StateDir = %q, want %q", r.StateDir, stateDir)
	}
	if calls != 0 {
		t.Errorf("UserHomeDir called %d times, want an explicit StateDir used without the home directory", calls)
	}
}

func TestResolveDefaultStateDirNeedsNoHomeDirWhenTheStateDirectoryIsSet(t *testing.T) {
	t.Parallel()
	stateHome := filepath.Join(t.TempDir(), "state-home")
	tests := []struct {
		name string
		goos string
		env  map[string]string
		want string
	}{
		{"linux with XDG_STATE_HOME", "linux", map[string]string{"XDG_STATE_HOME": stateHome},
			filepath.Join(stateHome, "agentic-toolkit")},
		{"windows with LocalAppData", "windows", map[string]string{"LocalAppData": stateHome},
			filepath.Join(stateHome, "agentic-toolkit", "state")},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c := validConfig("")
			c.GOOS = tc.goos
			c.Getenv = envOf(tc.env)
			calls := 0
			c.UserHomeDir = func() (string, error) {
				calls++
				return "", errors.New("no home directory")
			}

			r, err := c.resolve()
			if err != nil {
				t.Fatalf("resolve() error = %v, want nil", err)
			}
			if r.StateDir != tc.want {
				t.Errorf("StateDir = %q, want %q", r.StateDir, tc.want)
			}
			if calls != 0 {
				t.Errorf("UserHomeDir called %d times, want the state directory resolved without it", calls)
			}
		})
	}
}

func TestResolveReportsHomeDirError(t *testing.T) {
	t.Parallel()
	errNoHome := errors.New("no home directory")
	tests := []struct {
		name        string
		userHomeDir func() (string, error)
		want        []error
	}{
		{"UserHomeDir fails", func() (string, error) { return "", errNoHome }, []error{errNoHome, paths.ErrNoHomeDir}},
		{"UserHomeDir returns empty", func() (string, error) { return "", nil }, []error{paths.ErrNoHomeDir}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c := validConfig("")
			c.GOOS = "linux"
			c.UserHomeDir = tc.userHomeDir

			r, err := c.resolve()
			for _, want := range tc.want {
				if !errors.Is(err, want) {
					t.Errorf("resolve() error = %v, want one wrapping %v", err, want)
				}
			}
			if got := plainFieldsOf(r); got != (plainFields{}) {
				t.Errorf("resolve() Config = %+v, want the zero Config", got)
			}
			if r.Now != nil || r.Executable != nil || r.Getenv != nil || r.UserHomeDir != nil {
				t.Error("resolve() Config has func fields set, want the zero Config")
			}
		})
	}
}

func TestResolveDoesNotModifyItsReceiver(t *testing.T) {
	t.Parallel()
	c := validConfig(t.TempDir())
	before := plainFieldsOf(c)

	if _, err := c.resolve(); err != nil {
		t.Fatalf("resolve() error = %v, want nil", err)
	}
	if after := plainFieldsOf(c); after != before {
		t.Errorf("Config after resolve = %+v, want %+v", after, before)
	}
	if c.Now != nil || c.Executable != nil || c.EvalSymlinks != nil || c.InContainer != nil ||
		c.IsTerminal != nil || c.StateStore != nil || c.CheckInterval != 0 || c.Token != nil {
		t.Error("resolve set a func field on its receiver, want the receiver's nil func fields left nil")
	}
}

func TestResolveDefaultsSourceToTheGitHubSource(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		client *http.Client
	}{
		{"default client", nil},
		{"supplied client", &http.Client{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c := validConfig(t.TempDir())
			c.Source = nil
			c.HTTPClient = tc.client

			r, err := c.resolve()
			if err != nil {
				t.Fatalf("resolve() error = %v, want nil", err)
			}
			gh, ok := r.Source.(*GitHubSource)
			if !ok {
				t.Fatalf("Source = %T, want *GitHubSource", r.Source)
			}
			if gh.Owner != "pedromvgomes" || gh.Repo != "agentic-toolkit" {
				t.Errorf("Source = %s/%s, want the Identity's pedromvgomes/agentic-toolkit", gh.Owner, gh.Repo)
			}
			if gh.HTTPClient == nil || gh.HTTPClient != r.HTTPClient {
				t.Errorf("Source.HTTPClient = %p, want the resolved HTTPClient %p", gh.HTTPClient, r.HTTPClient)
			}
			if tc.client != nil && gh.HTTPClient != tc.client {
				t.Errorf("Source.HTTPClient = %p, want the Config's client %p", gh.HTTPClient, tc.client)
			}
		})
	}
}

func TestResolveKeepsASuppliedSource(t *testing.T) {
	t.Parallel()
	src := &stubSource{}
	c := validConfig(t.TempDir())
	c.Source = src

	r, err := c.resolve()
	if err != nil {
		t.Fatalf("resolve() error = %v, want nil", err)
	}
	if r.Source != Source(src) {
		t.Errorf("Source = %v, want the supplied source", r.Source)
	}
}

func TestResolveDoesNotSetTheSourceOnItsReceiver(t *testing.T) {
	t.Parallel()
	c := validConfig(t.TempDir())
	c.Source = nil

	if _, err := c.resolve(); err != nil {
		t.Fatalf("resolve() error = %v, want nil", err)
	}
	if c.Source != nil || c.HTTPClient != nil {
		t.Error("resolve set Source or HTTPClient on its receiver, want them left nil")
	}
}

func TestResolveRejectsInvalidConfig(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		set  func(*Config)
		want error
	}{
		{"empty Name", func(c *Config) { c.Name = "" }, validate.ErrEmpty},
		{"invalid DirName", func(c *Config) { c.DirName = ".." }, validate.ErrInvalid},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c := validConfig("")
			c.UserHomeDir = func() (string, error) {
				t.Error("UserHomeDir called, want an invalid Config rejected before any default")
				return "", errors.New("UserHomeDir called")
			}
			c.BinaryName = "toolkit"
			tc.set(&c)
			before := plainFieldsOf(c)

			r, err := c.resolve()
			if !errors.Is(err, tc.want) {
				t.Errorf("resolve() error = %v, want one wrapping %v", err, tc.want)
			}
			if got := plainFieldsOf(r); got != (plainFields{}) {
				t.Errorf("resolve() Config = %+v, want the zero Config", got)
			}
			if r.Now != nil || r.Executable != nil || r.Getenv != nil || r.UserHomeDir != nil {
				t.Error("resolve() Config has func fields set, want the zero Config")
			}
			if after := plainFieldsOf(c); after != before {
				t.Errorf("Config after resolve = %+v, want %+v", after, before)
			}
		})
	}
}

func TestResolvedFillsDefaultsFromTheIdentity(t *testing.T) {
	t.Parallel()
	c := validConfig(t.TempDir())
	id := c.Identity
	c.BinaryName, c.AssetTemplate, c.AssetExt, c.ChecksumsAsset = "", "", "", ""
	r, err := c.Resolved()
	if err != nil {
		t.Fatalf("Resolved() error = %v, want nil", err)
	}
	if r.BinaryName != id.Name {
		t.Errorf("BinaryName = %q, want the Identity Name %q", r.BinaryName, id.Name)
	}
	if r.AssetTemplate != asset.DefaultTemplate {
		t.Errorf("AssetTemplate = %q, want %q", r.AssetTemplate, asset.DefaultTemplate)
	}
	if r.AssetExt != asset.DefaultExt {
		t.Errorf("AssetExt = %q, want %q", r.AssetExt, asset.DefaultExt)
	}
	if r.ChecksumsAsset != asset.DefaultChecksumsTemplate {
		t.Errorf("ChecksumsAsset = %q, want %q", r.ChecksumsAsset, asset.DefaultChecksumsTemplate)
	}
	if r.PreflightTimeout != 10*time.Second {
		t.Errorf("PreflightTimeout = %v, want %v", r.PreflightTimeout, 10*time.Second)
	}
}

func TestResolvedRejectsInvalidConfig(t *testing.T) {
	t.Parallel()
	c := validConfig("")
	c.Name = ""
	r, err := c.Resolved()
	if !errors.Is(err, validate.ErrEmpty) {
		t.Errorf("Resolved() error = %v, want one wrapping %v", err, validate.ErrEmpty)
	}
	if got := plainFieldsOf(r); got != (plainFields{}) {
		t.Errorf("Resolved() Config = %+v, want the zero Config", got)
	}
}

func TestOptOutVariableIsDerivedFromEnvPrefix(t *testing.T) {
	t.Parallel()
	name, err := validIdentity().EnvVar(optOutSuffix)
	if err != nil {
		t.Fatalf("EnvVar(%q) error = %v, want nil", optOutSuffix, err)
	}
	if name != "ATK_NO_UPDATE_CHECK" {
		t.Errorf("EnvVar(%q) = %q, want %q", optOutSuffix, name, "ATK_NO_UPDATE_CHECK")
	}
}

func TestValidateAcceptsValidConfig(t *testing.T) {
	t.Parallel()
	if err := (Config{Identity: validIdentity(), TrustedKeys: []string{testKey()}, Source: &stubSource{}}).Validate(); err != nil {
		t.Fatalf("Validate() = %v, want nil", err)
	}
}

func TestValidateReportsEachProblem(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		set  func(*Config)
		want error
	}{
		{"empty Name", func(c *Config) { c.Name = "" }, validate.ErrEmpty},
		{"invalid Name", func(c *Config) { c.Name = "Atk" }, validate.ErrInvalid},
		{"empty Owner", func(c *Config) { c.Owner = "" }, validate.ErrEmpty},
		{"invalid Owner", func(c *Config) { c.Owner = "a/b" }, validate.ErrInvalid},
		{"empty Repo", func(c *Config) { c.Repo = "" }, validate.ErrEmpty},
		{"invalid Repo", func(c *Config) { c.Repo = "toolkit.git" }, validate.ErrInvalid},
		{"empty EnvPrefix", func(c *Config) { c.EnvPrefix = "" }, validate.ErrEmpty},
		{"invalid EnvPrefix", func(c *Config) { c.EnvPrefix = "atk" }, validate.ErrInvalid},
		{"empty DirName", func(c *Config) { c.DirName = "" }, validate.ErrEmpty},
		{"invalid DirName", func(c *Config) { c.DirName = ".hidden" }, validate.ErrInvalid},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c := Config{Identity: validIdentity(), TrustedKeys: []string{testKey()}, Source: &stubSource{}}
			tc.set(&c)
			if err := c.Validate(); !errors.Is(err, tc.want) {
				t.Errorf("Validate() = %v, want one wrapping %v", err, tc.want)
			}
		})
	}
}

func TestValidateReportsEveryProblemAtOnce(t *testing.T) {
	t.Parallel()
	c := Config{Identity: validIdentity(), TrustedKeys: []string{testKey()}}
	c.Name = ""
	c.DirName = ".."

	err := c.Validate()
	for _, want := range []error{validate.ErrEmpty, validate.ErrInvalid} {
		if !errors.Is(err, want) {
			t.Errorf("Validate() = %v, want one wrapping %v", err, want)
		}
	}
}

func TestValidateChecksAssetFields(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		set  func(*Config)
		want []error
	}{
		{"unknown placeholder in AssetTemplate", func(c *Config) { c.AssetTemplate = "{name}_{platform}.tar.gz" },
			[]error{validate.ErrInvalid, asset.ErrUnknownPlaceholder}},
		{"unclosed placeholder in AssetTemplate", func(c *Config) { c.AssetTemplate = "{name" },
			[]error{validate.ErrInvalid, asset.ErrUnclosedPlaceholder}},
		{"unknown placeholder in ChecksumsAsset", func(c *Config) { c.ChecksumsAsset = "{nope}.txt" },
			[]error{validate.ErrInvalid, asset.ErrUnknownPlaceholder}},
		{"{ext} in ChecksumsAsset", func(c *Config) { c.ChecksumsAsset = "{name}.{ext}" },
			[]error{validate.ErrInvalid, asset.ErrMisplacedExt}},
		{"AssetExt with a slash", func(c *Config) { c.AssetExt = "tar/gz" }, []error{validate.ErrInvalid}},
		{"AssetExt with a backslash", func(c *Config) { c.AssetExt = `tar\gz` }, []error{validate.ErrInvalid}},
		{"AssetExt with an opening brace", func(c *Config) { c.AssetExt = "{ext" }, []error{validate.ErrInvalid}},
		{"AssetExt with a closing brace", func(c *Config) { c.AssetExt = "ext}" }, []error{validate.ErrInvalid}},
		{"AssetExt with a leading dot", func(c *Config) { c.AssetExt = ".zip" }, []error{validate.ErrInvalid}},
		{"unsupported AssetExt tar.xz", func(c *Config) { c.AssetExt = "tar.xz" }, []error{validate.ErrInvalid}},
		{"unsupported AssetExt tar.zst", func(c *Config) { c.AssetExt = "tar.zst" }, []error{validate.ErrInvalid}},
		{"upper-case AssetExt TGZ", func(c *Config) { c.AssetExt = "TGZ" }, []error{validate.ErrInvalid}},
		{"unsupported AssetExt with an explicit {ext} template", func(c *Config) {
			c.AssetTemplate = "{name}.{ext}"
			c.AssetExt = "7z"
		}, []error{validate.ErrInvalid}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c := validConfig("")
			tc.set(&c)
			err := c.Validate()
			for _, want := range tc.want {
				if !errors.Is(err, want) {
					t.Errorf("Validate() = %v, want one wrapping %v", err, want)
				}
			}
			if _, rerr := c.resolve(); rerr == nil {
				t.Error("resolve() error = nil, want the invalid Config rejected")
			}
		})
	}
}

func TestValidateRejectsNegativeDownloadLimits(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		set   func(*Config)
		field string
	}{
		{"negative MaxDownloadBytes", func(c *Config) { c.MaxDownloadBytes = -1 }, "MaxDownloadBytes"},
		{"negative MaxExtractBytes", func(c *Config) { c.MaxExtractBytes = -1 }, "MaxExtractBytes"},
		{"negative DownloadTimeout", func(c *Config) { c.DownloadTimeout = -time.Second }, "DownloadTimeout"},
		{"negative PreflightTimeout", func(c *Config) { c.PreflightTimeout = -time.Nanosecond }, "PreflightTimeout"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c := validConfig("")
			tc.set(&c)
			err := c.Validate()
			if !errors.Is(err, validate.ErrInvalid) {
				t.Errorf("Validate() = %v, want one wrapping %v", err, validate.ErrInvalid)
			}
			var fe *validate.FieldError
			if !errors.As(err, &fe) || fe.Field != tc.field {
				t.Errorf("Validate() = %v, want a FieldError for %q", err, tc.field)
			}
			if _, rerr := c.resolve(); rerr == nil {
				t.Error("resolve() error = nil, want the invalid Config rejected")
			}
		})
	}
}

func TestValidateAcceptsZeroAndPositiveDownloadLimits(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		set  func(*Config)
	}{
		{"zero limits", func(*Config) {}},
		{"positive limits", func(c *Config) {
			c.MaxDownloadBytes = 1
			c.MaxExtractBytes = 1
			c.DownloadTimeout = time.Nanosecond
			c.PreflightTimeout = time.Nanosecond
		}},
		{"long PreflightTimeout", func(c *Config) { c.PreflightTimeout = 24 * time.Hour }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c := validConfig("")
			tc.set(&c)
			if err := c.Validate(); err != nil {
				t.Errorf("Validate() = %v, want nil", err)
			}
		})
	}
}

func TestValidateAcceptsEmptyAndValidAssetFields(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		set  func(*Config)
	}{
		{"every field empty", func(*Config) {}},
		{"custom templates and extension", func(c *Config) {
			c.AssetTemplate = "{name}-{version}-{os}-{arch}.{ext}"
			c.ChecksumsAsset = "{name}_{tag}_checksums.txt"
			c.AssetExt = "zip"
		}},
		{"raw binary template", func(c *Config) { c.AssetTemplate = "{name}-{os}-{arch}" }},
		{"tgz extension", func(c *Config) { c.AssetExt = "tgz" }},
		{"any extension on a raw binary template", func(c *Config) {
			c.AssetTemplate = "{name}-{os}-{arch}"
			c.AssetExt = "tar.xz"
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c := validConfig("")
			tc.set(&c)
			if err := c.Validate(); err != nil {
				t.Errorf("Validate() = %v, want nil", err)
			}
		})
	}
}

func TestValidateRejectsANilDetector(t *testing.T) {
	t.Parallel()
	detector := func(string) (Install, bool) { return Install{}, false }
	tests := []struct {
		name      string
		detectors []Detector
		field     string
	}{
		{"only entry nil", []Detector{nil}, "Detectors[0]"},
		{"second entry nil", []Detector{detector, nil}, "Detectors[1]"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c := validConfig("")
			c.Detectors = tc.detectors
			err := c.Validate()
			if !errors.Is(err, validate.ErrInvalid) {
				t.Errorf("Validate() = %v, want one wrapping %v", err, validate.ErrInvalid)
			}
			var fe *validate.FieldError
			if !errors.As(err, &fe) || fe.Field != tc.field || fe.Value != "nil" {
				t.Errorf("Validate() = %v, want a FieldError for %q with Value %q", err, tc.field, "nil")
			}
			if _, rerr := c.resolve(); rerr == nil {
				t.Error("resolve() error = nil, want the invalid Config rejected")
			}
		})
	}
}

func TestValidateAcceptsTheSignaturePolicies(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		set  func(*Config)
	}{
		{"one key", func(c *Config) { c.TrustedKeys = []string{testKey()} }},
		{"several keys", func(c *Config) { c.TrustedKeys = []string{testKey(), testKey2()} }},
		{"a key with its comment line", func(c *Config) {
			c.TrustedKeys = []string{"untrusted comment: minisign public key 1\n" + testKey() + "\n"}
		}},
		{"AllowUnsigned", func(c *Config) { c.TrustedKeys, c.AllowUnsigned = nil, true }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c := validConfig("")
			tc.set(&c)
			if err := c.Validate(); err != nil {
				t.Errorf("Validate() = %v, want nil", err)
			}
		})
	}
}

func TestValidateRejectsAKeyThatDoesNotParse(t *testing.T) {
	t.Parallel()
	c := validConfig("")
	c.TrustedKeys = []string{testKey(), "not a key"}
	err := c.Validate()
	var fe *validate.FieldError
	if !errors.As(err, &fe) || fe.Field != "TrustedKeys[1]" {
		t.Fatalf("Validate() = %v, want a FieldError for TrustedKeys[1]", err)
	}
	if !errors.Is(err, validate.ErrInvalid) {
		t.Errorf("Validate() = %v, want one wrapping %v", err, validate.ErrInvalid)
	}
	var mk *minisign.MalformedKeyError
	if !errors.As(err, &mk) {
		t.Errorf("Validate() = %v, want one wrapping *minisign.MalformedKeyError", err)
	}
	if _, rerr := c.resolve(); rerr == nil {
		t.Error("resolve() error = nil, want the invalid Config rejected")
	}
}

func TestValidateRejectsADuplicateKeyID(t *testing.T) {
	t.Parallel()
	c := validConfig("")
	c.TrustedKeys = []string{testKey(), testKey2(), "untrusted comment: again\n" + testKey()}
	err := c.Validate()
	var fe *validate.FieldError
	if !errors.As(err, &fe) || fe.Field != "TrustedKeys[2]" {
		t.Fatalf("Validate() = %v, want a FieldError for TrustedKeys[2]", err)
	}
	key, perr := minisign.ParsePublicKey(testKey())
	if perr != nil {
		t.Fatal(perr)
	}
	if fe.Value != key.ID.String() {
		t.Errorf("FieldError.Value = %q, want the key id %q", fe.Value, key.ID)
	}
	if !errors.Is(err, validate.ErrInvalid) {
		t.Errorf("Validate() = %v, want one wrapping %v", err, validate.ErrInvalid)
	}
}

func TestValidateRequiresKeysOrAllowUnsigned(t *testing.T) {
	t.Parallel()
	for name, c := range map[string]Config{
		"zero Config":        {},
		"identity alone":     {Identity: validIdentity()},
		"empty non-nil keys": {Identity: validIdentity(), TrustedKeys: []string{}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			err := c.Validate()
			var fe *validate.FieldError
			found := false
			for _, e := range unwrapAll(err) {
				if errors.As(e, &fe) && fe.Field == "TrustedKeys" {
					found = true
				}
			}
			if !found || !errors.Is(err, validate.ErrInvalid) {
				t.Errorf("Validate() = %v, want a FieldError for TrustedKeys wrapping %v", err, validate.ErrInvalid)
			}
			if _, rerr := c.resolve(); rerr == nil {
				t.Error("resolve() error = nil, want the invalid Config rejected")
			}
		})
	}
}

func TestValidateRejectsKeysTogetherWithAllowUnsigned(t *testing.T) {
	t.Parallel()
	c := validConfig("")
	c.AllowUnsigned = true
	err := c.Validate()
	var fe *validate.FieldError
	if !errors.As(err, &fe) || fe.Field != "AllowUnsigned" {
		t.Fatalf("Validate() = %v, want a FieldError for AllowUnsigned", err)
	}
	if !errors.Is(err, validate.ErrInvalid) {
		t.Errorf("Validate() = %v, want one wrapping %v", err, validate.ErrInvalid)
	}
}

// unwrapAll returns the errors err joins, or err alone.
func unwrapAll(err error) []error {
	if j, ok := err.(interface{ Unwrap() []error }); ok {
		return j.Unwrap()
	}
	return []error{err}
}

func TestDeriveFormat(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		tmpl   string
		ext    string
		want   assetFormat
		wantOK bool
	}{
		{"tar.gz", "{name}.{ext}", "tar.gz", formatTarGz, true},
		{"tgz", "{name}.{ext}", "tgz", formatTarGz, true},
		{"zip", "{name}.{ext}", "zip", formatZip, true},
		{"empty template and extension", "", "", formatTarGz, true},
		{"empty extension", "{name}.{ext}", "", formatTarGz, true},
		{"empty template with zip", "", "zip", formatZip, true},
		{"template without {ext}", "{name}-{os}-{arch}", "zip", formatRaw, true},
		{"template without {ext} and an unsupported extension", "{name}", "tar.xz", formatRaw, true},
		{"template without {ext} and an empty extension", "{name}", "", formatRaw, true},
		{"unparseable template", "{name", "tar.xz", formatRaw, true},
		{"tar.xz", "{name}.{ext}", "tar.xz", 0, false},
		{"tar.zst", "{name}.{ext}", "tar.zst", 0, false},
		{"upper-case TGZ", "{name}.{ext}", "TGZ", 0, false},
		{"upper-case ZIP", "{name}.{ext}", "ZIP", 0, false},
		{"unsupported extension on the default template", "", "tar.xz", 0, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, ok := deriveFormat(tc.tmpl, tc.ext)
			if got != tc.want || ok != tc.wantOK {
				t.Errorf("deriveFormat(%q, %q) = %v, %v, want %v, %v", tc.tmpl, tc.ext, got, ok, tc.want, tc.wantOK)
			}
		})
	}
}

func TestValidateReportsEveryNilDetector(t *testing.T) {
	t.Parallel()
	detector := func(string) (Install, bool) { return Install{}, false }
	c := validConfig("")
	c.Detectors = []Detector{nil, detector, nil}

	err := c.Validate()
	if err == nil {
		t.Fatal("Validate() = nil, want the nil detectors rejected")
	}
	for field, want := range map[string]bool{"Detectors[0]": true, "Detectors[1]": false, "Detectors[2]": true} {
		if got := strings.Contains(err.Error(), field); got != want {
			t.Errorf("Validate() = %v, names %s: %v, want %v", err, field, got, want)
		}
	}
}

func TestValidateAcceptsDetectors(t *testing.T) {
	t.Parallel()
	detector := func(string) (Install, bool) { return Install{}, false }
	for _, detectors := range [][]Detector{nil, {}, {detector, detector}} {
		c := validConfig("")
		c.Detectors = detectors
		if err := c.Validate(); err != nil {
			t.Errorf("Validate() with %d detectors = %v, want nil", len(detectors), err)
		}
	}
}

func TestResolvedConfigFormat(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		set  func(*Config)
		want assetFormat
	}{
		{"defaults", func(*Config) {}, formatTarGz},
		{"tgz", func(c *Config) { c.AssetExt = "tgz" }, formatTarGz},
		{"zip", func(c *Config) { c.AssetExt = "zip" }, formatZip},
		{"raw template", func(c *Config) { c.AssetTemplate = "{name}-{os}-{arch}" }, formatRaw},
		{"raw template ignores the extension", func(c *Config) {
			c.AssetTemplate = "{name}"
			c.AssetExt = "zip"
		}, formatRaw},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c := validConfig(t.TempDir())
			tc.set(&c)
			r, err := c.resolve()
			if err != nil {
				t.Fatalf("resolve() = %v, want nil", err)
			}
			if got := r.format(); got != tc.want {
				t.Errorf("format() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestValidateAssetTemplateAndExtTogether(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		tmpl    string
		ext     string
		wantErr bool
	}{
		{"empty template and extension", "", "", false},
		{"empty template with an unsupported extension", "", "tar.xz", true},
		{"empty template with zip", "", "zip", false},
		{"unparseable template reports the template alone", "{name", "tar.xz", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c := validConfig("")
			c.AssetTemplate, c.AssetExt = tc.tmpl, tc.ext
			err := c.Validate()
			if (err != nil) != tc.wantErr {
				t.Fatalf("Validate() = %v, want error: %v", err, tc.wantErr)
			}
			var fe *validate.FieldError
			if tc.tmpl == "{name" && (!errors.As(err, &fe) || fe.Field != "AssetTemplate" || errors.Is(err, asset.ErrEmptyTemplate)) {
				t.Errorf("Validate() = %v, want a FieldError for AssetTemplate", err)
			}
		})
	}
}

func TestResolveDefaultsCheckWait(t *testing.T) {
	t.Parallel()
	r, err := validConfig(t.TempDir()).resolve()
	if err != nil {
		t.Fatalf("resolve() error = %v, want nil", err)
	}
	if r.CheckWait != 250*time.Millisecond {
		t.Errorf("CheckWait = %v, want %v", r.CheckWait, 250*time.Millisecond)
	}
}

func TestResolveKeepsCheckWait(t *testing.T) {
	t.Parallel()
	c := validConfig(t.TempDir())
	c.CheckWait = time.Second
	r, err := c.resolve()
	if err != nil {
		t.Fatalf("resolve() error = %v, want nil", err)
	}
	if r.CheckWait != time.Second {
		t.Errorf("CheckWait = %v, want %v", r.CheckWait, time.Second)
	}
}

func TestValidateChecksCheckWait(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		wait    time.Duration
		wantErr bool
	}{
		{"zero", 0, false},
		{"one nanosecond", time.Nanosecond, false},
		{"exactly two seconds", 2 * time.Second, false},
		{"negative", -time.Nanosecond, true},
		{"just over two seconds", 2*time.Second + time.Nanosecond, true},
		{"a minute", time.Minute, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c := validConfig("")
			c.CheckWait = tc.wait
			err := c.Validate()
			if (err != nil) != tc.wantErr {
				t.Fatalf("Validate() = %v, want error: %v", err, tc.wantErr)
			}
			if !tc.wantErr {
				return
			}
			var fe *validate.FieldError
			if !errors.Is(err, validate.ErrInvalid) || !errors.As(err, &fe) || fe.Field != "CheckWait" {
				t.Errorf("Validate() = %v, want a FieldError for CheckWait wrapping %v", err, validate.ErrInvalid)
			}
			if _, rerr := c.resolve(); rerr == nil {
				t.Error("resolve() error = nil, want the invalid Config rejected")
			}
		})
	}
}

func TestValidateChecksChannel(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		channel Channel
		wantErr bool
	}{
		{"zero is stable", 0, false},
		{"stable", Stable, false},
		{"beta", Beta, false},
		{"edge", Edge, false},
		{"one past edge", Edge + 1, true},
		{"seven", Channel(7), true},
		{"negative", Channel(-1), true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c := validConfig("")
			c.Channel = tc.channel
			err := c.Validate()
			if (err != nil) != tc.wantErr {
				t.Fatalf("Validate() = %v, want error: %v", err, tc.wantErr)
			}
			if !tc.wantErr {
				return
			}
			var fe *validate.FieldError
			if !errors.Is(err, validate.ErrInvalid) || !errors.As(err, &fe) || fe.Field != "Channel" {
				t.Errorf("Validate() = %v, want a FieldError for Channel wrapping %v", err, validate.ErrInvalid)
			}
			if _, rerr := c.resolve(); rerr == nil {
				t.Error("resolve() error = nil, want the invalid Config rejected")
			}
		})
	}
}

type fakeLocker struct{}

func (fakeLocker) Lock(context.Context) (func(), error) { return func() {}, nil }

func (fakeLocker) TryLock() (func(), error) { return func() {}, nil }

func TestResolveKeepsASuppliedLocker(t *testing.T) {
	t.Parallel()
	c := validConfig(t.TempDir())
	c.Locker = fakeLocker{}
	r, err := c.resolve()
	if err != nil {
		t.Fatalf("resolve() error = %v, want nil", err)
	}
	if r.Locker != filelock.Locker(fakeLocker{}) {
		t.Errorf("Locker = %v, want the supplied locker", r.Locker)
	}
}

func TestResolveDefaultLockerCreatesTheStateDir(t *testing.T) {
	t.Parallel()
	stateDir := filepath.Join(t.TempDir(), "missing", "state")
	c := validConfig("")
	c.StateDir = stateDir
	r, err := c.resolve()
	if err != nil {
		t.Fatalf("resolve() error = %v, want nil", err)
	}
	ctx := context.Background()

	unlock, err := r.Locker.Lock(ctx)
	if err != nil {
		t.Fatalf("Lock() error = %v, want nil", err)
	}
	if info, err := os.Stat(stateDir); err != nil || !info.IsDir() {
		t.Errorf("Stat(%q) = %v, %v, want the directory created", stateDir, info, err)
	}
	if _, err := os.Stat(filepath.Join(stateDir, "update.lock")); err != nil {
		t.Errorf("Stat(update.lock) error = %v, want the lock file", err)
	}

	held, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	defer cancel()
	if unlock2, err := r.Locker.Lock(held); err == nil {
		unlock2()
		t.Fatal("second Lock() while held error = nil, want the context's error")
	} else if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("second Lock() error = %v, want one wrapping %v", err, context.DeadlineExceeded)
	}

	unlock()
	unlock, err = r.Locker.Lock(ctx)
	if err != nil {
		t.Fatalf("Lock() after unlock error = %v, want nil", err)
	}
	unlock()
}

func TestDefaultLockerReportsAnUncreatableStateDir(t *testing.T) {
	t.Parallel()
	blocker := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocker, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := dirLocker{dir: filepath.Join(blocker, "state")}.Lock(context.Background())
	if err == nil {
		t.Fatal("Lock() error = nil, want the directory error")
	}
}

func TestDefaultLockerTryLockReportsAnUncreatableStateDir(t *testing.T) {
	t.Parallel()
	blocker := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocker, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := dirLocker{dir: filepath.Join(blocker, "state")}.TryLock()
	if err == nil {
		t.Fatal("TryLock() error = nil, want the directory error")
	}
}

func TestResolveDefaultLockerTryLock(t *testing.T) {
	t.Parallel()
	stateDir := filepath.Join(t.TempDir(), "missing", "state")
	c := validConfig("")
	c.StateDir = stateDir
	r, err := c.resolve()
	if err != nil {
		t.Fatalf("resolve() error = %v, want nil", err)
	}

	unlock, err := r.Locker.TryLock()
	if err != nil {
		t.Fatalf("TryLock() error = %v, want nil", err)
	}
	if info, err := os.Stat(stateDir); err != nil || !info.IsDir() {
		t.Errorf("Stat(%q) = %v, %v, want the directory created", stateDir, info, err)
	}

	if unlock2, err := r.Locker.TryLock(); err == nil {
		unlock2()
		t.Fatal("second TryLock() while held error = nil, want filelock.ErrLocked")
	} else if !errors.Is(err, filelock.ErrLocked) {
		t.Errorf("second TryLock() error = %v, want one wrapping %v", err, filelock.ErrLocked)
	}
	unlock()

	held, err := r.Locker.Lock(context.Background())
	if err != nil {
		t.Fatalf("Lock() after unlock error = %v, want nil", err)
	}
	if unlock2, err := r.Locker.TryLock(); err == nil {
		unlock2()
		t.Fatal("TryLock() while Lock holds error = nil, want filelock.ErrLocked")
	} else if !errors.Is(err, filelock.ErrLocked) {
		t.Errorf("TryLock() error = %v, want one wrapping %v", err, filelock.ErrLocked)
	}
	held()

	unlock, err = r.Locker.TryLock()
	if err != nil {
		t.Fatalf("TryLock() after unlock error = %v, want nil", err)
	}
	unlock()
}

func TestResolveWiresTheSourceClockFromNow(t *testing.T) {
	t.Parallel()

	at := time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)
	c := validConfig(t.TempDir())
	c.Source = nil
	c.Now = func() time.Time { return at }

	r, err := c.resolve()
	if err != nil {
		t.Fatalf("resolve() error = %v, want nil", err)
	}
	gh, ok := r.Source.(*GitHubSource)
	if !ok {
		t.Fatalf("Source = %T, want *GitHubSource", r.Source)
	}
	if gh.Now == nil || !gh.Now().Equal(at) {
		t.Error("Source.Now is not the Config's clock")
	}
}

func TestResolveDefaultSourceClockIsTheWallClock(t *testing.T) {
	t.Parallel()

	c := validConfig(t.TempDir())
	c.Source = nil
	c.Now = nil

	r, err := c.resolve()
	if err != nil {
		t.Fatalf("resolve() error = %v, want nil", err)
	}
	gh, ok := r.Source.(*GitHubSource)
	if !ok || gh.Now == nil {
		t.Fatal("Source.Now = nil, want the resolved clock")
	}
	if d := time.Since(gh.Now()); d < 0 || d > time.Minute {
		t.Errorf("Source.Now() is %v from the wall clock, want about 0", d)
	}
}

func TestResolveKeepsASuppliedSourceUntouched(t *testing.T) {
	t.Parallel()

	gh := &GitHubSource{Owner: "o", Repo: "r"}
	c := validConfig(t.TempDir())
	c.Source = gh
	c.Now = func() time.Time { return time.Unix(1, 0) }

	if _, err := c.resolve(); err != nil {
		t.Fatalf("resolve() error = %v, want nil", err)
	}
	if gh.Now != nil {
		t.Error("resolve set Now on a supplied GitHubSource, want it left nil")
	}
	if gh.Token != nil {
		t.Error("resolve set Token on a supplied GitHubSource, want it left nil")
	}
}

func TestResolveWiresTheFileResponseCacheForTheDefaultSource(t *testing.T) {
	t.Parallel()

	stateDir := filepath.Join(t.TempDir(), "state")
	c := validConfig(t.TempDir())
	c.Source = nil
	c.StateDir = stateDir

	r, err := c.resolve()
	if err != nil {
		t.Fatalf("resolve() error = %v, want nil", err)
	}
	gh, ok := r.Source.(*GitHubSource)
	if !ok {
		t.Fatalf("Source = %T, want *GitHubSource", r.Source)
	}
	fc, ok := gh.Cache.(*fileCache)
	if !ok {
		t.Fatalf("Source.Cache = %T, want the file cache", gh.Cache)
	}
	if want := filepath.Join(stateDir, "github-cache.json"); fc.path() != want {
		t.Errorf("cache file = %s, want %s", fc.path(), want)
	}
}

func TestResolveLeavesTheCacheOfASuppliedSourceAlone(t *testing.T) {
	t.Parallel()

	gh := &GitHubSource{Owner: "o", Repo: "r"}
	c := validConfig(t.TempDir())
	c.Source = gh

	if _, err := c.resolve(); err != nil {
		t.Fatalf("resolve() error = %v, want nil", err)
	}
	if gh.Cache != nil {
		t.Errorf("Cache = %T, want a supplied GitHubSource's Cache left nil", gh.Cache)
	}
}

// resolvedGitHubSource resolves c, whose Source is nil, and returns the default GitHubSource
// pointed at g's web and API servers and at the repository they serve.
func resolvedGitHubSource(t *testing.T, c Config, g *splitGitHub) *GitHubSource {
	t.Helper()

	r, err := c.resolve()
	if err != nil {
		t.Fatalf("resolve() error = %v, want nil", err)
	}
	gh, ok := r.Source.(*GitHubSource)
	if !ok {
		t.Fatalf("Source = %T, want *GitHubSource", r.Source)
	}
	gh.Owner, gh.Repo = testOwner, testRepo
	gh.WebURL = g.web.URL
	gh.APIURL = g.api.URL

	return gh
}

func TestResolveDefaultSourceReadsTheTokenThroughGetenv(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		env  map[string]string
		want string
	}{
		{name: "GH_TOKEN wins", env: map[string]string{"GH_TOKEN": "gh-token", "GITHUB_TOKEN": "github-token"}, want: "Bearer gh-token"},
		{name: "GITHUB_TOKEN alone", env: map[string]string{"GITHUB_TOKEN": "github-token"}, want: "Bearer github-token"},
		{name: "neither", env: nil, want: ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			g := newSplitGitHub(t, respond(http.StatusOK, ""), respond(http.StatusOK, apiReleaseJSON("v1.4.0")))
			c := validConfig(t.TempDir())
			c.Source = nil
			c.Getenv = envOf(tc.env)

			if _, err := resolvedGitHubSource(t, c, g).Latest(context.Background(), Stable); err != nil {
				t.Fatalf("Latest() error = %v", err)
			}
			if seen := g.apiAuth.seen(); len(seen) != 1 || seen[0] != tc.want {
				t.Errorf("API Authorization = %q, want one %q", seen, tc.want)
			}
			g.webAuth.assertNoAuthorization(t, "the web host")
		})
	}
}

func TestResolveDefaultSourceUsesTheConfigToken(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name  string
		token string
		want  string
	}{
		{name: "token", token: "from-config", want: "Bearer from-config"},
		{name: "empty token opts out", token: "", want: ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			g := newSplitGitHub(t, respond(http.StatusOK, ""), respond(http.StatusOK, apiReleaseJSON("v1.4.0")))
			c := validConfig(t.TempDir())
			c.Source = nil
			c.Getenv = envOf(map[string]string{"GH_TOKEN": "from-env"})
			c.Token = staticToken(tc.token)

			if _, err := resolvedGitHubSource(t, c, g).Latest(context.Background(), Stable); err != nil {
				t.Fatalf("Latest() error = %v", err)
			}
			if seen := g.apiAuth.seen(); len(seen) != 1 || seen[0] != tc.want {
				t.Errorf("API Authorization = %q, want one %q", seen, tc.want)
			}
		})
	}
}

func TestResolveDefaultSourceRejectedEnvTokenIsTokenRejectedError(t *testing.T) {
	t.Parallel()

	g := newSplitGitHub(t, respond(http.StatusOK, ""), respond(http.StatusUnauthorized, "bad credentials"))
	c := validConfig(t.TempDir())
	c.Source = nil
	c.Getenv = envOf(map[string]string{"GITHUB_TOKEN": "revoked"})

	_, err := resolvedGitHubSource(t, c, g).Latest(context.Background(), Stable)

	var tr *TokenRejectedError
	if !errors.As(err, &tr) {
		t.Fatalf("err = %v, want a *TokenRejectedError", err)
	}
	if tr.Status.StatusCode != http.StatusUnauthorized {
		t.Errorf("TokenRejectedError.Status.StatusCode = %d, want 401", tr.Status.StatusCode)
	}
}

func TestResolveDefaultsTokenToTheEnvironment(t *testing.T) {
	t.Parallel()

	c := validConfig(t.TempDir())
	c.Getenv = envOf(map[string]string{"GH_TOKEN": "gh-token"})

	r, err := c.resolve()
	if err != nil {
		t.Fatalf("resolve() error = %v, want nil", err)
	}
	if r.Token == nil {
		t.Fatal("Token = nil, want the environment reader")
	}
	if got, err := r.Token(context.Background()); err != nil || got != "gh-token" {
		t.Errorf("Token() = %q, %v, want %q, nil", got, err, "gh-token")
	}
}
