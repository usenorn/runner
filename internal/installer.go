package internal

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/usenorn/runner/internal/pkg/servicemanager"
)

type Installer struct {
	manager *servicemanager.Manager
	out     io.Writer
}

func NewInstaller(manager *servicemanager.Manager) *Installer {
	return &Installer{manager: manager, out: os.Stdout}
}

func (i *Installer) Install(ctx context.Context) error {
	plan, err := i.manager.Install(ctx)
	if err != nil {
		if plan.Path != "" {
			_, _ = fmt.Fprintf(i.out, "wrote %s\n", plan.Path)
		}

		return err
	}

	_, err = fmt.Fprintf(i.out, "registered %s from %s\n", plan.Label, plan.Path)

	return err
}

func (i *Installer) Uninstall(ctx context.Context) error {
	plan, err := i.manager.Uninstall(ctx)
	if err != nil {
		return err
	}

	_, err = fmt.Fprintf(i.out, "removed %s\n", plan.Label)

	return err
}
