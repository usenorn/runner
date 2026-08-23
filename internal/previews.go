package internal

import (
	"context"

	"github.com/usenorn/runner/internal/control"
)

func (s *Services) Expose(
	ctx context.Context,
	executionID string,
	request control.PreviewRequest,
	asJSON bool,
) error {
	run, err := s.run(executionID)
	if err != nil {
		return err
	}

	exposed, err := s.client.ExposePreview(ctx, run, request)
	if err != nil {
		return err
	}

	return s.preview(exposed, asJSON)
}

func (s *Services) Close(
	ctx context.Context,
	executionID string,
	name string,
	asJSON bool,
) error {
	run, err := s.run(executionID)
	if err != nil {
		return err
	}

	closed, err := s.client.ClosePreview(ctx, run, name)
	if err != nil {
		return err
	}

	return s.preview(closed, asJSON)
}

func (s *Services) Previews(ctx context.Context, executionID string, asJSON bool) error {
	run, err := s.run(executionID)
	if err != nil {
		return err
	}

	open, err := s.client.Previews(ctx, run)
	if err != nil {
		return err
	}

	if asJSON {
		return s.encode(open)
	}

	if len(open) == 0 {
		return s.line("this run has nothing open")
	}

	rows := make([][5]string, 0, len(open))

	for _, preview := range open {
		rows = append(rows, [5]string{
			preview.Name, preview.Service, number(preview.Port), preview.URL, "",
		})
	}

	return s.table(rows)
}

func (s *Services) preview(preview control.Preview, asJSON bool) error {
	if asJSON {
		return s.encode(preview)
	}

	return s.line(preview.Name + " on " + preview.Service + ": " + preview.URL)
}
