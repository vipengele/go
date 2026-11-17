// Greet is an example tool built on the released projects of this repository, used as a consumer
// uses them: its go.mod requires go.vipengele.org/cli/core, go.vipengele.org/cli/spec,
// go.vipengele.org/config and go.vipengele.org/update at their tags, with no replace, so it builds
// against exactly what "go get" fetches and never against a sibling's working tree. The project
// is never tagged; it exists to be read, run and quoted by the consumer guide,
// docs/guide/consumer.md.
//
// It runs one command of its own:
//
//	greet hello [name] [--shout | --whisper]
//
// which greets name, "world" when it is omitted, with the word the configuration key "greeting"
// holds, "Hello" by default, in capitals under --shout and in lower case under --whisper, never
// both. "hi" is an alias of "hello". It also holds the "config" group of
// go.vipengele.org/cli/core, the "update" command of go.vipengele.org/update/updatecmd and
// "version", and runs a background update check around every command that does not carry the
// spec.SkipUpdateCheck annotation, which "update" and "version" do.
//
// Regions of the source between a "snippet:start <name>" comment and the next "snippet:end"
// comment are the excerpts the guide quotes, with the blank lines at their edges and their common
// indentation stripped. Each one reads on its own, and a name is unique within its file.
//
// # Moving to a new release
//
// The example follows the latest release of each project it requires. When update releases, one
// pull request moves together: the requires in go.mod, the commit SHA in the project's
// release.yml and the comment naming the release beside it, the project's .goreleaser.yaml,
// re-rendered with "GOWORK=off go test -run TestReleaseGoreleaserConfig . -update", and in
// docs/guide/consumer.md every "go.vipengele.org/<path>@vX.Y.Z", the quoted copies of release.yml
// and .goreleaser.yaml, and the tag in the "git ls-remote" command that finds the pinned commit.
// A release of another required project moves its require and its "@vX.Y.Z" in the guide. The
// project's tests fail until go.mod, release.yml, .goreleaser.yaml and the guide's versions and
// quoted files agree, so a Dependabot pull request that bumps go.mod alone goes red by design; no
// test reads the "git ls-remote" tag, which is checked by hand.
package main
