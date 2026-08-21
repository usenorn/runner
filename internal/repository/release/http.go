package release

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/usenorn/runner/internal/config"
	"github.com/usenorn/runner/internal/entity"
	"github.com/usenorn/runner/internal/repository"
)

const (
	maxFeedBytes  = 1 << 20
	acceptHeader  = "application/vnd.github+json"
	apiVersionKey = "X-GitHub-Api-Version"
	apiVersion    = "2022-11-28"
)

type published struct {
	TagName     string    `json:"tag_name"`
	HTMLURL     string    `json:"html_url"`
	PublishedAt time.Time `json:"published_at"`
}

type feed struct {
	http  *http.Client
	url   string
	agent string
}

func New(cfg config.Update, app config.App) repository.Release {
	return &feed{
		http:  &http.Client{Timeout: cfg.Timeout},
		url:   cfg.Feed,
		agent: "norn-runner/" + app.Version,
	}
}

func (r *feed) Latest(ctx context.Context) (entity.Release, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, r.url, nil)
	if err != nil {
		return entity.Release{}, fmt.Errorf("build a request for %s: %w", r.url, err)
	}

	request.Header.Set("Accept", acceptHeader)
	request.Header.Set(apiVersionKey, apiVersion)
	request.Header.Set("User-Agent", r.agent)

	response, err := r.http.Do(request)
	if err != nil {
		return entity.Release{}, fmt.Errorf("%w: %w", entity.ErrReleaseUnavailable, err)
	}

	defer func() { _ = response.Body.Close() }()

	if response.StatusCode == http.StatusNotFound {
		return entity.Release{}, entity.ErrReleaseUnpublished
	}

	if response.StatusCode != http.StatusOK {
		return entity.Release{}, fmt.Errorf(
			"%w: %s answered %s", entity.ErrReleaseUnavailable, r.url, response.Status,
		)
	}

	var latest published

	body, err := io.ReadAll(io.LimitReader(response.Body, maxFeedBytes))
	if err != nil {
		return entity.Release{}, fmt.Errorf("%w: %w", entity.ErrReleaseUnavailable, err)
	}

	if err := json.Unmarshal(body, &latest); err != nil {
		return entity.Release{}, fmt.Errorf(
			"%w: %s answered something that is not a release: %w",
			entity.ErrReleaseUnavailable, r.url, err,
		)
	}

	if latest.TagName == "" {
		return entity.Release{}, fmt.Errorf(
			"%w: %s answered a release with no tag", entity.ErrReleaseUnavailable, r.url,
		)
	}

	return entity.Release{
		Version:     latest.TagName,
		URL:         latest.HTMLURL,
		PublishedAt: latest.PublishedAt,
	}, nil
}
