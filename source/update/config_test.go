package update

import (
	"context"
	"errors"
	"net/http"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"go.vipengele.org/cli/spec"
	"go.vipengele.org/commons/validate"
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
// value, except UserConfigDir, which returns configDir so that no test reads the host's home.
func validConfig(configDir string) Config {
	return Config{
		Identity:      validIdentity(),
		Source:        &stubSource{},
		UserConfigDir: func() (string, error) { return configDir, nil },
	}
}

// plainFields is the comparable part of a Config: every field that is not a func.
type plainFields struct {
	Identity       spec.Identity
	BinaryName     string
	TempPrefix     string
	AssetTemplate  string
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
	configDir := filepath.Join(t.TempDir(), "config")

	before := time.Now()
	r, err := validConfig(configDir).resolve()
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
	if r.BinaryName != "atk" {
		t.Errorf("BinaryName = %q, want Identity.Name %q", r.BinaryName, "atk")
	}
	if r.TempPrefix != "atk-update-" {
		t.Errorf("TempPrefix = %q, want %q", r.TempPrefix, "atk-update-")
	}
	if r.AssetTemplate != "{name}_{os}_{arch}.tar.gz" {
		t.Errorf("AssetTemplate = %q, want %q", r.AssetTemplate, "{name}_{os}_{arch}.tar.gz")
	}
	if r.ChecksumsAsset != "checksums.txt" {
		t.Errorf("ChecksumsAsset = %q, want %q", r.ChecksumsAsset, "checksums.txt")
	}
	if want := filepath.Join(configDir, "agentic-toolkit"); r.StateDir != want {
		t.Errorf("StateDir = %q, want %q", r.StateDir, want)
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
	configRoot := filepath.Join(t.TempDir(), "elsewhere")

	c := validConfig("")
	c.BinaryName = "toolkit"
	c.TempPrefix = "tk-tmp-"
	c.AssetTemplate = "{name}-{os}-{arch}.zip"
	c.ChecksumsAsset = "SHA256SUMS"
	c.HTTPClient = client
	c.Now = func() time.Time { return fixed }
	c.GOOS = "plan9"
	c.GOARCH = "mips64"
	c.Executable = func() (string, error) { return "/opt/toolkit/bin/toolkit", nil }
	c.Getenv = func(name string) string { return "stub:" + name }
	c.UserConfigDir = func() (string, error) { return configRoot, nil }

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
	if r.AssetTemplate != "{name}-{os}-{arch}.zip" {
		t.Errorf("AssetTemplate = %q, want %q", r.AssetTemplate, "{name}-{os}-{arch}.zip")
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
	if got := r.Getenv("HOME"); got != "stub:HOME" {
		t.Errorf("Getenv(%q) = %q, want %q", "HOME", got, "stub:HOME")
	}
	if dir, err := r.UserConfigDir(); err != nil || dir != configRoot {
		t.Errorf("UserConfigDir() = %q, %v, want %q, nil", dir, err, configRoot)
	}
	if want := filepath.Join(configRoot, "agentic-toolkit"); r.StateDir != want {
		t.Errorf("StateDir = %q, want %q", r.StateDir, want)
	}
}

func TestResolveKeepsExplicitStateDir(t *testing.T) {
	t.Parallel()
	stateDir := filepath.Join(t.TempDir(), "state")
	c := validConfig("")
	c.StateDir = stateDir
	c.UserConfigDir = func() (string, error) {
		t.Error("UserConfigDir called, want an explicit StateDir used as it is")
		return "", errors.New("UserConfigDir called")
	}

	r, err := c.resolve()
	if err != nil {
		t.Fatalf("resolve() error = %v, want nil", err)
	}
	if r.StateDir != stateDir {
		t.Errorf("StateDir = %q, want %q", r.StateDir, stateDir)
	}
}

func TestResolveReportsUserConfigDirError(t *testing.T) {
	t.Parallel()
	errNoHome := errors.New("no home directory")
	c := validConfig("")
	c.UserConfigDir = func() (string, error) { return "", errNoHome }

	r, err := c.resolve()
	if !errors.Is(err, errNoHome) {
		t.Fatalf("resolve() error = %v, want one wrapping %v", err, errNoHome)
	}
	if got := plainFieldsOf(r); got != (plainFields{}) {
		t.Errorf("resolve() Config = %+v, want the zero Config", got)
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
	if c.Now != nil || c.Executable != nil || c.Getenv != nil {
		t.Error("resolve set a func field on its receiver, want the receiver's func fields left nil")
	}
}

func TestResolveRejectsInvalidConfig(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		set  func(*Config)
		want error
	}{
		{"nil Source", func(c *Config) { c.Source = nil }, ErrNoSource},
		{"empty Name", func(c *Config) { c.Name = "" }, validate.ErrEmpty},
		{"invalid DirName", func(c *Config) { c.DirName = ".." }, validate.ErrInvalid},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c := validConfig("")
			c.UserConfigDir = func() (string, error) {
				t.Error("UserConfigDir called, want an invalid Config rejected before any default")
				return "", errors.New("UserConfigDir called")
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
			if r.Now != nil || r.Executable != nil || r.Getenv != nil || r.UserConfigDir != nil {
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
		{"nil Source", func(c *Config) { c.Source = nil }, ErrNoSource},
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
	for _, want := range []error{ErrNoSource, validate.ErrEmpty, validate.ErrInvalid} {
		if !errors.Is(err, want) {
			t.Errorf("Validate() = %v, want one wrapping %v", err, want)
		}
	}
}
