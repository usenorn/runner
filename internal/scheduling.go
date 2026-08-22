package internal

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/usenorn/runner/internal/control"
)

type Scheduling struct {
	client *control.Client
	out    io.Writer
}

func NewScheduling(client *control.Client) *Scheduling {
	return &Scheduling{client: client, out: os.Stdout}
}

func (s *Scheduling) Pause(ctx context.Context, asJSON bool) error {
	paused, err := s.client.Pause(ctx)
	if err != nil {
		return err
	}

	return s.say(paused, asJSON, "this machine is paused and will turn down new work until "+
		"'norn runner resume'. Anything already running carries on")
}

func (s *Scheduling) Resume(ctx context.Context, asJSON bool) error {
	paused, err := s.client.Resume(ctx)
	if err != nil {
		return err
	}

	return s.say(paused, asJSON, "this machine is taking work again")
}

func (s *Scheduling) say(paused control.Paused, asJSON bool, line string) error {
	if asJSON {
		encoder := json.NewEncoder(s.out)
		encoder.SetIndent("", "  ")

		return encoder.Encode(paused)
	}

	if _, err := fmt.Fprintln(s.out, line); err != nil {
		return fmt.Errorf("write the answer: %w", err)
	}

	return nil
}
