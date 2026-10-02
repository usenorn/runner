package control

import (
	"fmt"
	"net/http"

	"github.com/usenorn/runner/internal/entity"
)

func (s *Server) doctor(w http.ResponseWriter, r *http.Request) {
	examined, err := s.toolchains.Doctor(r.Context())
	if err != nil {
		s.refuse(w, r, err)

		return
	}

	respond(w, r, http.StatusOK, doctorOf(examined))
}

func doctorOf(examined entity.Doctor) Doctor {
	doctor := Doctor{
		Healthy:      examined.Healthy(),
		Sandbox:      "works",
		CommitAuthor: entity.ErrCommitIdentityMissing.Error(),
		PullRequests: "neither gh nor glab is installed and signed in, so runs push branches but " +
			"open no pull requests",
		Codebases: make([]DoctorCodebase, 0, len(examined.Codebases)),
	}

	if examined.Sandbox != "" {
		doctor.Sandbox = examined.Sandbox
	}

	if examined.Identity.Complete() {
		doctor.CommitAuthor = fmt.Sprintf("%s <%s>", examined.Identity.Name, examined.Identity.Email)
	}

	if examined.PullRequests != "" {
		doctor.PullRequests = string(examined.PullRequests) + " is signed in"
	}

	for _, codebase := range examined.Codebases {
		held := DoctorCodebase{
			Name:    codebase.Name,
			Root:    codebase.Root,
			Failure: codebase.Failure,
			Tools:   make([]DoctorTool, 0, len(codebase.Report)),
		}

		for _, check := range codebase.Report {
			held.Tools = append(held.Tools, DoctorTool{
				Name:    check.Tool.Name,
				State:   string(check.State),
				Path:    check.Path,
				Version: check.Version,
				Needed:  check.Needed,
				Summary: check.Line(),
			})
		}

		doctor.Codebases = append(doctor.Codebases, held)
	}

	return doctor
}
