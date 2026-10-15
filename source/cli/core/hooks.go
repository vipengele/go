package cli

import (
	"context"

	"go.vipengele.org/cli/spec"
)

// PreRunHook is called before the run function of the command an [App] executes. It receives
// the context and the [spec.Invocation] the run function is about to receive, with the command
// line parsed and its Output set, and cmd, the command that runs: its path from the root,
// the root's name included, and its own annotations. An error it returns stops the execution:
// neither the run function nor any [PostRunHook] is called, and the error is reported as a run
// error: it is mapped to an exit code as a run function's error is, so it may carry a
// [spec.CodedError] or match a registered exit code.
type PreRunHook func(ctx context.Context, inv *spec.Invocation, cmd spec.CommandInfo) error

// PostRunHook is called after the run function of the command an [App] executes returns. It
// receives what a [PreRunHook] receives and runErr, the error the run function returned. When
// runErr is not nil it is the error reported, whatever the hook returns. Otherwise an error the
// hook returns is reported, with [ExitError] even when it describes a usage problem. That error
// is opaque to exit-code selection: it is not unwrapped, so a [spec.CodedError] it wraps, a
// registered exit code it matches and a [spec.ReportedError] it carries have no effect.
type PostRunHook func(ctx context.Context, inv *spec.Invocation, cmd spec.CommandInfo, runErr error) error

// WithPreRun adds h to the hooks the App calls before every run function, after the ones added
// before it. The hooks run in the order they were added, and the first that returns an error
// stops the execution. No hook runs for --help, for a command without a run function, or for a
// command line the tree does not accept, --verbose with --quiet included. A nil h is ignored.
func WithPreRun(h PreRunHook) Option {
	return func(s *settings) {
		if h != nil {
			s.preRun = append(s.preRun, h)
		}
	}
}

// WithPostRun adds h to the hooks the App calls after every run function that was called,
// whether it returned an error or not. The hooks run in the reverse of the order they were
// added, so a hook added after another runs inside it, and every one of them runs even when an
// earlier one returns an error; the first such error is reported when the run function
// returned nil. They run in no case where [WithPreRun]'s hooks do not, nor after a pre-run
// hook's error. A nil h is ignored.
func WithPostRun(h PostRunHook) Option {
	return func(s *settings) {
		if h != nil {
			s.postRun = append(s.postRun, h)
		}
	}
}

// hookFuncs returns the hooks s holds as the adapter takes them.
func (s settings) hookFuncs() (
	pre []func(context.Context, *spec.Invocation, spec.CommandInfo) error,
	post []func(context.Context, *spec.Invocation, spec.CommandInfo, error) error,
) {
	for _, h := range s.preRun {
		pre = append(pre, h)
	}
	for _, h := range s.postRun {
		post = append(post, h)
	}
	return pre, post
}
