// Package spec holds the framework-neutral pieces of a command-line tool that cli/core, update
// and daemon share. It depends on no command-line framework. This package performs no I/O: it
// imports no os and touches neither the environment nor the file system. The subpackage
// [go.vipengele.org/cli/spec/paths] resolves the platform directories a tool keeps its files in,
// and does touch the file system.
//
// [Identity] is the set of values fixed per tool.
//
// A [Command] describes a command as data: its name, descriptions, the positional arguments it
// accepts as an [ArgPolicy], its flags, its subcommands and the function that runs it. A flag is
// created by [BoolFlag], [StringFlag] or [IntFlag], which return a typed handle; the handle is
// listed in the Flags of the command that declares it, and its Get reads the flag's value from
// the [Invocation] the run function receives. An Invocation carries one execution's positional
// arguments, flag values and [Env], the standard streams, working directory and environment
// variables the run function reaches the process through, and the [Output] the adapter resolves
// for it: whether the execution is verbose or quiet, writes JSON and may use colour, which is
// decided for the standard output and the standard error separately by [ColorAllowed]. The
// [Styler] of a stream writes the glyphs and coloured text that decision allows.
//
// A command's Annotations are key-value metadata, such as [SkipUpdateCheck], that the framework
// and its hooks read through the [CommandInfo] of the command that runs.
//
// A tree of commands holds no per-execution state, so it can be run any number of times,
// concurrently, and by a test that substitutes the Env. An adapter in another module runs it:
// it checks the tree with [Command.Validate], maps each command and [Flag] to its framework,
// checks positional arguments with [ArgPolicy.Check], and builds each Invocation with
// [NewInvocation].
package spec
