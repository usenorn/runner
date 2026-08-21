package cmd

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/usenorn/runner/internal"
	"github.com/usenorn/runner/internal/config"
)

func newRunnerCommand() *cobra.Command {
	runner := &cobra.Command{
		Use:   "runner",
		Short: "Run the runner on this machine and say what it is doing",
	}

	runner.AddCommand(
		newRunnerStartCommand(),
		newRunnerStatusCommand(),
		newRunnerConnectCommand(),
		newRunnerDisconnectCommand(),
		newRunnerInstallCommand(),
		newRunnerUninstallCommand(),
	)

	return runner
}

func withStatus(cmd *cobra.Command, run func(context.Context, *internal.Status) error) error {
	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	status, cleanup, err := internal.InitStatus(cfgFile, config.Overrides{})
	if err != nil {
		return err
	}
	defer cleanup()

	return run(ctx, status)
}

func withInstaller(cmd *cobra.Command, run func(context.Context, *internal.Installer) error) error {
	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	installer, cleanup, err := internal.InitInstaller(cfgFile, config.Overrides{})
	if err != nil {
		return err
	}
	defer cleanup()

	return run(ctx, installer)
}

func withBinding(cmd *cobra.Command, run func(context.Context, *internal.Binding) error) error {
	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	binding, cleanup, err := internal.InitBinding(cfgFile, config.Overrides{})
	if err != nil {
		return err
	}
	defer cleanup()

	return run(ctx, binding)
}
