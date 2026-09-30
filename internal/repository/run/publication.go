package run

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/usenorn/runner/internal/entity"
	"github.com/usenorn/runner/internal/pkg/statedir"
)

type storedReviewed struct {
	Name    string `json:"name"`
	Branch  string `json:"branch"`
	Path    string `json:"path"`
	BaseSHA string `json:"baseSha"`
	HeadSHA string `json:"headSha"`
	Remote  string `json:"remote,omitempty"`
	Leased  bool   `json:"leased,omitempty"`
	Lease   string `json:"lease,omitempty"`
}

type storedReview struct {
	Version      int              `json:"version"`
	Revision     int              `json:"revision"`
	Summary      string           `json:"summary,omitempty"`
	Repositories []storedReviewed `json:"repositories"`
}

type storedOutcome struct {
	Repository  string `json:"repository"`
	Branch      string `json:"branch"`
	SHA         string `json:"sha"`
	State       string `json:"state"`
	Step        string `json:"step,omitempty"`
	Failure     string `json:"failure,omitempty"`
	PullRequest string `json:"pullRequest,omitempty"`
}

type storedPublication struct {
	Version      int             `json:"version"`
	Revision     int             `json:"revision"`
	Attempt      int             `json:"attempt"`
	Repositories []storedOutcome `json:"repositories"`
}

func (r *fileRun) metadataPath(name, file string) string {
	return filepath.Join(r.dir.Run(name), entity.RunMetadataDir, file)
}

func (r *fileRun) SaveReview(_ context.Context, name string, review entity.Review) error {
	stored := storedReview{Version: version, Revision: review.Revision, Summary: review.Summary}

	for _, repository := range review.Repositories {
		stored.Repositories = append(stored.Repositories, storedReviewed{
			Name:    repository.Name,
			Branch:  repository.Branch,
			Path:    repository.Path,
			BaseSHA: repository.BaseSHA,
			HeadSHA: repository.HeadSHA,
			Remote:  repository.Remote,
			Leased:  repository.Lease.Known,
			Lease:   repository.Lease.Tip,
		})
	}

	return r.keep(name, entity.RunReviewFile, stored)
}

func (r *fileRun) LoadReview(_ context.Context, name string) (entity.Review, error) {
	var stored storedReview

	if err := readInto(r.metadataPath(name, entity.RunReviewFile), &stored); err != nil {
		if errors.Is(err, entity.ErrSnapshotMissing) {
			return entity.Review{}, entity.ErrReviewMissing
		}

		return entity.Review{}, err
	}

	review := entity.Review{Revision: stored.Revision, Summary: stored.Summary}

	for _, repository := range stored.Repositories {
		review.Repositories = append(review.Repositories, entity.ReviewedRepository{
			Name:    repository.Name,
			Branch:  repository.Branch,
			Path:    repository.Path,
			BaseSHA: repository.BaseSHA,
			HeadSHA: repository.HeadSHA,
			Remote:  repository.Remote,
			Lease:   entity.Lease{Known: repository.Leased, Tip: repository.Lease},
		})
	}

	return review, nil
}

func (r *fileRun) SavePublication(_ context.Context, name string, publication entity.Publication) error {
	stored := storedPublication{
		Version: version, Revision: publication.Revision, Attempt: publication.Attempt,
	}

	for _, held := range publication.Repositories {
		stored.Repositories = append(stored.Repositories, storedOutcome{
			Repository:  held.Repository,
			Branch:      held.Branch,
			SHA:         held.SHA,
			State:       string(held.State),
			Step:        string(held.Step),
			Failure:     held.Failure,
			PullRequest: held.PullRequest,
		})
	}

	return r.keep(name, entity.RunPublicationFile, stored)
}

func (r *fileRun) LoadPublication(_ context.Context, name string) (entity.Publication, error) {
	var stored storedPublication

	if err := readInto(r.metadataPath(name, entity.RunPublicationFile), &stored); err != nil {
		if errors.Is(err, entity.ErrSnapshotMissing) {
			return entity.Publication{}, nil
		}

		return entity.Publication{}, err
	}

	publication := entity.Publication{Revision: stored.Revision, Attempt: stored.Attempt}

	for _, held := range stored.Repositories {
		publication.Repositories = append(publication.Repositories, entity.RepositoryPublication{
			Repository:  held.Repository,
			Branch:      held.Branch,
			SHA:         held.SHA,
			State:       entity.PublicationState(held.State),
			Step:        entity.PublicationStep(held.Step),
			Failure:     held.Failure,
			PullRequest: held.PullRequest,
		})
	}

	return publication, nil
}

func (r *fileRun) keep(name, file string, stored any) error {
	path := r.metadataPath(name, file)

	if err := os.MkdirAll(filepath.Dir(path), dirMode); err != nil {
		return fmt.Errorf("create %s: %w", filepath.Dir(path), err)
	}

	raw, err := json.MarshalIndent(stored, "", "  ")
	if err != nil {
		return fmt.Errorf("write %s for %s: %w", file, name, err)
	}

	return statedir.WriteSecret(path, append(raw, '\n'))
}
