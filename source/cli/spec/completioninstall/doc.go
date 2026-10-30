// Package completioninstall installs the shell completion script of a command-line tool into the
// per-user location its shell reads.
//
// [Target] is a pure table of the completion file for each [Shell], zsh, bash, fish and
// powershell, chosen from an [Env] the caller has resolved. [Install] writes the script that
// `<exe> completion <shell>` prints to the path Target chooses, and [Snippet] renders the same
// table as a POSIX-sh fragment for a tool's install script; a test holds the paths the two choose
// equal.
//
// # Requirements
//
// Completion must be enabled in the tool, with core.WithCompletion(): a tool built without it has
// no `completion` command, and Install returns an error for the failed generation.
//
// # Files only
//
// Install writes one completion file and never edits an rc file or a profile. Where the shell does
// not read the file as written, [Location.Note] and [Result.Note] say what the user adds: the
// directory to fpath for zsh, a dot-source line in $PROFILE for powershell.
//
// # Opting out
//
// The installation is skipped, with a nil error and [ReasonDisabled], when the environment variable
// <PREFIX>_NO_COMPLETION is "1", as [spec.Identity.EnvVar] names it with [NoCompletionEnvSuffix],
// or when [Options.Disabled] is true. A consumer registers the flag [NoCompletionFlag] on its own
// command and passes its value as Disabled.
//
// # Seams
//
// Options carries the tool's [spec.Identity] and one optional function for each side effect: the
// operating system, the environment, the home directory, the Homebrew prefix, running the tool,
// writing the file and locating the executable. A zero value of each uses the real process.
//
// # Paths
//
// The paths in the table are in forward-slash form and are converted with filepath.FromSlash
// before a file is written. The package has no windows-specific file; the windows paths are
// covered by tests of the table with an injected GOOS.
//
// See docs/adr/0021-the-completion-installer-is-cli-spec-completioninstall-with-an-update-post-hook.md.
package completioninstall
