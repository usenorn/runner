package cmd

import (
	"context"

	"github.com/spf13/cobra"

	"github.com/usenorn/runner/internal"
	"github.com/usenorn/runner/internal/control"
)

func newPreviewCommand() *cobra.Command {
	preview := &cobra.Command{
		Use:   "preview",
		Short: "Open one of this execution's services for a person to look at",
	}

	preview.AddCommand(
		newPreviewExposeCommand(),
		newPreviewListCommand(),
		newPreviewCloseCommand(),
	)

	return preview
}

func newPreviewExposeCommand() *cobra.Command {
	var (
		exec    string
		service string
		name    string
		path    string
		asJSON  bool
	)

	command := &cobra.Command{
		Use:   "expose --service <name>",
		Short: "Open a service of this execution",
		Long: "Only a service norn is running for this execution and has found healthy can be " +
			"opened. The address this answers with reaches this machine; the shared one arrives " +
			"with norn's preview tunnel.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			request := control.PreviewRequest{Service: service, Name: name, Path: path}

			return withServices(cmd, func(ctx context.Context, s *internal.Services) error {
				return s.Expose(ctx, exec, request, asJSON)
			})
		},
	}

	command.Flags().StringVar(&service, "service", "", "the service to open")
	command.Flags().StringVar(&name, "name", "", "what to call it; defaults to the service's name")
	command.Flags().StringVar(&path, "path", "", "the path to open at")

	if err := command.MarkFlagRequired("service"); err != nil {
		panic(err)
	}

	withRun(command, &exec, &asJSON)

	return command
}

func newPreviewListCommand() *cobra.Command {
	var (
		exec   string
		asJSON bool
	)

	command := &cobra.Command{
		Use:   "list",
		Short: "What this execution has open",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return withServices(cmd, func(ctx context.Context, s *internal.Services) error {
				return s.Previews(ctx, exec, asJSON)
			})
		},
	}

	withRun(command, &exec, &asJSON)

	return command
}

func newPreviewCloseCommand() *cobra.Command {
	var (
		exec   string
		asJSON bool
	)

	command := &cobra.Command{
		Use:   "close <name>",
		Short: "Close one of this execution's previews",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withServices(cmd, func(ctx context.Context, s *internal.Services) error {
				return s.Close(ctx, exec, args[0], asJSON)
			})
		},
	}

	withRun(command, &exec, &asJSON)

	return command
}
