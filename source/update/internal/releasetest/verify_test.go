package releasetest_test

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.vipengele.org/cli/spec"
	"go.vipengele.org/update"
	"go.vipengele.org/update/internal/minisign"
	"go.vipengele.org/update/internal/minisigntest"
	"go.vipengele.org/update/internal/releasetest"
)

// testTag is the tag every fixture release is built for.
const testTag = "v1.2.3"

// checksumsName is the checksums file GoReleaser writes for the default ChecksumsAsset.
const checksumsName = "checksums.txt"

// build is one platform of a fixture release and the name of its archive.
type build struct {
	goos, goarch, archive string
}

// defaultBuilds are the platforms of a fixture release, with their archives named as the default
// AssetTemplate and AssetExt name them.
func defaultBuilds() []build {
	return []build{
		{goos: "linux", goarch: "amd64", archive: "atk_linux_amd64.tar.gz"},
		{goos: "darwin", goarch: "arm64", archive: "atk_darwin_arm64.tar.gz"},
		{goos: "windows", goarch: "amd64", archive: "atk_windows_amd64.zip"},
	}
}

// fixture is a dist directory as GoReleaser leaves it, and the key it is signed with.
type fixture struct {
	dir    string
	pubkey string
	key    *minisigntest.Key
}

// newFixture writes a dist directory holding the archives of builds, artifacts.json, metadata.json
// naming tag, the checksums file listing every archive and its signature by a fresh key, whose
// public key it writes to a file outside the directory.
func newFixture(t *testing.T, tag string, builds []build) fixture {
	t.Helper()
	key, err := minisigntest.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	f := fixture{dir: t.TempDir(), pubkey: filepath.Join(t.TempDir(), "minisign.pub"), key: key}
	writeFile(t, f.pubkey, []byte(key.PublicKey()))

	var artifacts []map[string]string
	var sums strings.Builder
	for _, b := range builds {
		content := []byte("archive of " + b.goos + "/" + b.goarch)
		writeFile(t, filepath.Join(f.dir, b.archive), content)
		binDir := "atk_" + b.goos + "_" + b.goarch
		writeFile(t, filepath.Join(f.dir, binDir, "atk"), []byte("binary"))
		artifacts = append(artifacts,
			map[string]string{"name": "atk", "path": "dist/" + binDir + "/atk", "goos": b.goos, "goarch": b.goarch, "type": "Binary"},
			map[string]string{"name": b.archive, "path": "dist/" + b.archive, "goos": b.goos, "goarch": b.goarch, "type": "Archive"},
		)
		sum := sha256.Sum256(content)
		sums.WriteString(hex.EncodeToString(sum[:]) + "  " + b.archive + "\n")
	}
	artifacts = append(artifacts, map[string]string{"name": checksumsName, "path": "dist/" + checksumsName, "type": "Checksum"})
	writeJSON(t, filepath.Join(f.dir, "artifacts.json"), artifacts)
	writeJSON(t, filepath.Join(f.dir, "metadata.json"), map[string]string{"project_name": "atk", "tag": tag, "version": strings.TrimPrefix(tag, "v")})

	f.writeChecksums(t, []byte(sums.String()))
	return f
}

// writeChecksums replaces the checksums file with data and signs it with the fixture's key.
func (f fixture) writeChecksums(t *testing.T, data []byte) {
	t.Helper()
	writeFile(t, filepath.Join(f.dir, checksumsName), data)
	writeFile(t, filepath.Join(f.dir, checksumsName+".minisig"), f.key.Sign(data, "timestamp:1"))
}

// testConfig returns a Config trusting key, with every name at its default.
func testConfig(t *testing.T, key *minisigntest.Key) update.Config {
	t.Helper()
	return update.Config{
		Identity: spec.Identity{
			Name:      "atk",
			Owner:     "vipengele",
			Repo:      "agentic-toolkit",
			EnvPrefix: "ATK",
			DirName:   "agentic-toolkit",
		},
		TrustedKeys: []string{key.PublicKeyLine()},
		StateDir:    t.TempDir(),
	}
}

func writeFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeJSON(t *testing.T, path string, v any) {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, path, data)
}

func TestVerifyPasses(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		cfg    func(update.Config) update.Config
		builds []build
	}{
		{
			name:   "default names",
			cfg:    func(c update.Config) update.Config { return c },
			builds: defaultBuilds(),
		},
		{
			name: "version in the archive names",
			cfg: func(c update.Config) update.Config {
				c.AssetTemplate = "{name}_{version}_{os}_{arch}.{ext}"
				return c
			},
			builds: []build{
				{goos: "linux", goarch: "amd64", archive: "atk_1.2.3_linux_amd64.tar.gz"},
				{goos: "windows", goarch: "arm64", archive: "atk_1.2.3_windows_arm64.zip"},
			},
		},
		{
			name: "raw binaries",
			cfg: func(c update.Config) update.Config {
				c.AssetTemplate = "{name}-{tag}-{os}-{arch}"
				return c
			},
			builds: []build{
				{goos: "linux", goarch: "amd64", archive: "atk-v1.2.3-linux-amd64"},
				{goos: "darwin", goarch: "arm64", archive: "atk-v1.2.3-darwin-arm64"},
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newFixture(t, testTag, tc.builds)
			if err := releasetest.Verify(f.dir, f.pubkey, testTag, tc.cfg(testConfig(t, f.key))); err != nil {
				t.Fatalf("Verify: %v", err)
			}
		})
	}
}

func TestVerifyWrongChecksumsName(t *testing.T) {
	t.Parallel()
	f := newFixture(t, testTag, defaultBuilds())
	cfg := testConfig(t, f.key)
	cfg.ChecksumsAsset = "SHA256SUMS"

	err := releasetest.Verify(f.dir, f.pubkey, testTag, cfg)
	var notFound *update.AssetNotFoundError
	if !errors.As(err, &notFound) || notFound.Name != "SHA256SUMS" {
		t.Fatalf("Verify = %v, want an *update.AssetNotFoundError for %q", err, "SHA256SUMS")
	}
}

func TestVerifyBadDigest(t *testing.T) {
	t.Parallel()
	f := newFixture(t, testTag, defaultBuilds())
	writeFile(t, filepath.Join(f.dir, "atk_darwin_arm64.tar.gz"), []byte("tampered"))

	err := releasetest.Verify(f.dir, f.pubkey, testTag, testConfig(t, f.key))
	if !errors.Is(err, releasetest.ErrDigestMismatch) || !strings.Contains(err.Error(), "atk_darwin_arm64.tar.gz") {
		t.Fatalf("Verify = %v, want ErrDigestMismatch naming the tampered archive", err)
	}
}

func TestVerifyListedFileMissing(t *testing.T) {
	t.Parallel()
	f := newFixture(t, testTag, defaultBuilds())
	data, err := os.ReadFile(filepath.Join(f.dir, checksumsName))
	if err != nil {
		t.Fatal(err)
	}
	f.writeChecksums(t, append(data, []byte(strings.Repeat("ab", sha256.Size)+"  atk_freebsd_386.tar.gz\n")...))

	err = releasetest.Verify(f.dir, f.pubkey, testTag, testConfig(t, f.key))
	if !errors.Is(err, releasetest.ErrDigestMismatch) || !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Verify = %v, want ErrDigestMismatch wrapping a missing file", err)
	}
}

func TestVerifyMalformedChecksums(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"not a digest":      "not a checksums line\n",
		"no name":           strings.Repeat("ab", sha256.Size) + "\n",
		"name in a subdir":  strings.Repeat("ab", sha256.Size) + "  sub/atk_linux_amd64.tar.gz\n",
		"name listed twice": strings.Repeat("ab", sha256.Size) + "  a\n" + strings.Repeat("cd", sha256.Size) + "  a\n",
		"empty":             "",
	}
	for name, data := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := newFixture(t, testTag, defaultBuilds())
			f.writeChecksums(t, []byte(data))

			err := releasetest.Verify(f.dir, f.pubkey, testTag, testConfig(t, f.key))
			if !errors.Is(err, releasetest.ErrMalformedChecksums) {
				t.Fatalf("Verify = %v, want ErrMalformedChecksums", err)
			}
		})
	}
}

func TestVerifyArchiveNotListed(t *testing.T) {
	t.Parallel()
	f := newFixture(t, testTag, defaultBuilds())
	data, err := os.ReadFile(filepath.Join(f.dir, checksumsName))
	if err != nil {
		t.Fatal(err)
	}
	var kept []string
	for _, line := range strings.SplitAfter(string(data), "\n") {
		if !strings.Contains(line, "atk_linux_amd64") {
			kept = append(kept, line)
		}
	}
	f.writeChecksums(t, []byte(strings.Join(kept, "")))

	err = releasetest.Verify(f.dir, f.pubkey, testTag, testConfig(t, f.key))
	if !errors.Is(err, releasetest.ErrNotListed) {
		t.Fatalf("Verify = %v, want ErrNotListed", err)
	}
}

func TestVerifyBadSignature(t *testing.T) {
	t.Parallel()
	f := newFixture(t, testTag, defaultBuilds())
	writeFile(t, filepath.Join(f.dir, checksumsName+".minisig"), f.key.Sign([]byte("other checksums"), "timestamp:1"))

	err := releasetest.Verify(f.dir, f.pubkey, testTag, testConfig(t, f.key))
	var bad *minisign.BadSignatureError
	if !errors.As(err, &bad) {
		t.Fatalf("Verify = %v, want a *minisign.BadSignatureError", err)
	}
}

func TestVerifyUnknownKey(t *testing.T) {
	t.Parallel()
	f := newFixture(t, testTag, defaultBuilds())
	other, err := minisigntest.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, f.pubkey, []byte(other.PublicKey()))

	err = releasetest.Verify(f.dir, f.pubkey, testTag, testConfig(t, f.key))
	var unknown *minisign.UnknownKeyError
	if !errors.As(err, &unknown) || unknown.ID != f.key.ID {
		t.Fatalf("Verify = %v, want a *minisign.UnknownKeyError naming %s", err, f.key.ID)
	}
}

func TestVerifyMissingSignature(t *testing.T) {
	t.Parallel()
	f := newFixture(t, testTag, defaultBuilds())
	if err := os.Remove(filepath.Join(f.dir, checksumsName+".minisig")); err != nil {
		t.Fatal(err)
	}

	err := releasetest.Verify(f.dir, f.pubkey, testTag, testConfig(t, f.key))
	var notFound *update.AssetNotFoundError
	if !errors.As(err, &notFound) || notFound.Name != checksumsName+".minisig" {
		t.Fatalf("Verify = %v, want an *update.AssetNotFoundError for the signature", err)
	}
}

func TestVerifyMissingArchive(t *testing.T) {
	t.Parallel()
	f := newFixture(t, testTag, defaultBuilds())
	if err := os.Remove(filepath.Join(f.dir, "atk_windows_amd64.zip")); err != nil {
		t.Fatal(err)
	}

	err := releasetest.Verify(f.dir, f.pubkey, testTag, testConfig(t, f.key))
	var notFound *update.AssetNotFoundError
	if !errors.As(err, &notFound) || notFound.Name != "atk_windows_amd64.zip" {
		t.Fatalf("Verify = %v, want an *update.AssetNotFoundError for the windows archive", err)
	}
}

func TestVerifyArchiveNamedOtherwise(t *testing.T) {
	t.Parallel()
	builds := defaultBuilds()
	builds[1].archive = "atk_macos_arm64.tar.gz"
	f := newFixture(t, testTag, builds)

	err := releasetest.Verify(f.dir, f.pubkey, testTag, testConfig(t, f.key))
	var notFound *update.AssetNotFoundError
	if !errors.As(err, &notFound) || notFound.Name != "atk_darwin_arm64.tar.gz" {
		t.Fatalf("Verify = %v, want an *update.AssetNotFoundError for the archive Select names", err)
	}
}

func TestVerifyWrongTag(t *testing.T) {
	t.Parallel()
	f := newFixture(t, testTag, defaultBuilds())

	err := releasetest.Verify(f.dir, f.pubkey, "v1.2.4", testConfig(t, f.key))
	if !errors.Is(err, releasetest.ErrTagMismatch) {
		t.Fatalf("Verify = %v, want ErrTagMismatch", err)
	}
}

func TestVerifyArchiveNamesCarryAnotherTag(t *testing.T) {
	t.Parallel()
	f := newFixture(t, testTag, []build{
		{goos: "linux", goarch: "amd64", archive: "atk_1.2.2_linux_amd64.tar.gz"},
	})
	cfg := testConfig(t, f.key)
	cfg.AssetTemplate = "{name}_{version}_{os}_{arch}.{ext}"

	err := releasetest.Verify(f.dir, f.pubkey, testTag, cfg)
	var notFound *update.AssetNotFoundError
	if !errors.As(err, &notFound) || notFound.Name != "atk_1.2.3_linux_amd64.tar.gz" {
		t.Fatalf("Verify = %v, want an *update.AssetNotFoundError for the archive carrying %s", err, testTag)
	}
}

func TestVerifyNoPlatforms(t *testing.T) {
	t.Parallel()
	f := newFixture(t, testTag, defaultBuilds())
	writeJSON(t, filepath.Join(f.dir, "artifacts.json"), []map[string]string{{"name": checksumsName, "type": "Checksum"}})

	err := releasetest.Verify(f.dir, f.pubkey, testTag, testConfig(t, f.key))
	if !errors.Is(err, releasetest.ErrNoPlatforms) {
		t.Fatalf("Verify = %v, want ErrNoPlatforms", err)
	}
}

func TestVerifyRefusesUnsigned(t *testing.T) {
	t.Parallel()
	f := newFixture(t, testTag, defaultBuilds())
	cfg := testConfig(t, f.key)
	cfg.TrustedKeys = nil
	cfg.AllowUnsigned = true

	err := releasetest.Verify(f.dir, f.pubkey, testTag, cfg)
	if !errors.Is(err, releasetest.ErrUnsigned) {
		t.Fatalf("Verify = %v, want ErrUnsigned", err)
	}
}
