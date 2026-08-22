package internal

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"

	"github.com/usenorn/runner/internal/config"
	"github.com/usenorn/runner/internal/entity"
	"github.com/usenorn/runner/internal/observability/logging"
	"github.com/usenorn/runner/internal/pkg/socket"
	"github.com/usenorn/runner/internal/service"
)

type Daemon struct {
	cfg       config.Control
	handler   http.Handler
	listener  *socket.Listener
	sessions  service.Sessions
	updates   service.Updates
	codebases service.Codebases
	channels  service.Channels
	runs      service.Executions
	services  service.Services
	logger    *slog.Logger
}

func NewDaemon(
	cfg config.Control,
	handler http.Handler,
	listener *socket.Listener,
	sessions service.Sessions,
	updates service.Updates,
	codebases service.Codebases,
	channels service.Channels,
	runs service.Executions,
	services service.Services,
	logger *slog.Logger,
) *Daemon {
	return &Daemon{
		cfg:       cfg,
		handler:   handler,
		listener:  listener,
		sessions:  sessions,
		updates:   updates,
		codebases: codebases,
		channels:  channels,
		runs:      runs,
		services:  services,
		logger:    logger,
	}
}

func (d *Daemon) Run(ctx context.Context) error {
	ctx = logging.Into(ctx, d.logger)

	server := &http.Server{
		Handler:           d.handler,
		ReadHeaderTimeout: d.cfg.ReadHeaderTimeout,
		BaseContext:       func(net.Listener) context.Context { return ctx },
	}

	renewing := make(chan struct{})

	go func() {
		defer close(renewing)

		d.sessions.Run(ctx)
	}()

	watching := make(chan struct{})

	go func() {
		defer close(watching)

		d.updates.Run(ctx)
	}()

	rescanning := make(chan struct{})

	go func() {
		defer close(rescanning)

		d.codebases.Run(ctx)
	}()

	connecting := make(chan struct{})

	go func() {
		defer close(connecting)

		d.channels.Run(ctx)
	}()

	preparing := make(chan struct{})

	go func() {
		defer close(preparing)

		d.runs.Run(ctx)
	}()

	supervising := make(chan struct{})

	go func() {
		defer close(supervising)

		d.services.Run(ctx)
	}()

	serving := make(chan error, 1)

	go func() {
		logging.From(ctx).InfoContext(
			ctx, "runner listening", slog.String("socket", d.listener.Path()),
		)

		if err := server.Serve(d.listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serving <- fmt.Errorf("serve the local control api: %w", err)

			return
		}

		serving <- nil
	}()

	select {
	case err := <-serving:
		return err
	case <-ctx.Done():
	}

	logging.From(ctx).InfoContext(ctx, "runner draining")

	shutdownCtx, cancel := context.WithTimeout(
		context.WithoutCancel(ctx), d.cfg.ShutdownTimeout,
	)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		_ = server.Close()
		<-serving

		logging.From(ctx).WarnContext(
			ctx,
			"runner drain forced",
			slog.Duration("shutdown_timeout", d.cfg.ShutdownTimeout),
		)

		return entity.Exit(
			entity.ExitDrainForced, fmt.Errorf("drain the local control api: %w", err),
		)
	}

	<-renewing
	<-watching
	<-rescanning
	<-connecting
	<-preparing
	<-supervising

	logging.From(ctx).InfoContext(ctx, "runner stopped")

	return <-serving
}
