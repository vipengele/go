package cli

import (
	"context"

	"go.vipengele.org/cli/core/internal/cobra"
	"go.vipengele.org/cli/spec"
)

// ExecuteConfig is what [ExecuteArgsContext] runs a tree with beyond [ExecuteArgs].
type ExecuteConfig struct {
	// Registry are the exit-code matchers, in registration order.
	Registry []func(err error) (int, bool)
	// PreRun are the hooks called before the run function.
	PreRun []func(ctx context.Context, inv *spec.Invocation, cmd spec.CommandInfo) error
	// PostRun are the hooks called after the run function.
	PostRun []func(ctx context.Context, inv *spec.Invocation, cmd spec.CommandInfo, runErr error) error
}

// ExecuteArgsContext is [ExecuteArgs] with every run function receiving ctx, and the matchers
// and hooks of c.
func ExecuteArgsContext(ctx context.Context, env *spec.Env, root *spec.Command, args []string,
	c ExecuteConfig,
) int {
	registry := make(exitMatchers, 0, len(c.Registry))
	for _, m := range c.Registry {
		registry = append(registry, m)
	}
	cfg := cobra.Config{Output: outputResolver{}.resolve, PreRun: c.PreRun, PostRun: c.PostRun}
	return executeArgsCtx(ctx, env, root, args, cfg, registry)
}

// NewUsageError returns err as the error of a command line the tree does not accept.
func NewUsageError(err error) error {
	return &cobra.UsageError{Err: err}
}
