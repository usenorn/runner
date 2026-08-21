package cmd

import (
	"context"

	"github.com/spf13/cobra"

	"github.com/usenorn/runner/internal"
)

func newRunnerUninstallCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "uninstall",
		Short: "Take the runner back out of this machine's service manager",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return withInstaller(cmd, func(ctx context.Context, installer *internal.Installer) error {
				return installer.Uninstall(ctx)
			})
		},
	}
}
