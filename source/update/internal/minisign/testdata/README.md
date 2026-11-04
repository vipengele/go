# minisign interop vector

These files come from an implementation independent of this package and of
`internal/minisigntest`, so the verifier is checked against signatures it did not produce itself.

| File | Content |
|---|---|
| `minisign.pub` | the public key, as `minisign.pub` holds it: comment line and key line (id `1096E18282B5BA98`) |
| `checksums.txt` | the signed message |
| `checksums.txt.minisig` | the hashed (`ED`) signature of `checksums.txt` |
| `checksums.txt.legacy.minisig` | the legacy (`Ed`) signature of `checksums.txt` |

## Provenance

- Tool: [`aead.dev/minisign`](https://github.com/aead/minisign) v0.3.0, a Go implementation of
  minisign that does not share code with this repository, with `golang.org/x/crypto` v0.13.0, built
  with Go 1.24.7.
- The key pair comes from `minisign.GenerateKey` fed the SHA-256 of a fixed string, so the program
  writes the same four files on every run: Ed25519 signing is deterministic.
- `aead.dev/minisign` derives the key id from the public key's BLAKE2b-256 hash where `minisign -G`
  draws it at random; the verifier treats the id as opaque either way.

The program, run in a scratch module outside this repository with `go mod init scratch/interop`,
`go get aead.dev/minisign@v0.3.0`, `go mod tidy` and `go run . <output dir>`:

```go
// Command interop writes a minisign interop vector with aead.dev/minisign.
package main

import (
	"bytes"
	"crypto/sha256"
	"log"
	"os"
	"path/filepath"

	"aead.dev/minisign"
)

func main() {
	out := os.Args[1]
	seed := sha256.Sum256([]byte("go.vipengele.org/update minisign interop vector"))
	pub, priv, err := minisign.GenerateKey(bytes.NewReader(seed[:]))
	if err != nil {
		log.Fatal(err)
	}
	pubText, _ := pub.MarshalText()
	msg := []byte("0f343b0931126a20f133d67c2b018a3b5d5d8d6e8f3a1c2b4e6f708192a3b4c5  tool_1.2.3_linux_amd64.tar.gz\n" +
		"a1b2c3d4e5f60718293a4b5c6d7e8f90112233445566778899aabbccddeeff00  tool_1.2.3_darwin_arm64.tar.gz\n")
	hashed := minisign.NewReader(bytes.NewReader(msg))
	if _, err := hashed.Read(make([]byte, len(msg)+1)); err != nil {
		log.Fatal(err)
	}
	sigED := hashed.SignWithComments(priv, "timestamp:1767225600\tfile:checksums.txt\thashed", "signature from aead.dev/minisign")
	sigEd := minisign.SignWithComments(priv, msg, "timestamp:1767225600\tfile:checksums.txt", "signature from aead.dev/minisign")
	if !minisign.Verify(pub, msg, sigED) || !minisign.Verify(pub, msg, sigEd) {
		log.Fatal("self-check failed")
	}
	for name, b := range map[string][]byte{
		"minisign.pub":                 append(pubText, '\n'),
		"checksums.txt":                msg,
		"checksums.txt.minisig":        sigED,
		"checksums.txt.legacy.minisig": sigEd,
	} {
		if err := os.WriteFile(filepath.Join(out, name), b, 0o644); err != nil {
			log.Fatal(err)
		}
	}
}
```
