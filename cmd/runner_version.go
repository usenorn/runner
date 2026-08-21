package cmd

import (
	"context"

	"github.com/spf13/cobra"

	"github.com/usenorn/runner/internal"
)

func newRunnerVersionCommand() *cobra.Command {
	var (
		asJSON  bool
		noCheck bool
	)

	command := &cobra.Command{
		Use:   "version",
		Short: "Say which build this is and whether a newer one has been released",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return withVersion(cmd, func(ctx context.Context, version *internal.Version) error {
				return version.Report(ctx, asJSON, !noCheck)
			})
		},
	}

	command.Flags().BoolVar(&asJSON, "json", false, "write the report as json")
	command.Flags().BoolVar(&noCheck, "no-check", false, "do not ask whether a newer release exists")

	return command
}
