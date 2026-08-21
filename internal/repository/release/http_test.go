package release_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/usenorn/runner/internal/config"
	"github.com/usenorn/runner/internal/entity"
	"github.com/usenorn/runner/internal/repository"
	releaserepo "github.com/usenorn/runner/internal/repository/release"
)

func newFeed(t *testing.T, handler http.HandlerFunc) repository.Release {
	t.Helper()

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	return releaserepo.New(
		config.Update{Check: true, Interval: time.Hour, Timeout: time.Second, Feed: server.URL},
		config.App{Version: "v1.0.0"},
	)
}

func TestThePublishedReleaseIsReadBackWithItsTagAndPage(t *testing.T) {
	var seen *http.Request

	feed := newFeed(t, func(w http.ResponseWriter, r *http.Request) {
		seen = r

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"tag_name": "v1.4.0",
			"html_url": "https://github.com/usenorn/runner/releases/tag/v1.4.0",
			"published_at": "2026-08-21T12:00:00Z",
			"draft": false,
			"prerelease": false
		}`))
	})

	latest, err := feed.Latest(context.Background())
	if err != nil {
		t.Fatalf("read the latest release: %v", err)
	}

	if latest.Version != "v1.4.0" {
		t.Errorf("the release is %q, want the published tag", latest.Version)
	}

	if latest.URL != "https://github.com/usenorn/runner/releases/tag/v1.4.0" {
		t.Errorf("the release points at %q, so nobody can go and read it", latest.URL)
	}

	if !latest.PublishedAt.Equal(time.Date(2026, time.August, 21, 12, 0, 0, 0, time.UTC)) {
		t.Errorf("the release is dated %s, want when it was published", latest.PublishedAt)
	}

	if seen.Header.Get("User-Agent") == "" {
		t.Fatalf("the request carried no user agent, which github refuses outright")
	}
}

func TestAServerWithNoReleaseYetIsNotAFailureToReport(t *testing.T) {
	feed := newFeed(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"message": "Not Found"}`))
	})

	_, err := feed.Latest(context.Background())
	if !errors.Is(err, entity.ErrReleaseUnpublished) {
		t.Fatalf("a repository with no releases returned %v, want it named as unpublished", err)
	}
}

func TestAFeedThatCannotAnswerIsReportedAsUnavailable(t *testing.T) {
	cases := map[string]http.HandlerFunc{
		"a rate limit": func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusForbidden)
		},
		"a server error": func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusBadGateway)
		},
		"a body that is not a release": func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("<html>rate limited</html>"))
		},
		"a release with no tag": func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"html_url": "https://example.invalid"}`))
		},
	}

	for name, handler := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := newFeed(t, handler).Latest(context.Background())
			if !errors.Is(err, entity.ErrReleaseUnavailable) {
				t.Fatalf("%s returned %v, want it reported as unavailable", name, err)
			}
		})
	}
}

func TestAFeedThatNeverAnswersGivesUpInsteadOfHoldingTheDaemon(t *testing.T) {
	release := make(chan struct{})

	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		<-release
	}))

	t.Cleanup(server.Close)
	t.Cleanup(func() { close(release) })

	feed := releaserepo.New(
		config.Update{
			Check: true, Interval: time.Hour, Timeout: 50 * time.Millisecond, Feed: server.URL,
		},
		config.App{Version: "v1.0.0"},
	)

	started := time.Now()

	_, err := feed.Latest(context.Background())
	if !errors.Is(err, entity.ErrReleaseUnavailable) {
		t.Fatalf("a feed that never answers returned %v, want it reported as unavailable", err)
	}

	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("the check took %s to give up, so a start would hang on it", elapsed)
	}
}
