package cmd

import (
	"context"
	"errors"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/usenorn/runner/internal"
)

const tokenEnv = "NORN_TOKEN"

func newRunnerConnectCommand() *cobra.Command {
	var (
		token         string
		name          string
		force         bool
		insecureStore bool
	)

	command := &cobra.Command{
		Use:   "connect",
		Short: "Bind this machine to a Norn agent",
		RunE: func(cmd *cobra.Command, _ []string) error {
			presented := strings.TrimSpace(token)
			if presented == "" {
				presented = strings.TrimSpace(os.Getenv(tokenEnv))
			}

			if presented == "" {
				return errors.New(
					"pass the agent's api token with --token, or put it in " + tokenEnv +
						" to keep it out of your shell history",
				)
			}

			return withBinding(cmd, func(ctx context.Context, binding *internal.Binding) error {
				return binding.Connect(ctx, internal.ConnectOptions{
					Token:         presented,
					Name:          name,
					Force:         force,
					InsecureStore: insecureStore,
				})
			})
		},
	}

	command.Flags().StringVar(&token, "token", "", "the agent's api token, read once and discarded")
	command.Flags().StringVar(&name, "name", "", "what to call this machine in norn")
	command.Flags().BoolVar(&force, "force", false, "replace an existing binding on this machine")
	command.Flags().BoolVar(
		&insecureStore,
		"insecure-store",
		false,
		"keep credentials in a file encrypted with the machine id, for hosts with no os keystore",
	)

	return command
}
