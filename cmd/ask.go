package cmd

import (
	"context"

	"github.com/spf13/cobra"

	"github.com/usenorn/runner/internal"
	"github.com/usenorn/runner/internal/control"
)

func newAskCommand() *cobra.Command {
	var (
		exec        string
		options     []string
		fallback    string
		kind        string
		preview     string
		files       []string
		waitSeconds int
		noWait      bool
		freeText    bool
		asJSON      bool
	)

	command := &cobra.Command{
		Use:   "ask <question>",
		Short: "Ask a person a question about this execution",
		Long: "Put a question on the issue this run belongs to. Without --meanwhile the run stops " +
			"and waits: the answer comes back if somebody gives one quickly, and otherwise this " +
			"says so and norn starts the run again once they do. With --meanwhile the question is " +
			"recorded and you carry on with what you declared.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			request := control.QuestionRequest{
				Kind:          kind,
				Blocking:      fallback == "",
				Message:       args[0],
				Options:       options,
				AllowFreeText: freeText,
				Default:       fallback,
				Preview:       preview,
				Files:         files,
			}

			if noWait {
				request.WaitSeconds = 1
			} else {
				request.WaitSeconds = waitSeconds
			}

			return withServices(cmd, func(ctx context.Context, s *internal.Services) error {
				return s.Ask(ctx, exec, request, asJSON)
			})
		},
	}

	command.Flags().StringVar(&exec, "exec", "", "execution id (defaults to NORN_EXEC_ID)")
	command.Flags().StringArrayVar(&options, "option", nil, "an answer to offer (repeatable)")
	command.Flags().StringVar(
		&fallback, "meanwhile", "",
		"what you will do if nobody answers; giving this means you do not stop",
	)
	command.Flags().StringVar(&kind, "kind", "", "decision, clarification or approval")
	command.Flags().StringVar(&preview, "preview", "", "the preview this question is about")
	command.Flags().StringArrayVar(&files, "file", nil, "a file this question is about (repeatable)")
	command.Flags().IntVar(&waitSeconds, "wait", 0, "seconds to hold this turn open for an answer")
	command.Flags().BoolVar(&noWait, "no-wait", false, "do not hold the turn open at all")
	command.Flags().BoolVar(&freeText, "free-text", true, "accept an answer that is not one of the options")
	command.Flags().BoolVar(&asJSON, "json", false, "print the answer as JSON")

	return command
}
