package goreleaser_test

import (
	"os"
	"slices"
	"strings"
	"testing"

	"go.vipengele.org/update"
	"go.vipengele.org/update/goreleaser"
)

// roundTripTag is the tag of the release each round trip publishes.
const roundTripTag = "v1.2.3"

// archiveData carries the fields a GoReleaser archive name_template reads. Version is the tag
// without its leading "v", as GoReleaser sets it.
type archiveData struct {
	ProjectName string
	Version     string
	Tag         string
	Os          string
	Arch        string
}

// checksumData carries the fields a GoReleaser checksum name_template reads. It has no Os or Arch:
// GoReleaser writes one checksums file per release, so a template reading either fails to execute.
type checksumData struct {
	ProjectName string
	Version     string
	Tag         string
}

// targets returns the platforms a GoReleaser build builds: every goos×goarch pair but the ignored
// ones.
func targets(build buildConfig) []goreleaser.Platform {
	var out []goreleaser.Platform
	for _, o := range build.Goos {
		for _, a := range build.Goarch {
			if !slices.Contains(build.Ignore, ignoredTarget{Goos: o, Goarch: a}) {
				out = append(out, goreleaser.Platform{GOOS: o, GOARCH: a})
			}
		}
	}
	return out
}

// samePlatforms reports whether a and b hold the same platforms, in any order.
func samePlatforms(a, b []goreleaser.Platform) bool {
	key := func(p goreleaser.Platform) string { return p.GOOS + "/" + p.GOARCH }
	ka := make([]string, len(a))
	for i, p := range a {
		ka[i] = key(p)
	}
	kb := make([]string, len(b))
	for i, p := range b {
		kb[i] = key(p)
	}
	slices.Sort(ka)
	slices.Sort(kb)
	return slices.Equal(ka, kb)
}

// published is a release asset as GoReleaser names it, with the platform it was built for when it
// is an archive.
type published struct {
	asset    update.Asset
	platform goreleaser.Platform
}

// TestRenderRoundTrip publishes a release under the names the rendered config gives its
// artifacts, as GoReleaser derives them, and checks that the Config selects the archive, the
// checksums file and the signature of every platform from it.
func TestRenderRoundTrip(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		cfg  func(t *testing.T) update.Config
		opts goreleaser.Options
	}{
		{
			name: "default",
			cfg:  testConfig,
			opts: testOptions(),
		},
		{
			name: "version in the names",
			cfg: func(t *testing.T) update.Config {
				t.Helper()
				cfg := testConfig(t)
				cfg.AssetTemplate = "{name}_{version}_{os}_{arch}.{ext}"
				cfg.ChecksumsAsset = "{name}_{version}_checksums.txt"
				return cfg
			},
			opts: testOptions(),
		},
		{
			name: "tag in the names",
			cfg: func(t *testing.T) update.Config {
				t.Helper()
				cfg := testConfig(t)
				cfg.AssetTemplate = "{name}-{tag}-{os}-{arch}.{ext}"
				cfg.AssetExt = "tgz"
				cfg.ChecksumsAsset = "SHA256SUMS-{tag}"
				return cfg
			},
			opts: testOptions(),
		},
		{
			name: "zip with windows",
			cfg: func(t *testing.T) update.Config {
				t.Helper()
				cfg := testConfig(t)
				cfg.AssetExt = "zip"
				return cfg
			},
			opts: goreleaser.Options{Main: "./cmd/atk", Platforms: []goreleaser.Platform{
				{GOOS: "linux", GOARCH: "amd64"},
				{GOOS: "darwin", GOARCH: "arm64"},
				{GOOS: "windows", GOARCH: "amd64"},
				{GOOS: "windows", GOARCH: "arm64"},
			}},
		},
		{
			name: "default extension with windows",
			cfg:  testConfig,
			opts: goreleaser.Options{Main: "./cmd/atk", Platforms: []goreleaser.Platform{
				{GOOS: "linux", GOARCH: "amd64"},
				{GOOS: "linux", GOARCH: "arm64"},
				{GOOS: "darwin", GOARCH: "arm64"},
				{GOOS: "windows", GOARCH: "amd64"},
			}},
		},
		{
			name: "tar.gz with windows",
			cfg: func(t *testing.T) update.Config {
				t.Helper()
				cfg := testConfig(t)
				cfg.AssetExt = "tar.gz"
				return cfg
			},
			opts: goreleaser.Options{Main: "./cmd/atk", Platforms: []goreleaser.Platform{
				{GOOS: "linux", GOARCH: "amd64"},
				{GOOS: "windows", GOARCH: "amd64"},
				{GOOS: "windows", GOARCH: "arm64"},
			}},
		},
		{
			name: "raw binary",
			cfg: func(t *testing.T) update.Config {
				t.Helper()
				cfg := testConfig(t)
				cfg.AssetTemplate = "{name}-{os}-{arch}"
				return cfg
			},
			opts: testOptions(),
		},
		{
			name: "unsigned",
			cfg:  unsignedConfig,
			opts: testOptions(),
		},
		{
			name: "binary name differs from the identity name",
			cfg: func(t *testing.T) update.Config {
				t.Helper()
				cfg := testConfig(t)
				cfg.BinaryName = "atk-cli"
				return cfg
			},
			opts: testOptions(),
		},
		{
			name: "not rectangular",
			cfg:  testConfig,
			opts: goreleaser.Options{Main: "./cmd/atk", Platforms: []goreleaser.Platform{
				{GOOS: "linux", GOARCH: "amd64"},
				{GOOS: "linux", GOARCH: "arm64"},
				{GOOS: "darwin", GOARCH: "arm64"},
				{GOOS: "freebsd", GOARCH: "386"},
			}},
		},
		{
			name: "hostile names",
			cfg: func(t *testing.T) update.Config {
				t.Helper()
				cfg := testConfig(t)
				cfg.BinaryName = `atk"\{{ .Tag }}$HOME@@BINARY@@`
				cfg.AssetTemplate = `"q\@@ARCHIVE_NAME@@}$x_{name}_{version}_{os}_{arch}.{ext}`
				cfg.ChecksumsAsset = `{name}"\@@CHECKSUM_NAME@@}$HOME_{tag}.txt`
				return cfg
			},
			opts: testOptions(),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := tc.cfg(t)
			roundTrip(t, cfg, tc.opts)
		})
	}
}

// roundTrip renders cfg with opts, derives the names of the artifacts GoReleaser publishes from
// the parsed config, and checks that cfg selects them for each platform the config builds.
func roundTrip(t *testing.T, cfg update.Config, opts goreleaser.Options) {
	t.Helper()
	resolved, err := cfg.Resolved()
	if err != nil {
		t.Fatalf("Resolved: %v", err)
	}
	conf := renderParsed(t, cfg, opts)
	build := conf.Builds[0]

	if binary := execute(t, build.Binary, nil); binary != resolved.BinaryName {
		t.Errorf("binary = %q, want the BinaryName Extract matches, %q", binary, resolved.BinaryName)
	}

	built := targets(build)
	wantPlatforms := opts.Platforms
	if len(wantPlatforms) == 0 {
		wantPlatforms = []goreleaser.Platform{
			{GOOS: "linux", GOARCH: "amd64"},
			{GOOS: "linux", GOARCH: "arm64"},
			{GOOS: "darwin", GOARCH: "amd64"},
			{GOOS: "darwin", GOARCH: "arm64"},
		}
	}
	if !samePlatforms(built, wantPlatforms) {
		t.Fatalf("the config builds %v, want %v", built, wantPlatforms)
	}

	formats := archiveFormats(t, conf.Archives[0])
	version := strings.TrimPrefix(roundTripTag, "v")

	var assets []published
	for _, p := range built {
		name := execute(t, conf.Archives[0].NameTemplate, archiveData{
			ProjectName: conf.ProjectName, Version: version, Tag: roundTripTag, Os: p.GOOS, Arch: p.GOARCH,
		})
		if format := formats(p.GOOS); format != "binary" {
			name += "." + format
		}
		assets = append(assets, published{asset: releaseAsset(name), platform: p})
	}

	checksums := execute(t, conf.Checksum.NameTemplate, checksumData{
		ProjectName: conf.ProjectName, Version: version, Tag: roundTripTag,
	})
	assets = append(assets, published{asset: releaseAsset(checksums)})

	signed := len(conf.Signs) > 0
	if signed == resolved.AllowUnsigned {
		t.Fatalf("signs present = %t under AllowUnsigned = %t", signed, resolved.AllowUnsigned)
	}
	var signature string
	if signed {
		if got := conf.Signs[0].Signature; got != "${artifact}.minisig" {
			t.Errorf("signs signature = %q, want %q", got, "${artifact}.minisig")
		}
		signature = os.Expand(conf.Signs[0].Signature, func(name string) string {
			if name != "artifact" {
				t.Errorf("signs signature expands the variable %q", name)
			}
			return checksums
		})
		assets = append(assets, published{asset: releaseAsset(signature)})
	}

	release := update.Release{Tag: roundTripTag, URL: "https://example.com/releases/" + roundTripTag}
	for _, a := range assets {
		release.Assets = append(release.Assets, a.asset)
	}

	for _, a := range assets {
		if a.platform == (goreleaser.Platform{}) {
			continue
		}
		target := cfg
		target.GOOS, target.GOARCH = a.platform.GOOS, a.platform.GOARCH
		sel, err := target.Select(release)
		if err != nil {
			t.Errorf("Select for %s/%s: %v", a.platform.GOOS, a.platform.GOARCH, err)
			continue
		}
		if sel.Archive != a.asset {
			t.Errorf("Select for %s/%s archive = %+v, want %+v", a.platform.GOOS, a.platform.GOARCH, sel.Archive, a.asset)
		}
		if sel.Checksums != releaseAsset(checksums) {
			t.Errorf("Select for %s/%s checksums = %+v, want %q", a.platform.GOOS, a.platform.GOARCH, sel.Checksums, checksums)
		}
		wantSignature := update.Asset{}
		if signed {
			wantSignature = releaseAsset(signature)
		}
		if sel.Signature != wantSignature {
			t.Errorf("Select for %s/%s signature = %+v, want %+v", a.platform.GOOS, a.platform.GOARCH, sel.Signature, wantSignature)
		}
	}
}

// archiveFormats returns the format GoReleaser archives a build for a goos in, as the archive
// states it: the formats of its format_overrides entry for that goos, else its formats. It fails
// the test unless each holds exactly one format and no goos has two entries.
func archiveFormats(t *testing.T, archive archiveConfig) func(goos string) string {
	t.Helper()
	if len(archive.Formats) != 1 {
		t.Fatalf("formats = %q, want one", archive.Formats)
	}
	overrides := make(map[string]string, len(archive.FormatOverrides))
	for _, o := range archive.FormatOverrides {
		if len(o.Formats) != 1 {
			t.Fatalf("format_overrides for %q = %q, want one format", o.Goos, o.Formats)
		}
		if _, dup := overrides[o.Goos]; dup {
			t.Fatalf("format_overrides holds %q twice", o.Goos)
		}
		overrides[o.Goos] = o.Formats[0]
	}
	return func(goos string) string {
		if f, ok := overrides[goos]; ok {
			return f
		}
		return archive.Formats[0]
	}
}

// releaseAsset returns the release asset named name, with a download URL of its own.
func releaseAsset(name string) update.Asset {
	return update.Asset{Name: name, URL: "https://example.com/download/" + name, Size: int64(len(name))}
}
