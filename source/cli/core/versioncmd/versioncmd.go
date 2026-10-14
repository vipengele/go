package versioncmd

import (
	"context"
	"encoding/json"
	"fmt"

	"go.vipengele.org/cli/core/buildinfo"
	"go.vipengele.org/cli/spec"
)

// New returns the "version" command, which prints info as text or, with --json, as JSON.
func New(info buildinfo.Info) *spec.Command {
	asJSON := spec.BoolFlag("json", "print the build information as JSON")
	return &spec.Command{
		Name:    "version",
		Summary: "Print the version and build information",
		Args:    spec.NoArgs(),
		Flags:   []spec.Flag{asJSON},
		Run: func(_ context.Context, inv *spec.Invocation) error {
			if !asJSON.Get(inv) {
				_, err := fmt.Fprintf(inv.Env.Stdout, "%s\n", info.String())
				return err
			}
			b, err := json.Marshal(info)
			if err != nil {
				return err
			}
			_, err = fmt.Fprintf(inv.Env.Stdout, "%s\n", b)
			return err
		},
	}
}
