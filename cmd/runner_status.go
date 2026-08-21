package cmd

import (
	"context"

	"github.com/spf13/cobra"

	"github.com/usenorn/runner/internal"
)

func newRunnerStatusCommand() *cobra.Command {
	var asJSON bool

	command := &cobra.Command{
		Use:   "status",
		Short: "Say whether the runner is running and what it is bound to",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return withStatus(cmd, func(ctx context.Context, status *internal.Status) error {
				return status.Report(ctx, asJSON)
			})
		},
	}

	command.Flags().BoolVar(&asJSON, "json", false, "write the report as json")

	return command
}
