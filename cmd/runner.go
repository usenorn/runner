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
		newRunnerVersionCommand(),
		newRunnerConnectCommand(),
		newRunnerDisconnectCommand(),
		newRunnerInspectCommand(),
		newRunnerSnapshotCommand(),
		newRunnerPauseCommand(),
		newRunnerResumeCommand(),
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

func withScheduling(
	cmd *cobra.Command,
	run func(context.Context, *internal.Scheduling) error,
) error {
	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	scheduling, cleanup, err := internal.InitScheduling(cfgFile, config.Overrides{})
	if err != nil {
		return err
	}
	defer cleanup()

	return run(ctx, scheduling)
}

func withVersion(cmd *cobra.Command, run func(context.Context, *internal.Version) error) error {
	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	version, cleanup, err := internal.InitVersion(cfgFile, config.Overrides{})
	if err != nil {
		return err
	}
	defer cleanup()

	return run(ctx, version)
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

func withInspection(
	cmd *cobra.Command,
	run func(context.Context, *internal.Inspection) error,
) error {
	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	inspection, cleanup, err := internal.InitInspection(cfgFile, config.Overrides{})
	if err != nil {
		return err
	}

	defer cleanup()

	return run(ctx, inspection)
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

func withSnapshotting(
	cmd *cobra.Command,
	run func(context.Context, *internal.Snapshotting) error,
) error {
	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	snapshotting, cleanup, err := internal.InitSnapshotting(cfgFile, config.Overrides{})
	if err != nil {
		return err
	}

	defer cleanup()

	return run(ctx, snapshotting)
}
