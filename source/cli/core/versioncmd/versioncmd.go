package versioncmd

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	"go.vipengele.org/cli/core/buildinfo"
)

// New returns the "version" command, which prints info as text or, with --json, as JSON.
func New(info buildinfo.Info) *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "version",
		Short: "Print the version and build information",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if !asJSON {
				_, err := fmt.Fprintln(cmd.OutOrStdout(), info.String())
				return err
			}
			b, err := json.Marshal(info)
			if err != nil {
				return err
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "%s\n", b)
			return err
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "print the build information as JSON")
	return cmd
}
