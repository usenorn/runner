package cmd

import (
	"context"

	"github.com/spf13/cobra"

	"github.com/usenorn/runner/internal"
)

func newRunnerExecutionsCommand() *cobra.Command {
	var asJSON bool

	command := &cobra.Command{
		Use:   "executions",
		Short: "List the runs this machine has taken",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return withExecutions(cmd, func(ctx context.Context, e *internal.Executions) error {
				return e.List(ctx, asJSON)
			})
		},
	}

	command.Flags().BoolVar(&asJSON, "json", false, "write the answer as json")

	return command
}

func newRunnerLogsCommand() *cobra.Command {
	var asJSON bool

	command := &cobra.Command{
		Use:   "logs <exec>",
		Short: "Show what happened in one run",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withExecutions(cmd, func(ctx context.Context, e *internal.Executions) error {
				return e.Logs(ctx, args[0], asJSON)
			})
		},
	}

	command.Flags().BoolVar(&asJSON, "json", false, "write the answer as json")

	return command
}
