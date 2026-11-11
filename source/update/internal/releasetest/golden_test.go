package releasetest_test

import (
	"flag"
	"os"
	"path/filepath"
	"testing"

	"go.vipengele.org/update/goreleaser"
	"go.vipengele.org/update/internal/releasetest"
)

// The flag is registered in init and read back by name: a package-level variable holding it would
// be mutable global state.
func init() {
	flag.Bool("update", false, "rewrite testdata/releasefixture/.goreleaser.yaml")
}

// TestFixtureGoldenConfig checks that the committed .goreleaser.yaml of the fixture tool is what
// goreleaser.Render emits for the fixture's Config and options, byte for byte.
func TestFixtureGoldenConfig(t *testing.T) {
	got, err := goreleaser.Render(releasetest.FixtureConfig(), releasetest.FixtureOptions())
	if err != nil {
		t.Fatal(err)
	}

	path := filepath.Join("..", "..", "testdata", "releasefixture", ".goreleaser.yaml")
	if flag.Lookup("update").Value.String() == "true" {
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Errorf("%s differs from the rendered config; run go test ./internal/releasetest -update\nrendered:\n%s\ncommitted:\n%s", path, got, want)
	}
}
