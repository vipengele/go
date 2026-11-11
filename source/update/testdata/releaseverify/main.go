// Command releaseverify checks a GoReleaser dist directory against the fixture tool's
// update.Config: releaseverify <dist dir> <public key file> <tag>. It prints OK and exits 0 when
// the release verifies, and prints the failure to stderr and exits 1 otherwise.
package main

import (
	"fmt"
	"os"

	"go.vipengele.org/update/internal/minisign"
	"go.vipengele.org/update/internal/releasetest"
)

func main() {
	if len(os.Args) != 4 {
		fmt.Fprintln(os.Stderr, "usage: releaseverify <dist dir> <public key file> <tag>")
		os.Exit(2)
	}
	data, err := os.ReadFile(os.Args[2])
	if err != nil {
		fmt.Fprintln(os.Stderr, "reading the public key:", err)
		os.Exit(1)
	}
	key, err := minisign.ParsePublicKey(string(data))
	if err != nil {
		fmt.Fprintf(os.Stderr, "parsing the public key %s: %v\n", os.Args[2], err)
		os.Exit(1)
	}
	if err := releasetest.Verify(os.Args[1], key, os.Args[3], releasetest.FixtureConfig()); err != nil {
		fmt.Fprintln(os.Stderr, "release verification failed:", err)
		os.Exit(1)
	}
	fmt.Println("OK")
}
