package internal

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/usenorn/runner/internal/control"
)

type Binding struct {
	client *control.Client
	out    io.Writer
}

func NewBinding(client *control.Client) *Binding {
	return &Binding{client: client, out: os.Stdout}
}

type ConnectOptions struct {
	Token         string
	Name          string
	Force         bool
	InsecureStore bool
}

func (b *Binding) Connect(ctx context.Context, options ConnectOptions) error {
	store := ""
	if options.InsecureStore {
		store = "encrypted-file"
	}

	connected, err := b.client.Connect(ctx, control.ConnectRequest{
		Token: options.Token,
		Name:  options.Name,
		Store: store,
		Force: options.Force,
	})
	if err != nil {
		return err
	}

	if _, err := fmt.Fprintf(
		b.out,
		"connected %s to %s as %s, keeping its credentials in the %s\n",
		connected.Machine, connected.Server, connected.Agent, connected.Store,
	); err != nil {
		return fmt.Errorf("write the connect report: %w", err)
	}

	return b.sessionLine(connected.Session, connected.SessionDetail)
}

func (b *Binding) Disconnect(ctx context.Context) error {
	disconnected, err := b.client.Disconnect(ctx)
	if err != nil {
		return err
	}

	if _, err := fmt.Fprintf(
		b.out,
		"disconnected %s and cleared its credentials from this machine\n"+
			"%s still lists it; revoke it there under settings, runners, to retire it for good\n",
		disconnected.Machine, disconnected.Server,
	); err != nil {
		return fmt.Errorf("write the disconnect report: %w", err)
	}

	return nil
}

func (b *Binding) sessionLine(state, detail string) error {
	line := "session " + state
	if detail != "" {
		line += " — " + detail
	}

	if _, err := fmt.Fprintln(b.out, line); err != nil {
		return fmt.Errorf("write the session report: %w", err)
	}

	return nil
}
