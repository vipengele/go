package releasetest

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"go.vipengele.org/update"
	"go.vipengele.org/update/internal/minisign"
)

// File names GoReleaser writes into its dist directory beside the release's assets.
const (
	// artifactsFile lists every artifact of the build, each with its type and platform.
	artifactsFile = "artifacts.json"
	// metadataFile names the project, the tag and the version of the build.
	metadataFile = "metadata.json"
)

// binaryArtifact is the artifacts.json type of a binary a build produced, one per platform built.
const binaryArtifact = "Binary"

// signatureSuffix follows the checksums file's name in the name of its minisign signature.
const signatureSuffix = ".minisig"

var (
	// ErrUnsigned reports a Config under AllowUnsigned, whose release Verify cannot check.
	ErrUnsigned = errors.New("config allows unsigned releases")
	// ErrTagMismatch reports a dist directory built for a tag other than the one Verify was given.
	ErrTagMismatch = errors.New("built for another tag")
	// ErrNoPlatforms reports a dist directory whose artifacts.json lists no binary.
	ErrNoPlatforms = errors.New("no platform built")
	// ErrMalformedChecksums reports a checksums file holding a line that is not a sha256sum entry,
	// a name listed twice, or no entry at all.
	ErrMalformedChecksums = errors.New("malformed checksums file")
	// ErrDigestMismatch reports a file whose SHA-256 digest differs from the one its checksums file
	// lists, or a listed file that is missing.
	ErrDigestMismatch = errors.New("digest mismatch")
	// ErrNotListed reports a selected archive its checksums file does not list.
	ErrNotListed = errors.New("archive not listed in the checksums file")
)

// platform is a GOOS and GOARCH a release was built for.
type platform struct {
	goos, goarch string
}

// artifact is the part of an artifacts.json entry Verify reads.
type artifact struct {
	Name   string `json:"name"`
	Type   string `json:"type"`
	Goos   string `json:"goos"`
	Goarch string `json:"goarch"`
}

// metadata is the part of metadata.json Verify reads.
type metadata struct {
	Tag string `json:"tag"`
}

// Verify checks the GoReleaser dist directory dir as the release tagged tag of the tool cfg
// describes, with its checksums file signed by the minisign public key key. It returns nil when
// every check the package documentation lists passes, and otherwise the first failure, wrapping
// the [update.Config.Select] or [minisign.Verify] error or one of the package's sentinel errors.
// Every file Verify reads from dir, whatever name the checksums file or [update.Config.Select]
// gives it, is opened through an [os.Root] on dir, so none can lie outside it.
func Verify(dir string, key minisign.PublicKey, tag string, cfg update.Config) error {
	if cfg.AllowUnsigned {
		return fmt.Errorf("releasetest: %w", ErrUnsigned)
	}

	root, err := os.OpenRoot(dir)
	if err != nil {
		return fmt.Errorf("releasetest: %w", err)
	}
	defer func() { _ = root.Close() }()

	if err := checkTag(root, tag); err != nil {
		return err
	}

	platforms, err := builtPlatforms(root)
	if err != nil {
		return err
	}

	rel := distRelease(dir, root, tag)

	// archives maps the name of each checksums file Select picks to the archives selected with it.
	archives := make(map[string][]string)
	for _, p := range platforms {
		target := cfg
		target.GOOS, target.GOARCH = p.goos, p.goarch
		sel, err := target.Select(rel)
		if err != nil {
			return fmt.Errorf("releasetest: selecting the assets of %s/%s: %w", p.goos, p.goarch, err)
		}
		archives[sel.Checksums.Name] = append(archives[sel.Checksums.Name], sel.Archive.Name)
	}

	names := make([]string, 0, len(archives))
	for name := range archives {
		names = append(names, name)
	}
	slices.Sort(names)

	for _, name := range names {
		if err := verifyChecksums(root, name, key, archives[name]); err != nil {
			return err
		}
	}

	return nil
}

// checkTag fails unless the metadata.json in root names tag. The archive names alone cannot attest the
// tag: an AssetTemplate without {tag} or {version}, the default included, names the archives of
// every release alike.
func checkTag(root *os.Root, tag string) error {
	var meta metadata
	if err := readJSON(root, metadataFile, &meta); err != nil {
		return err
	}
	if meta.Tag != tag {
		return fmt.Errorf("releasetest: %s names the tag %q, want %q: %w", metadataFile, meta.Tag, tag, ErrTagMismatch)
	}
	return nil
}

// builtPlatforms returns the platforms the artifacts.json in root lists a binary for, each once, in the
// order first listed.
func builtPlatforms(root *os.Root) ([]platform, error) {
	var artifacts []artifact
	if err := readJSON(root, artifactsFile, &artifacts); err != nil {
		return nil, err
	}
	var platforms []platform
	for _, a := range artifacts {
		if a.Type != binaryArtifact {
			continue
		}
		p := platform{goos: a.Goos, goarch: a.Goarch}
		if p.goos == "" || p.goarch == "" {
			return nil, fmt.Errorf("releasetest: %s lists the binary %q with no goos or goarch", artifactsFile, a.Name)
		}
		if !slices.Contains(platforms, p) {
			platforms = append(platforms, p)
		}
	}
	if len(platforms) == 0 {
		return nil, fmt.Errorf("releasetest: %s: %w", artifactsFile, ErrNoPlatforms)
	}
	return platforms, nil
}

// readJSON decodes the JSON file name in root into v.
func readJSON(root *os.Root, name string, v any) error {
	data, err := root.ReadFile(name)
	if err != nil {
		return fmt.Errorf("releasetest: %w", err)
	}
	if err := json.Unmarshal(data, v); err != nil {
		return fmt.Errorf("releasetest: %s: %w", name, err)
	}
	return nil
}

// distRelease returns the release tagged tag whose assets are the entries at the top of root, the
// directory dir, other than directories: where GoReleaser writes the archives, the checksums file
// and its signature. A symlink is an asset like any file, and reading it through root fails when it
// leads out of the directory. A directory that cannot be listed has no assets, so Select reports
// the first one it looks for as missing.
func distRelease(dir string, root *os.Root, tag string) update.Release {
	names, _ := fs.Glob(root.FS(), "*")
	rel := update.Release{Tag: tag, Assets: []update.Asset{}}
	for _, name := range names {
		info, err := root.Lstat(name)
		if err != nil || info.IsDir() {
			continue
		}
		rel.Assets = append(rel.Assets, update.Asset{
			Name: name,
			URL:  filepath.Join(dir, name),
			Size: info.Size(),
		})
	}
	return rel
}

// verifyChecksums verifies the checksums file name in root: its signature with key, every digest
// it lists, and that it lists each of archives. A listed name that is not a file in root fails as a
// digest mismatch.
func verifyChecksums(root *os.Root, name string, key minisign.PublicKey, archives []string) error {
	data, err := root.ReadFile(name)
	if err != nil {
		return fmt.Errorf("releasetest: %w", err)
	}
	sig, err := root.ReadFile(name + signatureSuffix)
	if err != nil {
		return fmt.Errorf("releasetest: %w", err)
	}
	if _, err := minisign.Verify([]minisign.PublicKey{key}, data, sig); err != nil {
		return fmt.Errorf("releasetest: %s%s: %w", name, signatureSuffix, err)
	}

	digests, err := parseChecksums(data)
	if err != nil {
		return fmt.Errorf("releasetest: %s: %w", name, err)
	}

	listed := make([]string, 0, len(digests))
	for file := range digests {
		listed = append(listed, file)
	}
	slices.Sort(listed)

	for _, file := range listed {
		if err := checkDigest(root, file, digests[file]); err != nil {
			return fmt.Errorf("releasetest: %s lists %q: %w", name, file, err)
		}
	}

	for _, a := range archives {
		if _, ok := digests[a]; !ok {
			return fmt.Errorf("releasetest: %s: %q: %w", name, a, ErrNotListed)
		}
	}
	return nil
}

// checkDigest fails unless the file name exists in root and its SHA-256 digest is want.
func checkDigest(root *os.Root, name string, want []byte) error {
	f, err := root.Open(name)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrDigestMismatch, err)
	}
	defer func() { _ = f.Close() }()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	if got := h.Sum(nil); !bytes.Equal(got, want) {
		return fmt.Errorf("%w: sha256 %x, want %x", ErrDigestMismatch, got, want)
	}
	return nil
}

// parseChecksums returns the digest a checksums file in sha256sum's format lists for each name.
//
// Each line is `<64 hex digits><spaces or a '*' binary marker><file name>`, ending in LF or CRLF;
// an empty line is skipped. Stricter than the parser update downloads with, it fails on a line of
// any other shape, on a name listed twice, on a name that is not a bare file name, since GoReleaser
// lists the files at the top of dist, and on a file with no entry.
func parseChecksums(data []byte) (map[string][]byte, error) {
	const hexLen = 2 * sha256.Size

	digests := make(map[string][]byte)
	for i, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSuffix(line, "\r")
		if line == "" {
			continue
		}

		digest, err := hex.DecodeString(line[:min(len(line), hexLen)])
		if err != nil || len(digest) != sha256.Size {
			return nil, fmt.Errorf("%w: line %d does not start with a SHA-256 digest", ErrMalformedChecksums, i+1)
		}

		rest := line[hexLen:]
		name := strings.TrimLeft(rest, " *")
		if name == rest || name == "" {
			return nil, fmt.Errorf("%w: line %d has no file name after the digest", ErrMalformedChecksums, i+1)
		}
		name = strings.TrimPrefix(name, "./")
		if name == "." || name == ".." || strings.ContainsAny(name, `/\`) {
			return nil, fmt.Errorf("%w: line %d names %q, not a file at the top of dist", ErrMalformedChecksums, i+1, name)
		}
		if _, dup := digests[name]; dup {
			return nil, fmt.Errorf("%w: %q is listed twice", ErrMalformedChecksums, name)
		}
		digests[name] = digest
	}
	if len(digests) == 0 {
		return nil, fmt.Errorf("%w: no entry", ErrMalformedChecksums)
	}
	return digests, nil
}
