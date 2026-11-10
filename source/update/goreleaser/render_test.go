package goreleaser_test

import (
	"bytes"
	"errors"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"
	"text/template"

	"go.vipengele.org/cli/spec"
	"go.vipengele.org/cli/spec/version"
	"go.vipengele.org/commons/validate"
	"go.vipengele.org/update"
	"go.vipengele.org/update/asset"
	"go.vipengele.org/update/goreleaser"
	"go.vipengele.org/update/internal/minisigntest"
	"go.yaml.in/yaml/v3"
)

// releaseConfig is the part of a GoReleaser v2 config that Render writes. Decoding with known
// fields only makes any other key Render emits a failure.
type releaseConfig struct {
	Version     int             `yaml:"version"`
	ProjectName string          `yaml:"project_name"`
	Builds      []buildConfig   `yaml:"builds"`
	Archives    []archiveConfig `yaml:"archives"`
	Checksum    checksumConfig  `yaml:"checksum"`
	Signs       []signConfig    `yaml:"signs"`
}

type buildConfig struct {
	Binary string `yaml:"binary"`
	Main   string `yaml:"main"`
	// Dir is nil when the build has no dir key.
	Dir     *string         `yaml:"dir"`
	Env     []string        `yaml:"env"`
	Ldflags []string        `yaml:"ldflags"`
	Goos    []string        `yaml:"goos"`
	Goarch  []string        `yaml:"goarch"`
	Ignore  []ignoredTarget `yaml:"ignore"`
}

type ignoredTarget struct {
	Goos   string `yaml:"goos"`
	Goarch string `yaml:"goarch"`
}

type archiveConfig struct {
	NameTemplate    string           `yaml:"name_template"`
	Formats         []string         `yaml:"formats"`
	FormatOverrides []formatOverride `yaml:"format_overrides"`
}

type formatOverride struct {
	Goos    string   `yaml:"goos"`
	Formats []string `yaml:"formats"`
}

type checksumConfig struct {
	NameTemplate string `yaml:"name_template"`
	Algorithm    string `yaml:"algorithm"`
}

type signConfig struct {
	Artifacts string   `yaml:"artifacts"`
	Cmd       string   `yaml:"cmd"`
	Args      []string `yaml:"args"`
	Signature string   `yaml:"signature"`
}

// buildData carries the fields a GoReleaser build template reads.
type buildData struct {
	Tag        string
	FullCommit string
	Date       string
}

// testConfig returns a valid, signed Config whose StateDir is a directory of the test, so
// resolving it consults no environment.
func testConfig(t *testing.T) update.Config {
	t.Helper()
	return update.Config{
		Identity: spec.Identity{
			Name:      "atk",
			Owner:     "vipengele",
			Repo:      "agentic-toolkit",
			EnvPrefix: "ATK",
			DirName:   "agentic-toolkit",
		},
		TrustedKeys: []string{testKeyLine(t)},
		StateDir:    t.TempDir(),
	}
}

// unsignedConfig returns testConfig under AllowUnsigned.
func unsignedConfig(t *testing.T) update.Config {
	t.Helper()
	cfg := testConfig(t)
	cfg.TrustedKeys = nil
	cfg.AllowUnsigned = true
	return cfg
}

// testKeyLine returns the key line of the minisign key GenerateKey draws from a fixed stream. The
// stream is longer than the key id and Ed25519 seed GenerateKey reads, so it never runs dry.
func testKeyLine(t *testing.T) string {
	t.Helper()
	key, err := minisigntest.GenerateKey(strings.NewReader(strings.Repeat("goreleaser test key ", 4)))
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	return key.PublicKeyLine()
}

// testOptions returns Options naming a main package and nothing else.
func testOptions() goreleaser.Options {
	return goreleaser.Options{Main: "./cmd/atk"}
}

// render renders cfg with opts and fails the test on an error.
func render(t *testing.T, cfg update.Config, opts goreleaser.Options) []byte {
	t.Helper()
	out, err := goreleaser.Render(cfg, opts)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	return out
}

// parse decodes a rendered config, failing the test on any key releaseConfig does not hold, and
// checks it has the one build and one archive Render writes.
func parse(t *testing.T, out []byte) releaseConfig {
	t.Helper()
	dec := yaml.NewDecoder(bytes.NewReader(out))
	dec.KnownFields(true)
	var conf releaseConfig
	if err := dec.Decode(&conf); err != nil {
		t.Fatalf("decoding the rendered config: %v\n%s", err, out)
	}
	if conf.Version != 2 {
		t.Errorf("version = %d, want 2", conf.Version)
	}
	if len(conf.Builds) != 1 || len(conf.Archives) != 1 {
		t.Fatalf("got %d builds and %d archives, want one of each:\n%s", len(conf.Builds), len(conf.Archives), out)
	}
	return conf
}

// renderParsed renders cfg with opts and decodes the result.
func renderParsed(t *testing.T, cfg update.Config, opts goreleaser.Options) releaseConfig {
	t.Helper()
	return parse(t, render(t, cfg, opts))
}

// execute runs a GoReleaser template through text/template, as GoReleaser does.
func execute(t *testing.T, text string, data any) string {
	t.Helper()
	tmpl, err := template.New("field").Parse(text)
	if err != nil {
		t.Fatalf("parsing %q: %v", text, err)
	}
	var b strings.Builder
	if err := tmpl.Execute(&b, data); err != nil {
		t.Fatalf("executing %q: %v", text, err)
	}
	return b.String()
}

// expandSignArg expands a field of a signs block as GoReleaser does: environment variables
// first, from env, then the template. A variable outside env fails the test.
func expandSignArg(t *testing.T, arg string, env map[string]string) string {
	t.Helper()
	expanded := os.Expand(arg, func(name string) string {
		v, ok := env[name]
		if !ok {
			t.Errorf("%q expands the variable %q", arg, name)
		}
		return v
	})
	return execute(t, expanded, nil)
}

// linkFlag splits an -X flag into the variable path and its value.
func linkFlag(t *testing.T, flag string) (path, value string) {
	t.Helper()
	rest, ok := strings.CutPrefix(flag, "-X ")
	if !ok {
		t.Fatalf("ldflag %q is not -X", flag)
	}
	path, value, ok = strings.Cut(rest, "=")
	if !ok {
		t.Fatalf("ldflag %q has no '='", flag)
	}
	return path, value
}

func TestRenderLdflags(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		vars goreleaser.LinkVars
		want []string
	}{
		{
			name: "default",
			want: []string{
				"-s -w",
				"-X " + goreleaser.DefaultVersionVar + "={{ .Tag }}",
				"-X " + goreleaser.DefaultCommitVar + "={{ .FullCommit }}",
				"-X " + goreleaser.DefaultDateVar + "={{ .Date }}",
			},
		},
		{
			name: "default link vars",
			vars: goreleaser.DefaultLinkVars(),
			want: []string{
				"-s -w",
				"-X " + goreleaser.DefaultVersionVar + "={{ .Tag }}",
				"-X " + goreleaser.DefaultCommitVar + "={{ .FullCommit }}",
				"-X " + goreleaser.DefaultDateVar + "={{ .Date }}",
			},
		},
		{
			name: "every variable overridden",
			vars: goreleaser.LinkVars{Version: "example.com/tool/info.v", Commit: "example.com/tool/info.c", Date: "main.date"},
			want: []string{
				"-s -w",
				"-X example.com/tool/info.v={{ .Tag }}",
				"-X example.com/tool/info.c={{ .FullCommit }}",
				"-X main.date={{ .Date }}",
			},
		},
		{
			name: "one variable overridden",
			vars: goreleaser.LinkVars{Commit: "main.commit"},
			want: []string{
				"-s -w",
				"-X " + goreleaser.DefaultVersionVar + "={{ .Tag }}",
				"-X main.commit={{ .FullCommit }}",
				"-X " + goreleaser.DefaultDateVar + "={{ .Date }}",
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			opts := testOptions()
			opts.LinkVars = tc.vars
			build := renderParsed(t, testConfig(t), opts).Builds[0]
			if !slices.Equal(build.Ldflags, tc.want) {
				t.Errorf("ldflags = %q, want %q", build.Ldflags, tc.want)
			}
			if want := []string{"CGO_ENABLED=0"}; !slices.Equal(build.Env, want) {
				t.Errorf("env = %q, want %q", build.Env, want)
			}
		})
	}
}

func TestRenderLinksTheTagAsAReleaseVersion(t *testing.T) {
	t.Parallel()

	data := buildData{Tag: "v1.2.3", FullCommit: "0123456789abcdef0123456789abcdef01234567", Date: "2026-11-08T12:00:00Z"}
	build := renderParsed(t, testConfig(t), testOptions()).Builds[0]
	if len(build.Ldflags) != 4 {
		t.Fatalf("ldflags = %q, want four", build.Ldflags)
	}
	want := [][2]string{
		{goreleaser.DefaultVersionVar, data.Tag},
		{goreleaser.DefaultCommitVar, data.FullCommit},
		{goreleaser.DefaultDateVar, data.Date},
	}
	for i, w := range want {
		path, value := linkFlag(t, execute(t, build.Ldflags[i+1], data))
		if path != w[0] || value != w[1] {
			t.Errorf("ldflags[%d] sets %s=%s, want %s=%s", i+1, path, value, w[0], w[1])
		}
	}
	_, linked := linkFlag(t, execute(t, build.Ldflags[1], data))
	if !version.IsRelease(linked) {
		t.Errorf("the linked version %q is not a release version", linked)
	}
}

func TestRenderBinaryAndProjectName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		binaryName string
		want       string
	}{
		{name: "binary name defaults to the identity name", want: "atk"},
		{name: "binary name differs from the identity name", binaryName: "atk-cli", want: "atk-cli"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := testConfig(t)
			cfg.BinaryName = tc.binaryName
			conf := renderParsed(t, cfg, testOptions())
			if conf.ProjectName != "atk" {
				t.Errorf("project_name = %q, want %q", conf.ProjectName, "atk")
			}
			if got := execute(t, conf.Builds[0].Binary, nil); got != tc.want {
				t.Errorf("binary = %q, want %q", got, tc.want)
			}
			if got := conf.Builds[0].Main; got != "./cmd/atk" {
				t.Errorf("main = %q, want %q", got, "./cmd/atk")
			}
		})
	}
}

func TestRenderSigns(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		unsigned bool
		keyPath  string
		wantKey  string
	}{
		{name: "default key path", wantKey: goreleaser.DefaultSigningKeyPath},
		{name: "key path overridden", keyPath: "secrets/release.key", wantKey: "secrets/release.key"},
		{name: "unsigned", unsigned: true},
		{name: "unsigned ignores the key path", unsigned: true, keyPath: "secrets/release.key"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := testConfig(t)
			if tc.unsigned {
				cfg = unsignedConfig(t)
			}
			opts := testOptions()
			opts.SigningKeyPath = tc.keyPath
			out := render(t, cfg, opts)
			conf := parse(t, out)
			if tc.unsigned {
				if len(conf.Signs) != 0 || bytes.Contains(out, []byte("signs:")) {
					t.Errorf("an unsigned config has a signs block:\n%s", out)
				}
				return
			}
			if len(conf.Signs) != 1 {
				t.Fatalf("signs has %d entries, want 1:\n%s", len(conf.Signs), out)
			}
			sign := conf.Signs[0]
			if sign.Artifacts != "checksum" || sign.Cmd != "minisign" {
				t.Errorf("signs artifacts, cmd = %q, %q, want %q, %q", sign.Artifacts, sign.Cmd, "checksum", "minisign")
			}
			wantArgs := []string{"-S", "-s", tc.wantKey, "-m", "${artifact}", "-x", "${signature}"}
			if !slices.Equal(sign.Args, wantArgs) {
				t.Errorf("signs args = %q, want %q", sign.Args, wantArgs)
			}
			if sign.Signature != "${artifact}.minisig" {
				t.Errorf("signs signature = %q, want %q", sign.Signature, "${artifact}.minisig")
			}
		})
	}
}

func TestRenderFormats(t *testing.T) {
	t.Parallel()

	withWindows := []goreleaser.Platform{
		{GOOS: "linux", GOARCH: "amd64"},
		{GOOS: "windows", GOARCH: "amd64"},
		{GOOS: "darwin", GOARCH: "arm64"},
		{GOOS: "windows", GOARCH: "arm64"},
	}
	tests := []struct {
		name          string
		template      string
		ext           string
		platforms     []goreleaser.Platform
		want          string
		wantOverrides []formatOverride
	}{
		{name: "default extension", want: "tar.gz"},
		{name: "tar.gz", template: asset.DefaultTemplate, ext: "tar.gz", want: "tar.gz"},
		{name: "tgz", template: asset.DefaultTemplate, ext: "tgz", want: "tgz"},
		{name: "zip", template: asset.DefaultTemplate, ext: "zip", want: "zip"},
		{name: "raw binary", template: "{name}_{os}_{arch}", want: "binary"},
		{name: "raw binary ignores the extension", template: "{name}_{os}_{arch}", ext: "zip", want: "binary"},
		{
			name:          "default extension with windows",
			platforms:     withWindows,
			want:          asset.DefaultExtFor("linux"),
			wantOverrides: []formatOverride{{Goos: "windows", Formats: []string{asset.DefaultExtFor("windows")}}},
		},
		{
			name: "default extension with windows first",
			platforms: []goreleaser.Platform{
				{GOOS: "windows", GOARCH: "amd64"},
				{GOOS: "linux", GOARCH: "amd64"},
				{GOOS: "darwin", GOARCH: "arm64"},
			},
			want: asset.DefaultExtFor("windows"),
			wantOverrides: []formatOverride{
				{Goos: "linux", Formats: []string{asset.DefaultExtFor("linux")}},
				{Goos: "darwin", Formats: []string{asset.DefaultExtFor("darwin")}},
			},
		},
		{
			name:      "default extension for windows alone",
			platforms: []goreleaser.Platform{{GOOS: "windows", GOARCH: "amd64"}},
			want:      asset.DefaultExtFor("windows"),
		},
		{name: "tar.gz with windows", ext: "tar.gz", platforms: withWindows, want: "tar.gz"},
		{name: "zip with windows", ext: "zip", platforms: withWindows, want: "zip"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := testConfig(t)
			cfg.AssetTemplate = tc.template
			cfg.AssetExt = tc.ext
			opts := testOptions()
			opts.Platforms = tc.platforms
			out := render(t, cfg, opts)
			conf := parse(t, out)
			if want := []string{tc.want}; !slices.Equal(conf.Archives[0].Formats, want) {
				t.Errorf("formats = %q, want %q", conf.Archives[0].Formats, want)
			}
			if !slices.EqualFunc(conf.Archives[0].FormatOverrides, tc.wantOverrides, func(a, b formatOverride) bool {
				return a.Goos == b.Goos && slices.Equal(a.Formats, b.Formats)
			}) {
				t.Errorf("format_overrides = %+v, want %+v", conf.Archives[0].FormatOverrides, tc.wantOverrides)
			}
			if len(tc.wantOverrides) == 0 && bytes.Contains(out, []byte("format_overrides:")) {
				t.Errorf("the config holds a format_overrides key:\n%s", out)
			}
			if conf.Checksum.Algorithm != "sha256" {
				t.Errorf("checksum algorithm = %q, want sha256", conf.Checksum.Algorithm)
			}
		})
	}
}

// TestRenderIndependentOfGOOS renders one Config resolved for each of several operating systems
// and checks the configs are identical: what GoReleaser publishes for a platform depends on that
// platform, never on the GOOS the Config names or on the host that renders it.
func TestRenderIndependentOfGOOS(t *testing.T) {
	t.Parallel()

	opts := goreleaser.Options{Main: "./cmd/atk", Platforms: []goreleaser.Platform{
		{GOOS: "linux", GOARCH: "amd64"},
		{GOOS: "darwin", GOARCH: "arm64"},
		{GOOS: "windows", GOARCH: "amd64"},
	}}
	for _, ext := range []string{"", "tar.gz", "zip"} {
		t.Run("extension "+strconv.Quote(ext), func(t *testing.T) {
			t.Parallel()
			cfg := testConfig(t)
			cfg.AssetExt = ext
			var want []byte
			for _, goos := range []string{"", "linux", "windows", "darwin"} {
				cfg.GOOS = goos
				out := render(t, cfg, opts)
				if want == nil {
					want = out
					continue
				}
				if !bytes.Equal(out, want) {
					t.Errorf("the config rendered with GOOS %q differs from the one with an empty GOOS:\n%s\nwant:\n%s", goos, out, want)
				}
			}
		})
	}
}

func TestRenderAssetNamesDistinctPerPlatform(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		template  string
		platforms []goreleaser.Platform
	}{
		{
			name:      "one platform needs no platform in the name",
			template:  "{name}.{ext}",
			platforms: []goreleaser.Platform{{GOOS: "linux", GOARCH: "amd64"}},
		},
		{
			name:     "the os alone tells the platforms apart",
			template: "{name}_{os}.{ext}",
			platforms: []goreleaser.Platform{
				{GOOS: "linux", GOARCH: "amd64"},
				{GOOS: "darwin", GOARCH: "arm64"},
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := testConfig(t)
			cfg.AssetTemplate = tc.template
			opts := testOptions()
			opts.Platforms = tc.platforms
			renderParsed(t, cfg, opts)
		})
	}
}

func TestRenderPlatforms(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		platforms  []goreleaser.Platform
		wantGoos   []string
		wantGoarch []string
		wantIgnore []ignoredTarget
	}{
		{
			name:       "default",
			wantGoos:   []string{"linux", "darwin"},
			wantGoarch: []string{"amd64", "arm64"},
		},
		{
			name:       "one platform",
			platforms:  []goreleaser.Platform{{GOOS: "linux", GOARCH: "amd64"}},
			wantGoos:   []string{"linux"},
			wantGoarch: []string{"amd64"},
		},
		{
			name: "not rectangular",
			platforms: []goreleaser.Platform{
				{GOOS: "linux", GOARCH: "amd64"},
				{GOOS: "linux", GOARCH: "arm64"},
				{GOOS: "darwin", GOARCH: "arm64"},
				{GOOS: "windows", GOARCH: "amd64"},
			},
			wantGoos:   []string{"linux", "darwin", "windows"},
			wantGoarch: []string{"amd64", "arm64"},
			wantIgnore: []ignoredTarget{{Goos: "darwin", Goarch: "amd64"}, {Goos: "windows", Goarch: "arm64"}},
		},
		{
			name: "diagonal",
			platforms: []goreleaser.Platform{
				{GOOS: "linux", GOARCH: "amd64"},
				{GOOS: "darwin", GOARCH: "arm64"},
			},
			wantGoos:   []string{"linux", "darwin"},
			wantGoarch: []string{"amd64", "arm64"},
			wantIgnore: []ignoredTarget{{Goos: "linux", Goarch: "arm64"}, {Goos: "darwin", Goarch: "amd64"}},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			opts := testOptions()
			opts.Platforms = tc.platforms
			build := renderParsed(t, testConfig(t), opts).Builds[0]
			if !slices.Equal(build.Goos, tc.wantGoos) {
				t.Errorf("goos = %q, want %q", build.Goos, tc.wantGoos)
			}
			if !slices.Equal(build.Goarch, tc.wantGoarch) {
				t.Errorf("goarch = %q, want %q", build.Goarch, tc.wantGoarch)
			}
			if !slices.Equal(build.Ignore, tc.wantIgnore) {
				t.Errorf("ignore = %q, want %q", build.Ignore, tc.wantIgnore)
			}
		})
	}
}

func TestRenderDir(t *testing.T) {
	t.Parallel()

	t.Run("unset", func(t *testing.T) {
		t.Parallel()
		out := render(t, testConfig(t), testOptions())
		if dir := parse(t, out).Builds[0].Dir; dir != nil {
			t.Errorf("dir = %q, want no dir key", *dir)
		}
		if bytes.Contains(out, []byte("dir:")) {
			t.Errorf("the config holds a dir key:\n%s", out)
		}
	})
	t.Run("set", func(t *testing.T) {
		t.Parallel()
		opts := testOptions()
		opts.Dir = "tools/atk"
		dir := renderParsed(t, testConfig(t), opts).Builds[0].Dir
		if dir == nil || *dir != "tools/atk" {
			t.Errorf("dir = %v, want %q", dir, "tools/atk")
		}
	})
}

// TestRenderHostileValuesStayLiteral renders values holding YAML quoting characters, the tokens
// of the config source, template actions and environment variables, and checks each reaches
// GoReleaser as the literal it was given.
func TestRenderHostileValuesStayLiteral(t *testing.T) {
	t.Parallel()

	const (
		binary  = `atk"\ @@BINARY@@ {{ .Tag }} {{ "{" }} {{ ${HOME} $HOME } {`
		main    = "./cmd/@@MAIN@@\"\\{{ .Tag }}$HOME @@DIR_BEGIN@@\n@@SIGNED_END@@\t\u0085\u2028\ufeff"
		dir     = "@@DIR@@ \"quoted\" \\ {{ .Env.HOME }} ${PWD}\n@@DIR_END@@"
		keyPath = `keys/"k"\@@SIGNING_KEY@@{{ .Env.KEY }}${HOME}$HOME{.key`
		varPath = `example.com/a\b@@LDFLAGS@@{{.Tag}}${HOME}$x{.v`
	)
	cfg := testConfig(t)
	cfg.BinaryName = binary
	opts := goreleaser.Options{
		Main:           main,
		Dir:            dir,
		SigningKeyPath: keyPath,
		LinkVars:       goreleaser.LinkVars{Version: varPath, Commit: varPath + "c", Date: varPath + "d"},
	}
	conf := renderParsed(t, cfg, opts)
	build := conf.Builds[0]

	if build.Main != main {
		t.Errorf("main = %q, want %q", build.Main, main)
	}
	if build.Dir == nil || *build.Dir != dir {
		t.Errorf("dir = %v, want %q", build.Dir, dir)
	}
	if got := execute(t, build.Binary, nil); got != binary {
		t.Errorf("binary expands to %q, want %q", got, binary)
	}
	data := buildData{Tag: "v1.2.3", FullCommit: "abc", Date: "today"}
	for i, want := range [][2]string{{varPath, data.Tag}, {varPath + "c", data.FullCommit}, {varPath + "d", data.Date}} {
		path, value := linkFlag(t, execute(t, build.Ldflags[i+1], data))
		if path != want[0] || value != want[1] {
			t.Errorf("ldflags[%d] sets %q=%q, want %q=%q", i+1, path, value, want[0], want[1])
		}
	}
	if len(conf.Signs) != 1 || len(conf.Signs[0].Args) != 7 {
		t.Fatalf("signs = %+v, want one entry of seven args", conf.Signs)
	}
	env := map[string]string{"artifact": "checksums.txt", "signature": "checksums.txt.minisig"}
	if got := expandSignArg(t, conf.Signs[0].Args[2], env); got != keyPath {
		t.Errorf("signing key expands to %q, want %q", got, keyPath)
	}
	for i, want := range map[int]string{4: "checksums.txt", 6: "checksums.txt.minisig"} {
		if got := expandSignArg(t, conf.Signs[0].Args[i], env); got != want {
			t.Errorf("signs args[%d] expands to %q, want %q", i, got, want)
		}
	}
}

func TestRenderErrors(t *testing.T) {
	t.Parallel()

	windows := []goreleaser.Platform{{GOOS: "linux", GOARCH: "amd64"}, {GOOS: "windows", GOARCH: "amd64"}}
	tests := []struct {
		name string
		cfg  func(*update.Config)
		opts func(*goreleaser.Options)
		want error
	}{
		{
			name: "empty main",
			opts: func(o *goreleaser.Options) { o.Main = "" },
			want: goreleaser.ErrNoMain,
		},
		{
			name: "empty GOOS",
			opts: func(o *goreleaser.Options) { o.Platforms = []goreleaser.Platform{{GOARCH: "amd64"}} },
			want: goreleaser.ErrEmptyPlatform,
		},
		{
			name: "empty GOARCH",
			opts: func(o *goreleaser.Options) {
				o.Platforms = []goreleaser.Platform{{GOOS: "linux", GOARCH: "amd64"}, {GOOS: "linux"}}
			},
			want: goreleaser.ErrEmptyPlatform,
		},
		{
			name: "repeated platform",
			opts: func(o *goreleaser.Options) {
				o.Platforms = []goreleaser.Platform{
					{GOOS: "linux", GOARCH: "amd64"},
					{GOOS: "darwin", GOARCH: "arm64"},
					{GOOS: "linux", GOARCH: "amd64"},
				}
			},
			want: goreleaser.ErrDuplicatePlatform,
		},
		{
			name: "checksums by os",
			cfg:  func(c *update.Config) { c.ChecksumsAsset = "{name}_{os}_checksums.txt" },
			want: goreleaser.ErrPlatformChecksums,
		},
		{
			name: "checksums by arch",
			cfg:  func(c *update.Config) { c.ChecksumsAsset = "checksums_{arch}.txt" },
			want: goreleaser.ErrPlatformChecksums,
		},
		{
			name: "raw binary for windows",
			cfg:  func(c *update.Config) { c.AssetTemplate = "{name}_{os}_{arch}" },
			opts: func(o *goreleaser.Options) { o.Platforms = windows },
			want: goreleaser.ErrRawWindowsAsset,
		},
		{
			name: "asset template without the platform",
			cfg:  func(c *update.Config) { c.AssetTemplate = "{name}.{ext}" },
			want: goreleaser.ErrSharedAssetName,
		},
		{
			name: "asset template without the architecture of two platforms",
			cfg:  func(c *update.Config) { c.AssetTemplate = "{name}_{os}.{ext}" },
			opts: func(o *goreleaser.Options) {
				o.Platforms = []goreleaser.Platform{{GOOS: "linux", GOARCH: "amd64"}, {GOOS: "linux", GOARCH: "arm64"}}
			},
			want: goreleaser.ErrSharedAssetName,
		},
		{
			name: "version variable with a space",
			opts: func(o *goreleaser.Options) { o.LinkVars.Version = "main.ver sion" },
			want: goreleaser.ErrInvalidLinkVar,
		},
		{
			name: "commit variable with a double quote",
			opts: func(o *goreleaser.Options) { o.LinkVars.Commit = `main."commit` },
			want: goreleaser.ErrInvalidLinkVar,
		},
		{
			name: "date variable with a single quote",
			opts: func(o *goreleaser.Options) { o.LinkVars.Date = "main.'date" },
			want: goreleaser.ErrInvalidLinkVar,
		},
		{
			name: "variable with an equals sign",
			opts: func(o *goreleaser.Options) { o.LinkVars.Version = "main.v=1" },
			want: goreleaser.ErrInvalidLinkVar,
		},
		{
			name: "variable with a tab",
			opts: func(o *goreleaser.Options) { o.LinkVars.Version = "main.\tv" },
			want: goreleaser.ErrInvalidLinkVar,
		},
		{
			name: "binary name not UTF-8",
			cfg:  func(c *update.Config) { c.BinaryName = "atk\xff" },
			want: goreleaser.ErrNotUTF8,
		},
		{
			name: "asset template not UTF-8",
			cfg:  func(c *update.Config) { c.AssetTemplate = "\xfe{name}_{os}_{arch}.{ext}" },
			want: goreleaser.ErrNotUTF8,
		},
		{
			name: "main not UTF-8",
			opts: func(o *goreleaser.Options) { o.Main = "./cmd/\xff" },
			want: goreleaser.ErrNotUTF8,
		},
		{
			name: "dir not UTF-8",
			opts: func(o *goreleaser.Options) { o.Dir = "\xc3" },
			want: goreleaser.ErrNotUTF8,
		},
		{
			name: "platform not UTF-8",
			opts: func(o *goreleaser.Options) { o.Platforms = []goreleaser.Platform{{GOOS: "linux", GOARCH: "\xff"}} },
			want: goreleaser.ErrNotUTF8,
		},
		{
			name: "link variable not UTF-8",
			opts: func(o *goreleaser.Options) { o.LinkVars.Date = "main.\xff" },
			want: goreleaser.ErrNotUTF8,
		},
		{
			name: "signing key path not UTF-8",
			opts: func(o *goreleaser.Options) { o.SigningKeyPath = "\xff.key" },
			want: goreleaser.ErrNotUTF8,
		},
		{
			name: "invalid identity",
			cfg:  func(c *update.Config) { c.Name = "" },
			want: validate.ErrEmpty,
		},
		{
			name: "no signature policy",
			cfg:  func(c *update.Config) { c.TrustedKeys = nil },
			want: validate.ErrInvalid,
		},
		{
			name: "extension before the end of the asset template",
			cfg:  func(c *update.Config) { c.AssetTemplate = "{name}.{ext}_{os}_{arch}" },
			want: asset.ErrMisplacedExt,
		},
		{
			name: "asset template naming nothing but its extension",
			cfg:  func(c *update.Config) { c.AssetTemplate = ".{ext}" },
			want: asset.ErrEmptyTemplate,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := testConfig(t)
			if tc.cfg != nil {
				tc.cfg(&cfg)
			}
			opts := testOptions()
			if tc.opts != nil {
				tc.opts(&opts)
			}
			out, err := goreleaser.Render(cfg, opts)
			if !errors.Is(err, tc.want) {
				t.Fatalf("Render error = %v, want it to match %v", err, tc.want)
			}
			if out != nil {
				t.Errorf("Render returned %q with its error, want nil", out)
			}
			if !strings.HasPrefix(err.Error(), "goreleaser: ") {
				t.Errorf("Render error %q does not name the package", err)
			}
		})
	}
}
