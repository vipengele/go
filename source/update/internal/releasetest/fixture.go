package releasetest

import (
	"go.vipengele.org/cli/spec"
	"go.vipengele.org/update"
	"go.vipengele.org/update/goreleaser"
)

// fixturePublicKey is a minisign public key entry whose secret half nobody holds. It is in
// TrustedKeys so that the Config is a signed one: goreleaser.Render then emits the signs block,
// and Verify refuses a Config that allows unsigned releases. The rendered .goreleaser.yaml does
// not contain the key, and Verify checks the signature against the public key it is given,
// not against TrustedKeys.
const fixturePublicKey = `untrusted comment: minisign public key: 1096E18282B5BA98
RWSYurWCguGWELDj+MML/4/868JEjT2tjElVXZ3VvAcydaJNoPoNtaCx`

// FixtureConfig returns the update.Config of the fixture tool in testdata/releasefixture. The
// golden test renders the committed .goreleaser.yaml from it and the verifier checks the dist
// directory GoReleaser builds from that file against it, so both read one definition.
func FixtureConfig() update.Config {
	return update.Config{
		Identity: spec.Identity{
			Name:      "releasefixture",
			Owner:     "vipengele",
			Repo:      "go",
			EnvPrefix: "RELEASEFIXTURE",
			DirName:   "releasefixture",
		},
		TrustedKeys: []string{fixturePublicKey},
		// An explicit StateDir resolves without reading the environment; nothing creates it.
		StateDir: "/tmp/releasefixture-state",
	}
}

// FixtureOptions returns the options the fixture's .goreleaser.yaml is rendered with. GoReleaser
// runs in the fixture's directory, so Main is that directory. The fixture is package main, so its
// version variable is main.version. The platforms cover both archive formats and a GOOS matrix
// with ignored pairs.
func FixtureOptions() goreleaser.Options {
	return goreleaser.Options{
		Main: "./",
		Platforms: []goreleaser.Platform{
			{GOOS: "linux", GOARCH: "amd64"},
			{GOOS: "linux", GOARCH: "arm64"},
			{GOOS: "darwin", GOARCH: "arm64"},
			{GOOS: "windows", GOARCH: "amd64"},
		},
		LinkVars: goreleaser.LinkVars{
			Version: "main.version",
			Commit:  "main.commit",
			Date:    "main.date",
		},
	}
}
