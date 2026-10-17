// Package update checks for and applies updates to a command-line tool.
//
// A tool describes itself with a [Config]: the [spec.Identity] it is declared with, the names of
// its binary and release assets, the directory its state lives in, and the [Source] that reports
// its releases. Every way the library reaches the outside world — HTTP, the clock, the target
// platform, the running executable, the environment and the user's home directory — is a
// field of Config, so a test substitutes any of them. A field left at its zero value takes its
// production default; a nil Source is the [GitHubSource] for the Identity's Owner and Repo.
//
// The package depends on no command-line framework.
package update
