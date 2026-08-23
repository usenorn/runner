package cmd

import (
	"context"

	"github.com/spf13/cobra"

	"github.com/usenorn/runner/internal/config"
)

var cfgFile string

func newRootCommand() *cobra.Command {
	root := &cobra.Command{
		Use:           "norn",
		Short:         "Norn Runner",
		Version:       config.Version(),
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	root.SetVersionTemplate("{{.Version}}\n")

	root.PersistentFlags().StringVar(&cfgFile, "config", "", "path to a config file")

	root.AddCommand(
		newRunnerCommand(),
		newServiceCommand(),
		newPreviewCommand(),
		newAskCommand(),
		newMCPServerCommand(),
	)

	return root
}

func Execute() error {
	return newRootCommand().ExecuteContext(context.Background())
}
