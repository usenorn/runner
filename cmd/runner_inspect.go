package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/usenorn/runner/internal"
)

func newRunnerInspectCommand() *cobra.Command {
	var (
		confirm bool
		asJSON  bool
	)

	command := &cobra.Command{
		Use:   "inspect",
		Short: "Read this folder and connect it to Norn",
		RunE: func(cmd *cobra.Command, _ []string) error {
			root, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("read the current folder: %w", err)
			}

			return withInspection(cmd, func(ctx context.Context, inspection *internal.Inspection) error {
				return inspection.Inspect(ctx, internal.InspectOptions{
					Root:    root,
					Confirm: confirm,
					JSON:    asJSON,
				})
			})
		},
	}

	command.Flags().BoolVar(&confirm, "confirm", false, "accept what the scan finds without asking")
	command.Flags().BoolVar(&asJSON, "json", false, "write the scan as json")

	return command
}
