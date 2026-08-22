package control

import (
	"encoding/json"
	"net/http"

	"github.com/google/uuid"

	"github.com/usenorn/runner/internal/entity"
	"github.com/usenorn/runner/internal/service"
)

func (s *Server) inspect(w http.ResponseWriter, r *http.Request) {
	var request InspectRequest

	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		respond(w, r, http.StatusBadRequest, Failure{
			Reason: ReasonRefused, Message: "that inspect request is malformed",
		})

		return
	}

	scan, err := s.codebases.Scan(r.Context(), request.Root)
	if err != nil {
		s.refuse(w, r, err)

		return
	}

	respond(w, r, http.StatusOK, scanOf(scan))
}

func (s *Server) accept(w http.ResponseWriter, r *http.Request) {
	var scan Scan

	if err := json.NewDecoder(r.Body).Decode(&scan); err != nil {
		respond(w, r, http.StatusBadRequest, Failure{
			Reason: ReasonRefused, Message: "that inventory is malformed",
		})

		return
	}

	accepted, err := s.codebases.Accept(r.Context(), serviceScanOf(scan))
	if err != nil {
		s.refuse(w, r, err)

		return
	}

	respond(w, r, http.StatusOK, Accepted{
		CodebaseID:   accepted.ID.String(),
		Name:         accepted.Name,
		RootPath:     accepted.RootPath,
		Repositories: len(accepted.Confirmed.Listed()),
		Server:       s.runner.Server,
	})
}

func (s *Server) heldCodebases(r *http.Request) []StatusCodebase {
	held, err := s.codebases.List(r.Context())
	if err != nil {
		return nil
	}

	codebases := make([]StatusCodebase, 0, len(held))

	for _, codebase := range held {
		codebases = append(codebases, StatusCodebase{
			CodebaseID:   codebase.ID.String(),
			Name:         codebase.Name,
			RootPath:     codebase.RootPath,
			Repositories: len(codebase.Reported.Listed()),
			Drifted:      codebase.Drifted(),
		})
	}

	return codebases
}

func scanOf(scan service.Scan) Scan {
	held := Scan{
		Inventory: inventoryOf(scan.Inventory),
		Warnings:  scan.Warnings,
		Connected: scan.Connected,
		Reconcile: scan.Reconcile,
		Drift: Drift{
			Added:   scan.Drift.Added,
			Removed: scan.Drift.Removed,
			Changed: scan.Drift.Changed,
		},
	}

	if scan.CodebaseID != uuid.Nil {
		held.CodebaseID = scan.CodebaseID.String()
	}

	return held
}

func inventoryOf(inventory entity.Inventory) Inventory {
	repositories := make([]Repository, 0, len(inventory.Repositories))
	for _, repository := range inventory.Repositories {
		repositories = append(repositories, Repository{
			Name:          repository.Name,
			RelPath:       repository.RelPath,
			Kind:          string(repository.Kind),
			DefaultBranch: repository.DefaultBranch,
			Remote: Remote{
				Hash:     repository.Remote.Hash,
				Host:     repository.Remote.Host,
				PathTail: repository.Remote.PathTail,
			},
			CommonDir: repository.CommonDir,
			Parent:    repository.Parent,
		})
	}

	runtimes := make([]string, 0, len(inventory.Runtimes))
	for _, runtime := range inventory.Runtimes {
		runtimes = append(runtimes, string(runtime))
	}

	tools := make([]Tool, 0, len(inventory.Tools))
	for _, tool := range inventory.Tools {
		tools = append(tools, Tool{Name: tool.Name, Version: tool.Version})
	}

	return Inventory{
		Name:         inventory.Name,
		RootPath:     inventory.RootPath,
		Repositories: repositories,
		SharedFiles:  inventory.SharedFiles,
		Runtimes:     runtimes,
		Tools:        tools,
		ScannedAt:    inventory.ScannedAt,
	}
}

func serviceScanOf(scan Scan) service.Scan {
	held := service.Scan{
		Inventory: entityInventoryOf(scan.Inventory),
		Warnings:  scan.Warnings,
		Connected: scan.Connected,
		Reconcile: scan.Reconcile,
		Drift: entity.Drift{
			Added:   scan.Drift.Added,
			Removed: scan.Drift.Removed,
			Changed: scan.Drift.Changed,
		},
	}

	if parsed, err := uuid.Parse(scan.CodebaseID); err == nil {
		held.CodebaseID = parsed
	}

	return held
}

func entityInventoryOf(inventory Inventory) entity.Inventory {
	repositories := make([]entity.Repository, 0, len(inventory.Repositories))
	for _, repository := range inventory.Repositories {
		repositories = append(repositories, entity.Repository{
			Name:          repository.Name,
			RelPath:       repository.RelPath,
			Kind:          entity.RepositoryKind(repository.Kind),
			DefaultBranch: repository.DefaultBranch,
			Remote: entity.RemoteFingerprint{
				Hash:     repository.Remote.Hash,
				Host:     repository.Remote.Host,
				PathTail: repository.Remote.PathTail,
			},
			CommonDir: repository.CommonDir,
			Parent:    repository.Parent,
		})
	}

	runtimes := make([]entity.Runtime, 0, len(inventory.Runtimes))
	for _, runtime := range inventory.Runtimes {
		runtimes = append(runtimes, entity.Runtime(runtime))
	}

	tools := make([]entity.Tool, 0, len(inventory.Tools))
	for _, tool := range inventory.Tools {
		tools = append(tools, entity.Tool{Name: tool.Name, Version: tool.Version})
	}

	return entity.Inventory{
		Name:         inventory.Name,
		RootPath:     inventory.RootPath,
		Repositories: repositories,
		SharedFiles:  inventory.SharedFiles,
		Runtimes:     runtimes,
		Tools:        tools,
		ScannedAt:    inventory.ScannedAt,
	}
}
