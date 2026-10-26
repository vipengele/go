package updatetest

import (
	"crypto/sha256"
	"encoding/hex"
	"maps"
	"slices"
	"strings"
)

// DefaultChecksumsName is the name of the checksums file a release publishes when the updater's
// checksums template is left at its default.
const DefaultChecksumsName = "checksums.txt"

// Digest returns the SHA-256 digest of body as 64 lower-case hex digits.
func Digest(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

// ChecksumLine returns the checksums line for an asset called name with content body:
// `<sha256 hex>  <name>`, the digest in lower case and two spaces before the name, with no line
// ending.
func ChecksumLine(name string, body []byte) string {
	return Digest(body) + "  " + name
}

// Checksums returns the checksums file listing every asset of assets, keyed by name: one
// [ChecksumLine] per asset in name order, each ended by "\n". An empty assets renders an empty
// file.
func Checksums(assets map[string][]byte) []byte {
	var b strings.Builder
	for _, name := range slices.Sorted(maps.Keys(assets)) {
		b.WriteString(ChecksumLine(name, assets[name]))
		b.WriteByte('\n')
	}
	return []byte(b.String())
}
