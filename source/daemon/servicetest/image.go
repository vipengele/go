package servicetest

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"

	_ "embed"
)

// containerfile is the image definition: Debian stable pinned by digest, with
// systemd, dbus and the lingering account svc.
//
//go:embed Containerfile
var containerfile []byte

// imageRepository is the local repository every servicetest image is tagged
// in.
const imageRepository = "localhost/servicetest"

// imageTag returns the tag of the image built from file with the BASE
// override base, empty for the default the file names. It is a hash of both,
// so a change to either names a new image and an unchanged pair reuses the
// image already built.
func imageTag(file []byte, base string) string {
	h := sha256.New()
	h.Write(file)
	h.Write([]byte{0})
	h.Write([]byte(base))
	return imageRepository + ":" + hex.EncodeToString(h.Sum(nil))[:16]
}

// ensureImage returns the tag of the servicetest image for base, building it
// unless podman already holds it. The build context is dir, which holds only
// the Containerfile: the image copies nothing in.
func (h harness) ensureImage(ctx context.Context, base, dir string) (string, error) {
	tag := imageTag(containerfile, base)
	// Any failure of `image exists`, a missing image or not, falls through to
	// the build, which reports the real cause.
	if _, err := h.r.run(ctx, nil, "image", "exists", tag); err == nil {
		return tag, nil
	}
	file := filepath.Join(dir, "Containerfile")
	if err := os.WriteFile(file, containerfile, 0o600); err != nil {
		return "", fmt.Errorf("servicetest: write the Containerfile: %w", err) // [lydite:exclude_from_mutation][prepare fails the test without reading the tag when the error is set, so the tag returned with it is unobservable]
	}
	args := []string{"build", "--tag", tag, "--file", file}
	if base != "" {
		args = append(args, "--build-arg", "BASE="+base)
	}
	args = append(args, dir)
	if _, err := h.r.run(ctx, nil, args...); err != nil {
		return "", fmt.Errorf("servicetest: build image %s: %w", tag, err) // [lydite:exclude_from_mutation][prepare fails the test without reading the tag when the error is set, so the tag returned with it is unobservable]
	}
	return tag, nil
}
