package control

import (
	"net/http"

	"github.com/usenorn/runner/internal/entity"
)

func (s *Server) refreshRemote(w http.ResponseWriter, r *http.Request) {
	states, err := s.executions.Refresh(r.Context(), r.PathValue("executionId"))
	if err != nil {
		s.refuse(w, r, err)

		return
	}

	remote := RemoteRefresh{Repositories: make([]RemoteRepository, 0, len(states))}

	for _, state := range states {
		remote.Repositories = append(remote.Repositories, remoteRepositoryOf(state))
	}

	respond(w, r, http.StatusOK, remote)
}

func remoteRepositoryOf(state entity.RemoteState) RemoteRepository {
	return RemoteRepository{
		Repository:    state.Repository,
		DefaultBranch: state.Default,
		DefaultTip:    state.DefaultTip,
		Behind:        state.Behind,
		Ahead:         state.Ahead,
		Branch:        state.Branch,
		BranchTip:     state.BranchTip,
		Unmerged:      state.Unmerged,
		Conflicts:     state.Conflicts,
		Failure:       state.Failure,
		Summary:       state.Line(),
	}
}
