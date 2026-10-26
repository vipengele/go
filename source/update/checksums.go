package update

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"strings"
)

// maxChecksumsBytes is the most bytes of a checksums file [parseChecksum] reads.
const maxChecksumsBytes = 1 << 20

// parseChecksum returns the SHA-256 digest the checksums file r lists for the asset name.
//
// Each line is `<64 hex digits><spaces or a '*' binary marker><file name>`, as sha256sum writes
// it. Hex digits are of either case, a line ends in LF or CRLF, a file name keeps any spaces it
// contains, and a leading "./" on a file name is dropped. A line of any other shape is skipped.
// name is matched exactly. Digests are compared as decoded bytes, so a name listed twice with the
// same digest in different case is not a conflict.
//
// It reads at most 1 MiB. A longer input is a [*SizeLimitError] whose URL is empty, because the
// parser does not know where r came from; the caller fills it in. A name listed with two different
// digests is a [*ChecksumConflictError] and a name not listed is a [*ChecksumNotFoundError].
func parseChecksum(r io.Reader, name string) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, maxChecksumsBytes+1))
	if err != nil {
		return nil, err
	}

	if len(data) > maxChecksumsBytes {
		return nil, &SizeLimitError{Limit: maxChecksumsBytes}
	}

	var found []byte

	for _, line := range strings.Split(string(data), "\n") {
		digest, entry, ok := parseChecksumLine(strings.TrimSuffix(line, "\r"))
		if !ok || entry != name {
			continue
		}

		if found != nil && !bytes.Equal(found, digest) {
			return nil, &ChecksumConflictError{Name: name}
		}

		found = digest
	}

	if found == nil {
		return nil, &ChecksumNotFoundError{Name: name}
	}

	return found, nil
}

// parseChecksumLine splits a checksums line into its decoded digest and file name. ok is false for
// a line that is not a 64-digit hex digest, at least one space or '*', and a non-empty name.
func parseChecksumLine(line string) (digest []byte, name string, ok bool) {
	const hexLen = 2 * sha256.Size

	if len(line) <= hexLen {
		return nil, "", false
	}

	digest, err := hex.DecodeString(line[:hexLen])
	if err != nil {
		return nil, "", false
	}

	rest := line[hexLen:]
	name = strings.TrimLeft(rest, " *")

	if name == rest {
		return nil, "", false
	}

	name = strings.TrimPrefix(name, "./")
	if name == "" {
		return nil, "", false
	}

	return digest, name, true
}
