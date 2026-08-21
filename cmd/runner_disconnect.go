package cmd

import (
	"context"

	"github.com/spf13/cobra"

	"github.com/usenorn/runner/internal"
)

func newRunnerDisconnectCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "disconnect",
		Short: "Unbind this machine and clear its credentials",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return withBinding(cmd, func(ctx context.Context, binding *internal.Binding) error {
				return binding.Disconnect(ctx)
			})
		},
	}
}
