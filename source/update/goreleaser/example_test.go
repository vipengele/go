package goreleaser_test

import (
	"fmt"

	"go.vipengele.org/cli/spec"
	"go.vipengele.org/update"
	"go.vipengele.org/update/goreleaser"
)

// A tool renders its .goreleaser.yaml from the same update.Config it updates itself with, so the
// names GoReleaser publishes are the ones update selects. This tool publishes unsigned zip
// archives for linux and windows on amd64.
func ExampleRender() {
	cfg := update.Config{
		Identity: spec.Identity{
			Name:      "atk",
			Owner:     "vipengele",
			Repo:      "agentic-toolkit",
			EnvPrefix: "ATK",
			DirName:   "agentic-toolkit",
		},
		AssetExt:      "zip",
		AllowUnsigned: true,
		// An explicit StateDir resolves without reading the environment; Render creates nothing.
		StateDir: "/tmp/state",
	}
	out, err := goreleaser.Render(cfg, goreleaser.Options{
		Main: "./cmd/atk",
		Platforms: []goreleaser.Platform{
			{GOOS: "linux", GOARCH: "amd64"},
			{GOOS: "windows", GOARCH: "amd64"},
		},
	})
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Print(string(out))
	// Output:
	// # GoReleaser v2.6 or later. Rendered by go.vipengele.org/update/goreleaser from the tool's
	// # update.Config: the build, archive, checksum and signature names below are the ones update
	// # selects, verifies and extracts.
	// version: 2
	// project_name: "atk"
	// builds:
	//   - binary: "atk"
	//     main: "./cmd/atk"
	//     env: ["CGO_ENABLED=0"]
	//     ldflags: ["-s -w", "-X go.vipengele.org/cli/core/buildinfo.version={{ .Tag }}", "-X go.vipengele.org/cli/core/buildinfo.commit={{ .FullCommit }}", "-X go.vipengele.org/cli/core/buildinfo.date={{ .Date }}"]
	//     goos: ["linux", "windows"]
	//     goarch: ["amd64"]
	//     ignore: []
	// archives:
	//   - name_template: "atk_{{ .Os }}_{{ .Arch }}"
	//     formats: ["zip"]
	// checksum:
	//   name_template: "checksums.txt"
	//   algorithm: sha256
}
