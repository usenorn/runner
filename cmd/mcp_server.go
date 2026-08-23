package cmd

import (
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/usenorn/runner/internal"
	"github.com/usenorn/runner/internal/config"
	"github.com/usenorn/runner/internal/entity"
)

func newMCPServerCommand() *cobra.Command {
	var exec string

	command := &cobra.Command{
		Use:   "mcp-server",
		Short: "Serve norn's tools to one execution's coding agent",
		Long: "The coding agent's own CLI starts this from the config norn wrote for the run. " +
			"It speaks MCP over stdin and stdout, and every tool it offers acts on the one " +
			"execution it was started for.",
		Hidden: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			quiet := config.ConsoleNever

			server, cleanup, err := internal.InitMCPServer(
				cfgFile, config.Overrides{Console: &quiet},
			)
			if err != nil {
				return err
			}
			defer cleanup()

			return server.Run(ctx, held(exec))
		},
	}

	command.Flags().StringVar(
		&exec, "exec", "", "the execution to serve, over "+entity.ExecutionVariable,
	)

	return command
}

func held(exec string) string {
	if exec != "" {
		return exec
	}

	return os.Getenv(entity.ExecutionVariable)
}
