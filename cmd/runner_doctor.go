package cmd

import (
	"context"

	"github.com/spf13/cobra"

	"github.com/usenorn/runner/internal"
)

func newRunnerDoctorCommand() *cobra.Command {
	var asJSON bool

	command := &cobra.Command{
		Use:   "doctor",
		Short: "Check that this machine can build every connected folder inside the sandbox",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return withStatus(cmd, func(ctx context.Context, status *internal.Status) error {
				return status.Doctor(ctx, asJSON)
			})
		},
	}

	command.Flags().BoolVar(&asJSON, "json", false, "write the diagnosis as json")

	return command
}
