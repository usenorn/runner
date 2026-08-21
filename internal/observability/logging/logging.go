package logging

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"

	"gopkg.in/natefinch/lumberjack.v2"

	"github.com/usenorn/runner/internal/config"
	"github.com/usenorn/runner/internal/pkg/statedir"
)

const service = "norn-runner"

func New(app config.App, cfg config.Log, dir *statedir.Dir) (*slog.Logger, func(), error) {
	level, err := parseLevel(app.LogLevel)
	if err != nil {
		return nil, nil, err
	}

	rotator := &lumberjack.Logger{
		Filename:   dir.LogFile(),
		MaxSize:    cfg.MaxSizeMB,
		MaxBackups: cfg.MaxBackups,
		MaxAge:     cfg.MaxAgeDays,
		Compress:   cfg.Compress,
	}

	var sink io.Writer = rotator

	if consoleWanted(cfg.Console) {
		sink = io.MultiWriter(rotator, os.Stdout)
	}

	handler := slog.NewJSONHandler(sink, &slog.HandlerOptions{Level: level})

	logger := slog.New(redactHandler{inner: handler}).With(
		slog.String("service", service),
		slog.String("version", app.Version),
		slog.Int("pid", os.Getpid()),
	)

	cleanup := func() {
		_ = rotator.Close()
	}

	return logger, cleanup, nil
}

func consoleWanted(console config.Console) bool {
	switch console {
	case config.ConsoleAlways:
		return true
	case config.ConsoleNever:
		return false
	default:
		info, err := os.Stdout.Stat()

		return err == nil && info.Mode()&os.ModeCharDevice != 0
	}
}

func parseLevel(name string) (slog.Level, error) {
	switch strings.ToLower(name) {
	case "debug":
		return slog.LevelDebug, nil
	case "info":
		return slog.LevelInfo, nil
	case "warn":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return 0, fmt.Errorf("unknown log level %q", name)
	}
}
