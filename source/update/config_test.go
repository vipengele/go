package update

import (
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
	"go.vipengele.org/update/asset"
)

// stubSource is a Source that reports no release.
type stubSource struct{}

func (*stubSource) Latest(context.Context) (Release, error) {
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

// validConfig is a Config with every required field set and every optional field at its zero
// value, except the two seams the default StateDir is resolved through: Getenv reports every
// variable unset and UserHomeDir returns home, so that no test reads the host's environment or
// home directory.
func validConfig(home string) Config {
	return Config{
		Identity:    validIdentity(),
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
	r, err := Config{Identity: validIdentity(), Source: &stubSource{}}.resolve()
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
	if want := 5 * time.Minute; r.DownloadTimeout != want {
		t.Errorf("DownloadTimeout = %v, want %v", r.DownloadTimeout, want)
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

			r, err := Config{Identity: validIdentity(), Source: &stubSource{}}.resolve()
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
	c.DownloadTimeout = 7 * time.Second
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
	if r.DownloadTimeout != 7*time.Second {
		t.Errorf("DownloadTimeout = %v, want %v", r.DownloadTimeout, 7*time.Second)
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
	if c.Now != nil || c.Executable != nil || c.EvalSymlinks != nil || c.InContainer != nil {
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
	if err := (Config{Identity: validIdentity(), Source: &stubSource{}}).Validate(); err != nil {
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
			c := Config{Identity: validIdentity(), Source: &stubSource{}}
			tc.set(&c)
			if err := c.Validate(); !errors.Is(err, tc.want) {
				t.Errorf("Validate() = %v, want one wrapping %v", err, tc.want)
			}
		})
	}
}

func TestValidateReportsEveryProblemAtOnce(t *testing.T) {
	t.Parallel()
	c := Config{Identity: validIdentity()}
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
		{"negative DownloadTimeout", func(c *Config) { c.DownloadTimeout = -time.Second }, "DownloadTimeout"},
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
			c.DownloadTimeout = time.Nanosecond
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
		{"multi-part extension", func(c *Config) { c.AssetExt = "tar.xz" }},
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
