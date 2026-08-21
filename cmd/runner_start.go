package cmd

import (
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/usenorn/runner/internal"
	"github.com/usenorn/runner/internal/config"
)

func newRunnerStartCommand() *cobra.Command {
	var (
		capacity int
		runtime  string
	)

	command := &cobra.Command{
		Use:   "start",
		Short: "Run the runner in the foreground",
		RunE: func(cmd *cobra.Command, _ []string) error {
			overrides := config.Overrides{}

			if cmd.Flags().Changed("capacity") {
				overrides.Capacity = &capacity
			}

			if cmd.Flags().Changed("runtime") {
				chosen := config.Runtime(runtime)
				overrides.Runtime = &chosen
			}

			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			daemon, cleanup, err := internal.InitDaemon(cfgFile, overrides)
			if err != nil {
				return err
			}
			defer cleanup()

			return daemon.Run(ctx)
		},
	}

	command.Flags().IntVar(&capacity, "capacity", 0, "how many executions may run at once")
	command.Flags().StringVar(&runtime, "runtime", "", "auto, process or docker")

	return command
}
