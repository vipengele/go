package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"go.vipengele.org/cli/core/internal/cobra"
	"go.vipengele.org/cli/core/internal/errrender"
	"go.vipengele.org/cli/spec"
	"go.vipengele.org/cli/spec/terminal"
)

// The exit codes [Execute] and [ExecuteArgs] return.
const (
	// ExitOK is the exit code of a run function that returns nil, and of --help.
	ExitOK = 0
	// ExitError is the exit code of a run function that returns an error, and of a tree that
	// [spec.Command.Validate] rejects.
	ExitError = 1
	// ExitUsage is the exit code of a command line the tree does not accept: an unknown flag, a
	// flag value that does not parse, an unknown subcommand, or a number of positional arguments
	// the command's [spec.ArgPolicy] rejects. It is also the exit code of a [spec.UsageError] a
	// run function or a hook returns, for a command line that cannot proceed without input the
	// user cannot give interactively.
	ExitUsage = 2
)

// Execute runs root with the arguments of the process, os.Args[1:], against the environment of
// the process: its standard streams, working directory and environment variables. It returns
// the exit code, which a tool's main passes to os.Exit:
//
//	func main() {
//		os.Exit(cli.Execute(newRoot()))
//	}
//
// It behaves as [ExecuteArgs] does, with SIGINT and SIGTERM as the values of
// [spec.Env.Interrupts]: the first cancels the run's context and the second returns
// [ExitInterrupted]. It returns [ExitError] after writing the error, as [ExecuteArgs] writes one,
// to os.Stderr when the working directory cannot be determined.
func Execute(root *spec.Command) int {
	return executeProcess(name(root), func(env *spec.Env, args []string) int {
		return ExecuteArgs(env, root, args)
	})
}

// executeProcess calls run with the environment and the arguments of the process, and returns
// its exit code. It returns [ExitError] without calling run after writing the error, with tool
// as its prefix and no renderers, to os.Stderr when the working directory cannot be determined:
// the failure precedes the run, so no option's renderer applies to it.
//
// While run runs, SIGINT and SIGTERM reach it as values of env.Interrupts instead of ending the
// process; once executeProcess returns, they have their default effect again.
func executeProcess(tool string, run func(env *spec.Env, args []string) int) int {
	env, err := processEnv()
	if err != nil {
		errrender.Render(os.Stderr, tool, err, nil)
		return ExitError
	}
	interrupts, stop := notifyInterrupts()
	defer stop()
	env.Interrupts = interrupts
	return run(env, os.Args[1:])
}

// notifyInterrupts registers for SIGINT and SIGTERM and starts the goroutine that forwards each
// of them as a value of interrupts. stop unregisters, asks the goroutine to exit and waits until
// it has. The goroutine never blocks the delivery of a signal: os/signal drops a signal its
// channel has no room for rather than wait, and every send to interrupts also watches for stop.
// interrupts holds two values, the most a run reacts to, so two signals in quick succession
// both reach it.
func notifyInterrupts() (interrupts <-chan struct{}, stop func()) {
	sigs := make(chan os.Signal, 2)
	signal.Notify(sigs, os.Interrupt, syscall.SIGTERM)
	interruptc := make(chan struct{}, 2)
	done := make(chan struct{})
	exited := make(chan struct{})
	go func() {
		defer close(exited)
		for {
			select {
			case <-sigs:
				select {
				case interruptc <- struct{}{}:
				case <-done:
					return
				}
			case <-done:
				return
			}
		}
	}()
	return interruptc, func() {
		signal.Stop(sigs)
		close(done)
		<-exited
	}
}

// processEnv returns the environment of the process.
func processEnv() (*spec.Env, error) {
	wd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	return &spec.Env{
		Stdin:     os.Stdin,
		Stdout:    os.Stdout,
		Stderr:    os.Stderr,
		WorkDir:   wd,
		LookupEnv: os.LookupEnv,
	}, nil
}

// ExecuteArgs runs root with args against env, and returns the exit code. It touches no part of
// the process that env does not give it: a nil env, or a nil field of env, holds the inert
// default [spec.Env.Resolved] gives it, and a nil args is no arguments. It registers no signal
// handler of its own.
//
// Every run function and hook receives one context, cancelled when ExecuteArgs returns. The
// first value env.Interrupts delivers cancels it and does nothing else: the run goes on, and what
// it returns decides the exit code as it does without an interrupt, except that
// [context.Canceled] then returns [ExitInterrupted] and is written nowhere. A run that returns nil
// returns [ExitOK], and any other error is written and keeps its code. The second value returns
// [ExitInterrupted] at once, without waiting for the run: the abandoned run goes on in the
// background until its run function returns, and may still call its PostRun hooks and write to
// env's streams, though ExecuteArgs writes nothing more for it. A nil env.Interrupts watches
// nothing and starts no goroutine.
//
// A panic in a run function or a hook propagates to the caller of ExecuteArgs with the same
// value, whether env.Interrupts is nil or not, after ExecuteArgs has stopped watching
// env.Interrupts. The panic of a run abandoned on a second interrupt is recovered and dropped:
// nothing waits for that run any more.
//
// It checks root with [spec.Command.Validate] on every call, and returns [ExitError] after
// writing "<name>: invalid command tree" to env.Stderr, followed by one row per problem found,
// when the tree is invalid. It then selects the command args name and calls its run function.
// --help, on any command, writes the command's help to env.Stdout and returns [ExitOK]. A command
// with subcommands and no Run, given no positional argument, does the same. There is no help or
// completion subcommand.
//
// Every error is written to env.Stderr as a tree, unless it carries a [spec.ReportedError]: the
// run function has then shown it already, and nothing is written. The tree's first line is
// "<name>: " and the text of the error's outermost layer, where name is root's Name, or that text
// alone when the name is empty. Each further layer of a wrapped error is a row marked "→"
// indented under the first, and each error of an [errors.Join] a row marked "•" indented under
// the layer it is joined beneath, itself printed as a tree:
//
//	tool: load config
//	  → validate tool.yaml
//	    • name: required
//	    • port: out of range
//
// A layer is the text an error adds before ": " and the message of the error it wraps; an error
// whose message does not end that way prints whole, with nothing beneath it. An error that
// neither wraps nor joins, a usage error and a [spec.UsageError], print on the first line alone,
// as "<name>: <error>". An [App] can render an error of its own type in its own way with
// [WithErrorRenderer]. The exit code of an error is, first match wins:
//   - [ExitInterrupted], written nowhere, when an interrupt cancelled the run's context and the
//     error is [context.Canceled];
//   - the code of a [spec.CodedError] the error carries, when it is 1 or 3 through 125; a code
//     outside that range is ignored;
//   - [ExitUsage] for a command line the tree does not accept, and for an error that carries a
//     [spec.UsageError];
//   - [ExitError] otherwise.
//
// A nil env.Terminal is filled with [terminal.Detect] of the resolved env's Stdin, Stdout and
// Stderr, so [spec.Env.IsTerminal] answers for a real file instead of panicking; a non-nil
// env.Terminal is used as is. The Terminal is set on the resolved copy, and env is not modified.
//
// Every run function receives a [spec.Invocation] whose Output decides colour for [spec.Stdout]
// and [spec.Stderr] separately, each as [spec.ColorAllowed] decides it from env with no
// --no-color flag and no identity, so no prefixed NO_COLOR variable is read.
//
// One tree may be run any number of times, concurrently: each call parses its own flag values.
func ExecuteArgs(env *spec.Env, root *spec.Command, args []string) int {
	return executeArgs(env, root, args, cobra.Config{Output: outputResolver{}.resolve})
}

// executeArgs is [ExecuteArgs] with the adapter configured by cfg.
func executeArgs(env *spec.Env, root *spec.Command, args []string, cfg cobra.Config) int {
	return executeArgsCtx(context.Background(), env, root, args, cfg, nil)
}

// executeArgsCtx is [ExecuteArgs] with the adapter configured by cfg, every run function
// receiving the run's context, derived from ctx, which the first value of env.Interrupts
// cancels, the matchers of registry giving exit codes to errors, and renderers offered each error
// of the tree it writes, in order. The exit code of an error is, first match wins:
//  1. [ExitInterrupted], written nowhere, when the run's context is done, because ctx is done or
//     an interrupt cancelled it, and the error is [context.Canceled]. A run function that cancels
//     or times out a context of its own while the run's context is not done is not interrupted;
//  2. the code of a [spec.CodedError] the error carries, when [registrableExitCode] accepts it;
//  3. the code of the first matcher of registry that matches the error;
//  4. [ExitUsage] for a [*cobra.UsageError], and for a [spec.UsageError] the error carries;
//  5. [ExitError].
//
// A [spec.UsageError] ranks as a [*cobra.UsageError] does: a [spec.CodedError] that carries it, or
// a matcher of registry that matches it, decides its code first, as for any other error.
// Outside rule 1, the error is written to env.Stderr by [errrender.Render] unless it carries a
// [spec.ReportedError], which changes no exit code. The invalid-tree error is written whatever it
// carries. An error a PostRun returns after the run function succeeds reaches rules 2 to 4 as a
// [*cobra.HookError], which unwraps to nothing, so its code is [ExitError], interrupted or not.
func executeArgsCtx(ctx context.Context, env *spec.Env, root *spec.Command, args []string,
	cfg cobra.Config, registry exitMatchers, renderers ...errrender.Renderer,
) int {
	env = env.Resolved()
	if env.Terminal == nil {
		env.Terminal = terminal.Detect(env.Stdin, env.Stdout, env.Stderr)
	}
	if err := root.Validate(); err != nil {
		errrender.Render(env.Stderr, name(root), fmt.Errorf("invalid command tree: %w", err), renderers)
		return ExitError
	}
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	if env.Interrupts == nil {
		return report(runCtx, env, root, cobra.Run(runCtx, env, root, args, cfg), registry, renderers)
	}

	second, stop := watchInterrupts(env.Interrupts, cancel)
	done := make(chan outcome, 1)
	go func() {
		defer func() {
			if v := recover(); v != nil {
				done <- outcome{panicked: true, value: v}
			}
		}()
		done <- outcome{err: cobra.Run(runCtx, env, root, args, cfg)}
	}()
	select {
	case out := <-done:
		stop()
		if out.panicked {
			panic(out.value)
		}
		return report(runCtx, env, root, out.err, registry, renderers)
	case <-second:
		return ExitInterrupted
	}
}

// outcome is how a run on its own goroutine ended: it returned err, or it panicked with value.
type outcome struct {
	err      error
	panicked bool
	value    any
}

// report writes err to env.Stderr and returns its exit code by the rules of [executeArgsCtx],
// runCtx being the run's context. Rule 1 reads runCtx rather than the context executeArgsCtx was
// given, so a run an interrupt cancelled is interrupted as one whose parent context is done is.
func report(runCtx context.Context, env *spec.Env, root *spec.Command, err error,
	registry exitMatchers, renderers []errrender.Renderer,
) int {
	if err == nil {
		return ExitOK
	}
	if runCtx.Err() != nil && errors.Is(err, context.Canceled) {
		return ExitInterrupted
	}
	var reported *spec.ReportedError
	if !errors.As(err, &reported) {
		errrender.Render(env.Stderr, name(root), err, renderers)
	}
	return exitCode(err, registry)
}

// exitCode returns the exit code of err, which is not an interruption, by rules 2 to 5 of
// [executeArgsCtx].
func exitCode(err error, registry exitMatchers) int {
	var coded *spec.CodedError
	if errors.As(err, &coded) && registrableExitCode(coded.Code()) {
		return coded.Code()
	}
	if code, ok := registry.code(err); ok {
		return code
	}
	var usage *cobra.UsageError
	if errors.As(err, &usage) {
		return ExitUsage
	}
	var runUsage *spec.UsageError
	if errors.As(err, &runUsage) {
		return ExitUsage
	}
	return ExitError
}

// watchInterrupts starts the goroutine that watches interrupts for one run. It calls cancel on
// the first value and closes second on the next one, then exits. stop asks it to exit and waits
// until it has, so every cancel it makes happens before stop returns. stop is called at most
// once. A closed interrupts delivers no more values.
func watchInterrupts(interrupts <-chan struct{}, cancel context.CancelFunc) (
	second <-chan struct{}, stop func(),
) {
	secondc := make(chan struct{})
	stopc := make(chan struct{})
	exited := make(chan struct{})
	go func() {
		defer close(exited)
		seen := false
		for {
			select {
			case _, ok := <-interrupts:
				if !ok {
					interrupts = nil
					continue
				}
				if seen {
					close(secondc)
					return
				}
				seen = true
				cancel()
			case <-stopc:
				return
			}
		}
	}()
	return secondc, func() {
		close(stopc)
		<-exited
	}
}

// name returns root's name, and "" for a nil root.
func name(root *spec.Command) string {
	if root == nil {
		return ""
	}
	return root.Name
}
