package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/usenorn/runner/internal"
)

func newRunnerSnapshotCommand() *cobra.Command {
	snapshot := &cobra.Command{
		Use:   "snapshot",
		Short: "Copy a connected folder into a workspace an execution can work in",
	}

	snapshot.AddCommand(
		newRunnerSnapshotTakeCommand(),
		newRunnerSnapshotListCommand(),
		newRunnerSnapshotRemoveCommand(),
	)

	return snapshot
}

func newRunnerSnapshotTakeCommand() *cobra.Command {
	var (
		path         string
		attempt      int
		includeDirty bool
		asJSON       bool
	)

	command := &cobra.Command{
		Use:   "take <ISSUE-KEY>",
		Short: "Take a copy of this folder for an issue, leaving the original untouched",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if path == "" {
				here, err := os.Getwd()
				if err != nil {
					return fmt.Errorf("read the current folder: %w", err)
				}

				path = here
			}

			return withSnapshotting(cmd, func(ctx context.Context, snapshotting *internal.Snapshotting) error {
				return snapshotting.Take(ctx, internal.SnapshotOptions{
					Path:         path,
					IssueKey:     args[0],
					Attempt:      attempt,
					IncludeDirty: includeDirty,
					JSON:         asJSON,
				})
			})
		},
	}

	command.Flags().StringVar(&path, "codebase", "", "a path inside the connected folder to copy")
	command.Flags().IntVar(&attempt, "attempt", 1, "which attempt at this issue this is")
	command.Flags().BoolVar(
		&includeDirty, "include-dirty", false, "carry uncommitted work across as one commit",
	)
	command.Flags().BoolVar(&asJSON, "json", false, "write the report as json")

	return command
}

func newRunnerSnapshotListCommand() *cobra.Command {
	var asJSON bool

	command := &cobra.Command{
		Use:   "list",
		Short: "List the snapshots this machine is holding",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return withSnapshotting(cmd, func(ctx context.Context, snapshotting *internal.Snapshotting) error {
				return snapshotting.List(ctx, asJSON)
			})
		},
	}

	command.Flags().BoolVar(&asJSON, "json", false, "write the list as json")

	return command
}

func newRunnerSnapshotRemoveCommand() *cobra.Command {
	var asJSON bool

	command := &cobra.Command{
		Use:   "remove <name>",
		Short: "Take a snapshot away and give the original repositories their worktrees back",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withSnapshotting(cmd, func(ctx context.Context, snapshotting *internal.Snapshotting) error {
				return snapshotting.Remove(ctx, args[0], asJSON)
			})
		},
	}

	command.Flags().BoolVar(&asJSON, "json", false, "write the result as json")

	return command
}
