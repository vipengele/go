package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"go.vipengele.org/cli/core/internal/cobra"
	"go.vipengele.org/cli/spec"
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
	// the command's [spec.ArgPolicy] rejects.
	ExitUsage = 2
	// ExitInterrupted is the exit code of a run that returns an error after an interrupt reaches
	// it through [spec.Env.Interrupts], and of a run abandoned on a second interrupt: 128 plus the
	// number of SIGINT, as a shell reports a process an interrupt ended.
	ExitInterrupted = 130
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
// [ExitInterrupted]. It returns [ExitError] after writing "<name>: <error>" to os.Stderr when the
// working directory cannot be determined.
func Execute(root *spec.Command) int {
	return executeProcess(prefix(root), func(env *spec.Env, args []string) int {
		return ExecuteArgs(env, root, args)
	})
}

// executeProcess calls run with the environment and the arguments of the process, and returns
// its exit code. It returns [ExitError] without calling run after writing the error, preceded
// by errPrefix, to os.Stderr when the working directory cannot be determined.
//
// While run runs, SIGINT and SIGTERM reach it as values of env.Interrupts instead of ending the
// process; once executeProcess returns, they have their default effect again.
func executeProcess(errPrefix string, run func(env *spec.Env, args []string) int) int {
	env, err := processEnv()
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "%s%v\n", errPrefix, err)
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
// first value env.Interrupts delivers cancels it; a run that then returns an error, a PostRun
// hook's included, returns [ExitInterrupted] instead of [ExitError], a usage error still returns
// [ExitUsage], and a run that returns nil returns [ExitOK]. An interrupt pending in
// env.Interrupts when the run returns counts as one that reached it. The second value returns
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
// writing "<name>: invalid command tree: <error>" to env.Stderr when the tree is invalid. It
// then selects the command args name and calls its run function. Every error is written to
// env.Stderr as "<name>: <error>", where name is root's Name, and the exit code is [ExitUsage]
// for a command line the tree does not accept and [ExitError] for an error the run function
// returns. --help, on any command, writes the command's help to env.Stdout and returns
// [ExitOK]. A command with subcommands and no Run, given no positional argument, does the same.
// There is no help or completion subcommand.
//
// Every run function receives a [spec.Invocation] whose Output forbids colour when the
// environment variable NO_COLOR is set and not empty, or when env.Stdout is not a terminal.
//
// One tree may be run any number of times, concurrently: each call parses its own flag values.
func ExecuteArgs(env *spec.Env, root *spec.Command, args []string) int {
	return executeArgs(env, root, args, cobra.Config{Output: outputResolver{}.resolve})
}

// executeArgs is [ExecuteArgs] with the adapter configured by cfg.
func executeArgs(env *spec.Env, root *spec.Command, args []string, cfg cobra.Config) int {
	env = env.Resolved()
	if err := root.Validate(); err != nil {
		_, _ = fmt.Fprintf(env.Stderr, "%sinvalid command tree: %v\n", prefix(root), err)
		return ExitError
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if env.Interrupts == nil {
		return report(env, root, cobra.Run(ctx, env, root, args, cfg), false)
	}

	second, stop := watchInterrupts(env.Interrupts, cancel)
	done := make(chan outcome, 1)
	go func() {
		defer func() {
			if v := recover(); v != nil {
				done <- outcome{panicked: true, value: v}
			}
		}()
		done <- outcome{err: cobra.Run(ctx, env, root, args, cfg)}
	}()
	select {
	case out := <-done:
		interrupted := stop()
		if out.panicked {
			panic(out.value)
		}
		return report(env, root, out.err, interrupted)
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

// report writes err, preceded by root's name, to env.Stderr and returns its exit code:
// [ExitOK] for nil, [ExitUsage] for a command line the tree does not accept, [ExitInterrupted]
// for any other error when interrupted, and [ExitError] otherwise.
func report(env *spec.Env, root *spec.Command, err error, interrupted bool) int {
	if err == nil {
		return ExitOK
	}
	_, _ = fmt.Fprintf(env.Stderr, "%s%v\n", prefix(root), err)
	var usage *cobra.UsageError
	if errors.As(err, &usage) {
		return ExitUsage
	}
	if interrupted {
		return ExitInterrupted
	}
	return ExitError
}

// watchInterrupts starts the goroutine that watches interrupts for one run. It calls cancel on
// the first value and closes second on the next one, then exits. stop asks it to exit, waits
// until it has, and reports whether it received a value; a value pending in interrupts when
// stop is called counts as received. stop is called at most once. A closed interrupts delivers
// no more values.
func watchInterrupts(interrupts <-chan struct{}, cancel context.CancelFunc) (
	second <-chan struct{}, stop func() bool,
) {
	secondc := make(chan struct{})
	stopc := make(chan struct{})
	seenc := make(chan bool, 1)
	go func() {
		seen := false
		defer func() { seenc <- seen }()
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
				select {
				case _, ok := <-interrupts:
					seen = seen || ok
				default:
				}
				return
			}
		}
	}()
	return secondc, func() bool {
		close(stopc)
		return <-seenc
	}
}

// prefix returns "<name>: " for root's name, and "" for a nil root or an empty name.
func prefix(root *spec.Command) string {
	if root == nil || root.Name == "" {
		return ""
	}
	return root.Name + ": "
}
