// Greet is an example tool built on the released projects of this repository, used as a consumer
// uses them: its go.mod requires go.vipengele.org/cli/core, go.vipengele.org/cli/spec,
// go.vipengele.org/config and go.vipengele.org/update at their tags, with no replace, so it builds
// against exactly what "go get" fetches and never against a sibling's working tree. The project
// is never tagged; it exists to be read, run and quoted by the consumer guide,
// docs/guide/consumer.md.
//
// It runs one command of its own:
//
//	greet hello [name] [--shout]
//
// which greets name, "world" when it is omitted, with the word the configuration key "greeting"
// holds, "Hello" by default. It also holds the "config" group of go.vipengele.org/cli/core, the
// "update" command of go.vipengele.org/update/updatecmd and "version", and runs a background
// update check around every command that does not carry the spec.SkipUpdateCheck annotation,
// which "update" and "version" do.
//
// Regions of the source between a "snippet:start <name>" comment and the next "snippet:end"
// comment are the excerpts the guide quotes, with the blank lines at their edges and their common
// indentation stripped. Each one reads on its own, and a name is unique within its file.
//
// # Moving to a new release
//
// The example follows the latest release of go.vipengele.org/update. When update releases, one
// pull request moves together: the requires in go.mod, the commit SHA in the project's
// release.yml and the comment naming the release beside it, the project's .goreleaser.yaml,
// re-rendered for the release, and every "@vX.Y.Z" in docs/guide/consumer.md. The project's tests
// fail until all of them name the same release, so a Dependabot pull request that bumps go.mod
// alone goes red by design.
package main
