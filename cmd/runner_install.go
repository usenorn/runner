package cmd

import (
	"context"

	"github.com/spf13/cobra"

	"github.com/usenorn/runner/internal"
)

func newRunnerInstallCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "install",
		Short: "Register the runner with this machine's service manager",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return withInstaller(cmd, func(ctx context.Context, installer *internal.Installer) error {
				return installer.Install(ctx)
			})
		},
	}
}
