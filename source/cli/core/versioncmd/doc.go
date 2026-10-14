// Package versioncmd provides the "version" command, which prints the [buildinfo.Info] of the
// running binary.
//
// [New] returns a [github.com/spf13/cobra.Command] that a binary adds to its root command. It
// takes no arguments and writes to the command's output stream.
//
// # Text
//
// Without flags the command writes [buildinfo.Info.String] and a newline:
//
//	v1.0.0 build 0123abcdef01
//
// # JSON
//
// With --json the command writes the JSON encoding of the Info that [buildinfo.Info.MarshalJSON]
// specifies, as one object on one line followed by a newline. --json is a local flag of the
// command: it is declared on the command itself, not as a persistent flag, so no other command of
// the binary inherits it.
package versioncmd
