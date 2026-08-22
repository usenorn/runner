package cmd

import (
	"context"

	"github.com/spf13/cobra"

	"github.com/usenorn/runner/internal"
)

func newRunnerPauseCommand() *cobra.Command {
	var asJSON bool

	command := &cobra.Command{
		Use:   "pause",
		Short: "Stop taking new work on this machine",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return withScheduling(cmd, func(ctx context.Context, s *internal.Scheduling) error {
				return s.Pause(ctx, asJSON)
			})
		},
	}

	command.Flags().BoolVar(&asJSON, "json", false, "write the answer as json")

	return command
}

func newRunnerResumeCommand() *cobra.Command {
	var asJSON bool

	command := &cobra.Command{
		Use:   "resume",
		Short: "Take work on this machine again",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return withScheduling(cmd, func(ctx context.Context, s *internal.Scheduling) error {
				return s.Resume(ctx, asJSON)
			})
		},
	}

	command.Flags().BoolVar(&asJSON, "json", false, "write the answer as json")

	return command
}
