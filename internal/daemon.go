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
	"github.com/usenorn/runner/internal/mcpbridge"
	"github.com/usenorn/runner/internal/observability/logging"
	"github.com/usenorn/runner/internal/pkg/bridge"
	"github.com/usenorn/runner/internal/pkg/socket"
	"github.com/usenorn/runner/internal/service"
)

type Daemon struct {
	cfg       config.Control
	handler   http.Handler
	listener  *socket.Listener
	bridge    *bridge.Listener
	tools     *mcpbridge.Bridge
	sessions  service.Sessions
	updates   service.Updates
	codebases service.Codebases
	channels  service.Channels
	tunnels   service.Tunnels
	runs      service.Executions
	services  service.Services
	uploads   service.Uploads
	logger    *slog.Logger
}

func NewDaemon(
	cfg config.Control,
	handler http.Handler,
	listener *socket.Listener,
	bridge *bridge.Listener,
	tools *mcpbridge.Bridge,
	sessions service.Sessions,
	updates service.Updates,
	codebases service.Codebases,
	channels service.Channels,
	tunnels service.Tunnels,
	runs service.Executions,
	services service.Services,
	uploads service.Uploads,
	logger *slog.Logger,
) *Daemon {
	return &Daemon{
		cfg:       cfg,
		handler:   handler,
		listener:  listener,
		bridge:    bridge,
		tools:     tools,
		sessions:  sessions,
		updates:   updates,
		codebases: codebases,
		channels:  channels,
		tunnels:   tunnels,
		runs:      runs,
		services:  services,
		uploads:   uploads,
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

	if err := d.runs.Reclaim(ctx); err != nil {
		logging.From(ctx).WarnContext(
			ctx,
			"this machine could not read back the runs it was holding",
			slog.String("error", err.Error()),
		)
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

	carrying := make(chan struct{})

	go func() {
		defer close(carrying)

		d.tunnels.Run(ctx)
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

	sending := make(chan struct{})

	go func() {
		defer close(sending)

		d.uploads.Run(ctx)
	}()

	bridging := d.serveTools(ctx)

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

	bridging(shutdownCtx)

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
	<-carrying
	<-preparing
	<-supervising
	<-sending

	logging.From(ctx).InfoContext(ctx, "runner stopped")

	return <-serving
}

func (d *Daemon) serveTools(ctx context.Context) func(context.Context) {
	if err := d.bridge.Available(); err != nil {
		logging.From(ctx).InfoContext(
			ctx,
			"runs in docker cannot reach their tools on this machine, so docker is not offered",
			slog.String("reason", err.Error()),
		)

		return func(context.Context) {}
	}

	server := &http.Server{
		Handler:           d.tools,
		ReadHeaderTimeout: d.cfg.ReadHeaderTimeout,
		BaseContext:       func(net.Listener) context.Context { return ctx },
	}

	served := make(chan struct{})

	go func() {
		defer close(served)

		logging.From(ctx).InfoContext(
			ctx, "runner serving tools to containers", slog.String("address", d.bridge.Addr().String()),
		)

		if err := server.Serve(d.bridge); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logging.From(ctx).WarnContext(
				ctx, "runner stopped serving tools to containers", slog.String("error", err.Error()),
			)
		}
	}()

	return func(shutdown context.Context) {
		if err := server.Shutdown(shutdown); err != nil {
			_ = server.Close()
		}

		<-served
	}
}
