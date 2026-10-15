package versioncmd

import (
	"context"
	"encoding/json"
	"fmt"

	"go.vipengele.org/cli/core/buildinfo"
	"go.vipengele.org/cli/spec"
)

// Option configures the command [New] returns.
type Option func(*settings)

// settings holds what the [Option]s given to [New] set.
type settings struct {
	// outputJSON reads JSON from the invocation's Output instead of a local --json flag.
	outputJSON bool
}

// WithOutputJSON makes the command declare no --json flag of its own and write JSON when
// [spec.Output.JSON] reports it, as it does under an inherited --json the root declares, such
// as the standard flag of an App built with cli.WithJSON. A local --json under an inherited one
// makes the tree invalid.
func WithOutputJSON() Option {
	return func(s *settings) { s.outputJSON = true }
}

// New returns the "version" command, which prints info as text or, with --json, as JSON. By
// default --json is a local flag of the command; [WithOutputJSON] takes it from the Output
// instead. The command carries the [spec.SkipUpdateCheck] annotation: it reports the binary that
// runs, and the self-update preflight runs it on a binary not yet installed.
func New(info buildinfo.Info, opts ...Option) *spec.Command {
	var s settings
	for _, opt := range opts {
		opt(&s)
	}
	cmd := &spec.Command{
		Name:        "version",
		Summary:     "Print the version and build information",
		Args:        spec.NoArgs(),
		Annotations: map[string]string{spec.SkipUpdateCheck: ""},
	}
	asJSON := func(inv *spec.Invocation) bool { return inv.Output.JSON() }
	if !s.outputJSON {
		flag := spec.BoolFlag("json", "print the build information as JSON")
		cmd.Flags = []spec.Flag{flag}
		asJSON = flag.Get
	}
	cmd.Run = func(_ context.Context, inv *spec.Invocation) error {
		if !asJSON(inv) {
			_, err := fmt.Fprintf(inv.Env.Stdout, "%s\n", info.String())
			return err
		}
		b, err := json.Marshal(info)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(inv.Env.Stdout, "%s\n", b)
		return err
	}
	return cmd
}
