package internal

import (
	"context"
	"encoding/json"
	"fmt"
	"text/tabwriter"

	"github.com/usenorn/runner/internal/control"
	"github.com/usenorn/runner/internal/entity"
)

func (s *Status) Doctor(ctx context.Context, asJSON bool) error {
	doctor, err := s.client.Doctor(ctx)
	if err != nil {
		return err
	}

	if asJSON {
		encoder := json.NewEncoder(s.out)
		encoder.SetIndent("", "  ")

		if err := encoder.Encode(doctor); err != nil {
			return err
		}
	} else if err := s.diagnosis(doctor); err != nil {
		return err
	}

	if !doctor.Healthy {
		return entity.Exit(entity.ExitFailure, entity.ErrMachineNotReady)
	}

	return nil
}

func (s *Status) diagnosis(doctor control.Doctor) error {
	rows := [][2]string{
		{"sandbox", doctor.Sandbox},
		{"commits by", doctor.CommitAuthor},
		{"pull requests", doctor.PullRequests},
	}

	if len(doctor.Codebases) == 0 {
		rows = append(rows, [2]string{"folders", "none, connect one with 'norn runner inspect'"})
	}

	for _, codebase := range doctor.Codebases {
		rows = append(rows, [2]string{"folder", codebase.Root})

		if codebase.Failure != "" {
			rows = append(rows, [2]string{"", "could not be checked: " + codebase.Failure})
		}

		if len(codebase.Tools) == 0 && codebase.Failure == "" {
			rows = append(rows, [2]string{"", "nothing to build was found"})
		}

		for _, tool := range codebase.Tools {
			rows = append(rows, [2]string{"  " + tool.State, tool.Summary})
		}
	}

	writer := tabwriter.NewWriter(s.out, 0, 0, 3, ' ', 0)

	for _, row := range rows {
		if _, err := fmt.Fprintf(writer, "%s\t%s\n", row[0], row[1]); err != nil {
			return fmt.Errorf("write the diagnosis: %w", err)
		}
	}

	return writer.Flush()
}
