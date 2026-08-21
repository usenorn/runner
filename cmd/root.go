package cmd

import (
	"context"

	"github.com/spf13/cobra"
)

var cfgFile string

func newRootCommand() *cobra.Command {
	root := &cobra.Command{
		Use:           "norn",
		Short:         "Norn Runner",
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	root.PersistentFlags().StringVar(&cfgFile, "config", "", "path to a config file")

	root.AddCommand(newRunnerCommand())

	return root
}

func Execute() error {
	return newRootCommand().ExecuteContext(context.Background())
}
