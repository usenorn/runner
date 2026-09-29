package cmd

import (
	"context"
	"errors"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/usenorn/runner/internal"
)

const agentTokenEnv = "CLAUDE_CODE_OAUTH_TOKEN"

func newRunnerAgentTokenCommand() *cobra.Command {
	var token string

	command := &cobra.Command{
		Use:   "agent-token",
		Short: "Keep the token every run signs the coding agent in with",
		RunE: func(cmd *cobra.Command, _ []string) error {
			presented := strings.TrimSpace(token)
			if presented == "" {
				presented = strings.TrimSpace(os.Getenv(agentTokenEnv))
			}

			if presented == "" {
				return errors.New(
					"run 'claude setup-token', then pass what it prints with --token, or put it in " +
						agentTokenEnv + " to keep it out of your shell history",
				)
			}

			return withBinding(cmd, func(ctx context.Context, binding *internal.Binding) error {
				return binding.SaveAgentToken(ctx, presented)
			})
		},
	}

	command.Flags().StringVar(&token, "token", "", "the token 'claude setup-token' printed")

	return command
}
