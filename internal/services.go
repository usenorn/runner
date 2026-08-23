package internal

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/usenorn/runner/internal/control"
	"github.com/usenorn/runner/internal/entity"
)

var errNoExecution = errors.New(
	"this command runs inside one execution and nothing says which; set " +
		entity.ExecutionVariable +
		" or pass --exec, and 'norn runner executions' lists what this machine is holding",
)

type Services struct {
	client *control.Client
	out    io.Writer
	look   func(string) string
}

func NewServices(client *control.Client) *Services {
	return &Services{client: client, out: os.Stdout, look: os.Getenv}
}

func (s *Services) Start(
	ctx context.Context,
	executionID string,
	request control.ServiceRequest,
	asJSON bool,
) error {
	run, err := s.run(executionID)
	if err != nil {
		return err
	}

	service, err := s.client.StartService(ctx, run, request)
	if err != nil {
		return err
	}

	return s.one(service, asJSON)
}

func (s *Services) Stop(ctx context.Context, executionID, name string, asJSON bool) error {
	run, err := s.run(executionID)
	if err != nil {
		return err
	}

	service, err := s.client.StopService(ctx, run, name)
	if err != nil {
		return err
	}

	return s.one(service, asJSON)
}

func (s *Services) Restart(ctx context.Context, executionID, name string, asJSON bool) error {
	run, err := s.run(executionID)
	if err != nil {
		return err
	}

	service, err := s.client.RestartService(ctx, run, name)
	if err != nil {
		return err
	}

	return s.one(service, asJSON)
}

func (s *Services) List(ctx context.Context, executionID string, asJSON bool) error {
	run, err := s.run(executionID)
	if err != nil {
		return err
	}

	services, err := s.client.Services(ctx, run)
	if err != nil {
		return err
	}

	if asJSON {
		return s.encode(services)
	}

	if len(services) == 0 {
		return s.line("this run has no services")
	}

	rows := make([][5]string, 0, len(services))

	for _, service := range services {
		rows = append(rows, [5]string{
			service.Name, service.State, number(service.Port), number(service.PID), service.Reason,
		})
	}

	return s.table(rows)
}

func (s *Services) Logs(
	ctx context.Context,
	executionID string,
	name string,
	tail int,
	asJSON bool,
) error {
	run, err := s.run(executionID)
	if err != nil {
		return err
	}

	held, err := s.client.ServiceLogs(ctx, run, name, tail)
	if err != nil {
		return err
	}

	if asJSON {
		return s.encode(held)
	}

	if len(held.Lines) == 0 {
		return s.line("that service has written nothing")
	}

	return s.line(strings.Join(held.Lines, "\n"))
}

func (s *Services) Step(
	ctx context.Context,
	executionID string,
	request control.StepRequest,
	asJSON bool,
) error {
	run, err := s.run(executionID)
	if err != nil {
		return err
	}

	result, err := s.client.RunStep(ctx, run, request)
	if err != nil {
		return err
	}

	if asJSON {
		return s.encode(result)
	}

	if result.Output != "" {
		if err := s.line(result.Output); err != nil {
			return err
		}
	}

	if result.TimedOut {
		return entity.Exit(
			entity.ExitFailure,
			fmt.Errorf("%s was stopped after %s", result.Name, result.Took),
		)
	}

	if result.ExitCode != 0 {
		return entity.Exit(
			entity.ExitFailure,
			fmt.Errorf("%s stopped with exit code %d", result.Name, result.ExitCode),
		)
	}

	return s.line(fmt.Sprintf("%s finished in %s", result.Name, result.Took))
}

func (s *Services) Ask(
	ctx context.Context,
	executionID string,
	request control.QuestionRequest,
	asJSON bool,
) error {
	run, err := s.run(executionID)
	if err != nil {
		return err
	}

	answered, err := s.client.Ask(ctx, run, request)
	if err != nil {
		return err
	}

	if asJSON {
		return s.encode(answered)
	}

	switch answered.Status {
	case string(entity.AskAnswered):
		return s.line(said(answered) + "\n\n" + answered.Answer)
	default:
		return s.line(answered.Advice)
	}
}

func said(answered control.QuestionAnswer) string {
	if answered.AnsweredBy == "" {
		return "Somebody answered:"
	}

	return answered.AnsweredBy + " answered:"
}

func (s *Services) run(executionID string) (string, error) {
	if executionID != "" {
		return executionID, nil
	}

	if held := s.look(entity.ExecutionVariable); held != "" {
		return held, nil
	}

	return "", errNoExecution
}

func (s *Services) one(service control.Service, asJSON bool) error {
	if asJSON {
		return s.encode(service)
	}

	return s.line(fmt.Sprintf("%s is %s: %s", service.Name, service.State, service.Reason))
}

func number(held int) string {
	if held == 0 {
		return "-"
	}

	return strconv.Itoa(held)
}

func (s *Services) table(rows [][5]string) error {
	writer := tabwriter.NewWriter(s.out, 0, 0, 3, ' ', 0)

	for _, row := range rows {
		if _, err := fmt.Fprintf(
			writer, "%s\t%s\t%s\t%s\t%s\n", row[0], row[1], row[2], row[3], row[4],
		); err != nil {
			return fmt.Errorf("write the answer: %w", err)
		}
	}

	return writer.Flush()
}

func (s *Services) line(text string) error {
	if _, err := fmt.Fprintln(s.out, text); err != nil {
		return fmt.Errorf("write the answer: %w", err)
	}

	return nil
}

func (s *Services) encode(value any) error {
	encoder := json.NewEncoder(s.out)
	encoder.SetIndent("", "  ")

	return encoder.Encode(value)
}
