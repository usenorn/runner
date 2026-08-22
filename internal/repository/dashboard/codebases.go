package dashboard

import (
	"context"
	"fmt"
	"net/http"

	"github.com/google/uuid"

	api "github.com/usenorn/norn/pkg/http/v1/dashboard"

	"github.com/usenorn/runner/internal/entity"
	"github.com/usenorn/runner/internal/repository"
)

func (r *httpDashboard) ConnectCodebase(
	ctx context.Context,
	token string,
	inventory entity.Inventory,
) (repository.ConnectedCodebase, error) {
	name := inventory.Name
	repositories := repositoriesOf(inventory)
	shared := inventory.SharedFiles
	runtimes := runtimesOf(inventory)
	tools := toolsOf(inventory)

	response, err := r.client.ConnectCodebaseWithResponse(ctx, api.ConnectCodebaseJSONRequestBody{
		Name:         &name,
		RootPath:     inventory.RootPath,
		Repositories: &repositories,
		SharedFiles:  &shared,
		Runtimes:     &runtimes,
		Tools:        &tools,
	}, bearer(token))
	if err != nil {
		return repository.ConnectedCodebase{}, r.unreachable(err)
	}

	if response.JSON200 == nil {
		return repository.ConnectedCodebase{}, r.codebaseRefusal(
			response.HTTPResponse, response.Body,
		)
	}

	return connectedOf(*response.JSON200), nil
}

func (r *httpDashboard) ConfirmCodebase(
	ctx context.Context,
	token string,
	id uuid.UUID,
) (repository.ConnectedCodebase, error) {
	response, err := r.client.ConfirmCodebaseWithResponse(ctx, id, bearer(token))
	if err != nil {
		return repository.ConnectedCodebase{}, r.unreachable(err)
	}

	if response.JSON200 == nil {
		return repository.ConnectedCodebase{}, r.codebaseRefusal(
			response.HTTPResponse, response.Body,
		)
	}

	return connectedOf(*response.JSON200), nil
}

func (r *httpDashboard) ListCodebases(
	ctx context.Context,
	token string,
) ([]repository.ConnectedCodebase, error) {
	response, err := r.client.ListCurrentRunnerCodebasesWithResponse(ctx, bearer(token))
	if err != nil {
		return nil, r.unreachable(err)
	}

	if response.JSON200 == nil {
		return nil, r.codebaseRefusal(response.HTTPResponse, response.Body)
	}

	connected := make([]repository.ConnectedCodebase, 0, len(*response.JSON200))
	for _, codebase := range *response.JSON200 {
		connected = append(connected, connectedOf(codebase))
	}

	return connected, nil
}

func (r *httpDashboard) codebaseRefusal(response *http.Response, body []byte) error {
	switch response.StatusCode {
	case http.StatusUnauthorized:
		return entity.ErrCredentialInvalid
	case http.StatusForbidden:
		return entity.ErrCodebaseNotRunner
	case http.StatusNotFound:
		return entity.ErrCodebaseNotConnected
	case http.StatusConflict:
		return codebaseConflict(body)
	case http.StatusUnprocessableEntity:
		return refused(entity.ErrCodebaseRefused, body)
	default:
		return r.refusal(response, body)
	}
}

func codebaseConflict(body []byte) error {
	switch api.CodebaseProblemCode(code(body)) {
	case api.CodebaseProblemCodeCodebaseRootTaken:
		return entity.ErrCodebaseAlreadyConnected
	case api.CodebaseProblemCodeCodebaseNotDrifted:
		return entity.ErrCodebaseNotDrifted
	case api.CodebaseProblemCodeCodebaseDisconnected:
		return entity.ErrCodebaseNotConnected
	default:
		return refused(entity.ErrCodebaseRefused, body)
	}
}

func refused(sentinel error, body []byte) error {
	if detail := detailOf(body); detail != "" {
		return fmt.Errorf("%w: %s", sentinel, detail)
	}

	return sentinel
}

func connectedOf(codebase api.Codebase) repository.ConnectedCodebase {
	return repository.ConnectedCodebase{
		ID:       codebase.Id,
		Name:     codebase.Name,
		RootPath: codebase.RootPath,
		Drifted:  codebase.State == api.CodebaseStateDrift,
	}
}

func repositoriesOf(inventory entity.Inventory) []api.CodebaseRepository {
	listed := inventory.Listed()
	repositories := make([]api.CodebaseRepository, 0, len(listed))

	for _, held := range listed {
		repository := api.CodebaseRepository{Name: held.Name, RelPath: held.RelPath}

		if held.DefaultBranch != "" {
			branch := held.DefaultBranch
			repository.DefaultBranch = &branch
		}

		if held.Remote.Known() {
			hash, host, tail := held.Remote.Hash, held.Remote.Host, held.Remote.PathTail
			repository.Remote = &api.RemoteFingerprint{Hash: &hash, Host: &host, PathTail: &tail}
		}

		repositories = append(repositories, repository)
	}

	return repositories
}

func runtimesOf(inventory entity.Inventory) []api.CodebaseRuntime {
	runtimes := make([]api.CodebaseRuntime, 0, len(inventory.Runtimes))
	for _, runtime := range inventory.Runtimes {
		runtimes = append(runtimes, api.CodebaseRuntime(runtime))
	}

	return runtimes
}

func toolsOf(inventory entity.Inventory) []api.CodingTool {
	tools := make([]api.CodingTool, 0, len(inventory.Tools))

	for _, held := range inventory.Tools {
		tool := api.CodingTool{Name: held.Name}

		if held.Version != "" {
			version := held.Version
			tool.Version = &version
		}

		tools = append(tools, tool)
	}

	return tools
}
