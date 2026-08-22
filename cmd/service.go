package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/usenorn/runner/internal"
	"github.com/usenorn/runner/internal/config"
	"github.com/usenorn/runner/internal/control"
	"github.com/usenorn/runner/internal/entity"
)

func newServiceCommand() *cobra.Command {
	service := &cobra.Command{
		Use:   "service",
		Short: "Start and stop the services of one execution",
	}

	service.AddCommand(
		newServiceStartCommand(),
		newServiceStopCommand(),
		newServiceRestartCommand(),
		newServiceListCommand(),
		newServiceLogsCommand(),
		newServiceStepCommand(),
	)

	return service
}

func newServiceStartCommand() *cobra.Command {
	var (
		exec        string
		dir         string
		environment []string
		requires    []string
		httpPath    string
		tcpHealth   bool
		logPattern  string
		asJSON      bool
	)

	command := &cobra.Command{
		Use:   "start <name> -- <command …>",
		Short: "Start a service in this execution's workspace",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			run, err := split(cmd, args)
			if err != nil {
				return err
			}

			health, err := healthOf(cmd, httpPath, tcpHealth, logPattern)
			if err != nil {
				return err
			}

			values, err := valuesOf(environment)
			if err != nil {
				return err
			}

			request := control.ServiceRequest{
				Name:        args[0],
				Dir:         dir,
				Command:     run,
				Environment: values,
				Requires:    requires,
				Health:      health,
			}

			return withServices(cmd, func(ctx context.Context, s *internal.Services) error {
				return s.Start(ctx, exec, request, asJSON)
			})
		},
	}

	command.Flags().StringVar(&dir, "dir", "", "run in this folder of the workspace")
	command.Flags().StringArrayVar(&environment, "env", nil, "set KEY=VALUE for the service")
	command.Flags().StringArrayVar(&requires, "requires", nil, "wait for this service to be healthy")
	command.Flags().StringVar(&httpPath, "health-http", "", "call this path on the service's port")
	command.Flags().BoolVar(&tcpHealth, "health-tcp", false, "connect to the service's port")
	command.Flags().StringVar(&logPattern, "health-log", "", "wait for a line matching this pattern")

	withRun(command, &exec, &asJSON)

	return command
}

func newServiceStopCommand() *cobra.Command {
	var (
		exec   string
		asJSON bool
	)

	command := &cobra.Command{
		Use:   "stop <name>",
		Short: "Stop a service and everything it started",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withServices(cmd, func(ctx context.Context, s *internal.Services) error {
				return s.Stop(ctx, exec, args[0], asJSON)
			})
		},
	}

	withRun(command, &exec, &asJSON)

	return command
}

func newServiceRestartCommand() *cobra.Command {
	var (
		exec   string
		asJSON bool
	)

	command := &cobra.Command{
		Use:   "restart <name>",
		Short: "Stop a service and start it again on the same port",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withServices(cmd, func(ctx context.Context, s *internal.Services) error {
				return s.Restart(ctx, exec, args[0], asJSON)
			})
		},
	}

	withRun(command, &exec, &asJSON)

	return command
}

func newServiceListCommand() *cobra.Command {
	var (
		exec   string
		asJSON bool
	)

	command := &cobra.Command{
		Use:   "list",
		Short: "List this execution's services, their ports and their health",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return withServices(cmd, func(ctx context.Context, s *internal.Services) error {
				return s.List(ctx, exec, asJSON)
			})
		},
	}

	withRun(command, &exec, &asJSON)

	return command
}

func newServiceLogsCommand() *cobra.Command {
	var (
		exec   string
		tail   int
		asJSON bool
	)

	command := &cobra.Command{
		Use:   "logs <name>",
		Short: "Show what a service has written",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withServices(cmd, func(ctx context.Context, s *internal.Services) error {
				return s.Logs(ctx, exec, args[0], tail, asJSON)
			})
		},
	}

	command.Flags().IntVar(&tail, "tail", 0, "show only the last this many lines")

	withRun(command, &exec, &asJSON)

	return command
}

func newServiceStepCommand() *cobra.Command {
	var (
		exec    string
		dir     string
		timeout string
		asJSON  bool
	)

	command := &cobra.Command{
		Use:   "step <name> -- <command …>",
		Short: "Run a one-shot step in this execution's workspace",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			run, err := split(cmd, args)
			if err != nil {
				return err
			}

			request := control.StepRequest{
				Name:    args[0],
				Dir:     dir,
				Command: run,
				Timeout: timeout,
			}

			return withServices(cmd, func(ctx context.Context, s *internal.Services) error {
				return s.Step(ctx, exec, request, asJSON)
			})
		},
	}

	command.Flags().StringVar(&dir, "dir", "", "run in this folder of the workspace")
	command.Flags().StringVar(&timeout, "timeout", "", "stop the step after this long")

	withRun(command, &exec, &asJSON)

	return command
}

func withRun(command *cobra.Command, exec *string, asJSON *bool) {
	command.Flags().StringVar(exec, "exec", "", "the execution to act on, over "+internal.ExecutionVariable)
	command.Flags().BoolVar(asJSON, "json", false, "write the answer as json")
}

func split(command *cobra.Command, args []string) ([]string, error) {
	at := command.ArgsLenAtDash()

	if at < 0 || at >= len(args) {
		return nil, fmt.Errorf(
			"write the command after --, as in 'norn service %s %s -- pnpm dev'",
			command.Name(), args[0],
		)
	}

	return args[at:], nil
}

func healthOf(
	command *cobra.Command,
	httpPath string,
	tcpHealth bool,
	logPattern string,
) (control.Health, error) {
	asked := []string{}

	for name, chosen := range map[string]bool{
		"health-http": command.Flags().Changed("health-http"),
		"health-tcp":  tcpHealth,
		"health-log":  logPattern != "",
	} {
		if chosen {
			asked = append(asked, name)
		}
	}

	if len(asked) > 1 {
		return control.Health{}, fmt.Errorf(
			"a service is checked one way; --%s were all asked for", strings.Join(asked, ", --"),
		)
	}

	switch {
	case command.Flags().Changed("health-http"):
		return control.Health{Kind: string(entity.HealthHTTP), Path: httpPath}, nil
	case tcpHealth:
		return control.Health{Kind: string(entity.HealthTCP)}, nil
	case logPattern != "":
		return control.Health{Kind: string(entity.HealthLog), Pattern: logPattern}, nil
	default:
		return control.Health{Kind: string(entity.HealthNone)}, nil
	}
}

func valuesOf(environment []string) (map[string]string, error) {
	values := map[string]string{}

	for _, pair := range environment {
		key, value, found := strings.Cut(pair, "=")
		if !found || key == "" {
			return nil, fmt.Errorf("--env %s is not KEY=VALUE", pair)
		}

		values[key] = value
	}

	return values, nil
}

func withServices(cmd *cobra.Command, run func(context.Context, *internal.Services) error) error {
	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	services, cleanup, err := internal.InitServices(cfgFile, config.Overrides{})
	if err != nil {
		return err
	}
	defer cleanup()

	return run(ctx, services)
}
