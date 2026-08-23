package mcpserver

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/usenorn/runner/internal/control"
	"github.com/usenorn/runner/internal/entity"
)

type startServiceInput struct {
	Name        string            `json:"name" jsonschema:"a short name for this service, like web or api"`
	Command     []string          `json:"command" jsonschema:"the command and its arguments, already split"`
	Dir         string            `json:"dir,omitempty" jsonschema:"where to run it, relative to the workspace root"`
	Environment map[string]string `json:"env,omitempty" jsonschema:"extra environment for this process"`
	Requires    []string          `json:"requires,omitempty" jsonschema:"services that must be healthy before this one starts"`
	HealthHTTP  string            `json:"health_http,omitempty" jsonschema:"a path this service answers 2xx on once it is up"`
	HealthTCP   bool              `json:"health_tcp,omitempty" jsonschema:"true when being connectable is enough to call it up"`
	HealthLog   string            `json:"health_log,omitempty" jsonschema:"a pattern this service prints once it is up"`
	HealthPort  string            `json:"health_port,omitempty" jsonschema:"the port to probe, when it is not the service's own"`
}

type serviceOutput struct {
	Service serviceDTO `json:"service"`
}

type servicesOutput struct {
	Services []serviceDTO `json:"services"`
}

func (t *toolset) startService(
	ctx context.Context,
	_ *mcp.CallToolRequest,
	input startServiceInput,
) (*mcp.CallToolResult, serviceOutput, error) {
	service, err := t.client.StartService(ctx, t.execution, control.ServiceRequest{
		Name:        input.Name,
		Dir:         input.Dir,
		Command:     input.Command,
		Environment: input.Environment,
		Requires:    input.Requires,
		Health:      healthOf(input),
	})
	if err != nil {
		return nil, serviceOutput{}, toolFailure(err)
	}

	return nil, serviceOutput{Service: serviceDTOFrom(service)}, nil
}

type serviceInput struct {
	Name string `json:"name" jsonschema:"the service to act on"`
}

func (t *toolset) stopService(
	ctx context.Context,
	_ *mcp.CallToolRequest,
	input serviceInput,
) (*mcp.CallToolResult, serviceOutput, error) {
	service, err := t.client.StopService(ctx, t.execution, input.Name)
	if err != nil {
		return nil, serviceOutput{}, toolFailure(err)
	}

	return nil, serviceOutput{Service: serviceDTOFrom(service)}, nil
}

func (t *toolset) restartService(
	ctx context.Context,
	_ *mcp.CallToolRequest,
	input serviceInput,
) (*mcp.CallToolResult, serviceOutput, error) {
	service, err := t.client.RestartService(ctx, t.execution, input.Name)
	if err != nil {
		return nil, serviceOutput{}, toolFailure(err)
	}

	return nil, serviceOutput{Service: serviceDTOFrom(service)}, nil
}

func (t *toolset) listServices(
	ctx context.Context,
	_ *mcp.CallToolRequest,
	_ struct{},
) (*mcp.CallToolResult, servicesOutput, error) {
	services, err := t.client.Services(ctx, t.execution)
	if err != nil {
		return nil, servicesOutput{}, toolFailure(err)
	}

	answer := servicesOutput{Services: make([]serviceDTO, 0, len(services))}

	for _, service := range services {
		answer.Services = append(answer.Services, serviceDTOFrom(service))
	}

	return nil, answer, nil
}

type serviceLogsInput struct {
	Name string `json:"name" jsonschema:"the service whose output you want"`
	Tail int    `json:"tail,omitempty" jsonschema:"how many lines to bring back; the most recent ones"`
	Grep string `json:"grep,omitempty" jsonschema:"a regular expression; only lines matching it come back"`
}

type serviceLogsOutput struct {
	Lines []string `json:"lines"`
}

func (t *toolset) serviceLogs(
	ctx context.Context,
	_ *mcp.CallToolRequest,
	input serviceLogsInput,
) (*mcp.CallToolResult, serviceLogsOutput, error) {
	lines, err := t.client.ServiceLogs(ctx, t.execution, input.Name, entity.LogQuery{
		Tail: input.Tail,
		Grep: input.Grep,
	})
	if err != nil {
		return nil, serviceLogsOutput{}, toolFailure(err)
	}

	return nil, serviceLogsOutput{Lines: lines.Lines}, nil
}

type runStepInput struct {
	Name    string   `json:"name" jsonschema:"a short name for this step, like install or test"`
	Command []string `json:"command" jsonschema:"the command and its arguments, already split"`
	Dir     string   `json:"dir,omitempty" jsonschema:"where to run it, relative to the workspace root"`
	Timeout string   `json:"timeout,omitempty" jsonschema:"how long to give it, like 5m; there is a default"`
}

type runStepOutput struct {
	Name     string `json:"name"`
	ExitCode int    `json:"exit_code"`
	Output   string `json:"output,omitempty"`
	Took     string `json:"took"`
	TimedOut bool   `json:"timed_out,omitempty"`
}

func (t *toolset) runStep(
	ctx context.Context,
	_ *mcp.CallToolRequest,
	input runStepInput,
) (*mcp.CallToolResult, runStepOutput, error) {
	result, err := t.client.RunStep(ctx, t.execution, control.StepRequest{
		Name:    input.Name,
		Dir:     input.Dir,
		Command: input.Command,
		Timeout: input.Timeout,
	})
	if err != nil {
		return nil, runStepOutput{}, toolFailure(err)
	}

	return nil, runStepOutput{
		Name:     result.Name,
		ExitCode: result.ExitCode,
		Output:   result.Output,
		Took:     result.Took,
		TimedOut: result.TimedOut,
	}, nil
}

type allocatePortInput struct {
	Name string `json:"name" jsonschema:"the name to reserve a port under"`
}

type allocatePortOutput struct {
	Name string `json:"name"`
	Port int    `json:"port"`
}

func (t *toolset) allocatePort(
	ctx context.Context,
	_ *mcp.CallToolRequest,
	input allocatePortInput,
) (*mcp.CallToolResult, allocatePortOutput, error) {
	held, err := t.client.AllocatePort(ctx, t.execution, input.Name)
	if err != nil {
		return nil, allocatePortOutput{}, toolFailure(err)
	}

	return nil, allocatePortOutput{Name: held.Name, Port: held.Port}, nil
}

func healthOf(input startServiceInput) control.Health {
	switch {
	case input.HealthHTTP != "":
		return control.Health{
			Kind: string(entity.HealthHTTP), Path: input.HealthHTTP, Port: input.HealthPort,
		}
	case input.HealthLog != "":
		return control.Health{Kind: string(entity.HealthLog), Pattern: input.HealthLog}
	case input.HealthTCP:
		return control.Health{Kind: string(entity.HealthTCP), Port: input.HealthPort}
	default:
		return control.Health{Kind: string(entity.HealthNone)}
	}
}
