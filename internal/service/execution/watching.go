package execution

import (
	"context"
	"log/slog"
	"time"

	channelv1 "github.com/usenorn/norn/pkg/channel/v1"

	"github.com/usenorn/runner/internal/entity"
	"github.com/usenorn/runner/internal/observability/logging"
)

func (s *executionsService) patrol(ctx context.Context) {
	if s.results.WatchEvery <= 0 {
		<-ctx.Done()

		return
	}

	ticker := time.NewTicker(s.results.WatchEvery)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			for _, execution := range s.watched() {
				s.complain(ctx, execution.ID, s.inspect(ctx, execution))
			}
		}
	}
}

func (s *executionsService) watched() []entity.Execution {
	s.mu.Lock()
	defer s.mu.Unlock()

	var watching []entity.Execution

	for _, execution := range s.held {
		if execution.State == channelv1.StateWatching {
			watching = append(watching, execution)
		}
	}

	return watching
}

type watchedPullRequest struct {
	address    string
	repository entity.SnapshotRepository
}

func (s *executionsService) inspect(ctx context.Context, execution entity.Execution) error {
	if _, err := s.runs.LoadResume(ctx, execution.ID); err == nil {
		return nil
	}

	snapshot, err := s.runs.Load(ctx, execution.ID)
	if err != nil {
		return err
	}

	publication, err := s.runs.LoadPublication(ctx, execution.ID)
	if err != nil {
		return err
	}

	watch, err := s.runs.LoadWatch(ctx, execution.ID)
	if err != nil {
		return err
	}

	statuses := s.statuses(ctx, execution, watchedOf(snapshot, publication))

	switch entity.OutcomeOf(statuses) {
	case entity.WatchMerged:
		return s.conclude(ctx, execution, entity.PullRequestsMerged())
	case entity.WatchClosed:
		if err := s.move(ctx, execution, channelv1.StateFailed, entity.PullRequestsClosed()); err != nil {
			return err
		}

		return s.finished(ctx, execution.ID)
	}

	refreshed := map[string]entity.RemoteState{}
	for _, state := range s.snapshots.Refresh(ctx, snapshot) {
		refreshed[state.Repository] = state
	}

	var news []entity.PullRequestNews

	for _, status := range statuses {
		found := entity.NewsOf(status, refreshed[status.Repository], watch.Cursor)
		watch.Cursor = watch.Cursor.Absorb(status, found)

		if !found.Empty() {
			news = append(news, s.withLogs(ctx, found))
		}
	}

	if len(news) == 0 {
		return s.runs.SaveWatch(ctx, execution.ID, watch)
	}

	instruction := channelv1.Instruction{
		Reason:      entity.ResumePullRequest,
		Stage:       channelv1.StageImplementation,
		Instruction: entity.PullRequestBriefing(news),
		Threads:     entity.PullRequestThreads(news),
	}

	if err := s.runs.SaveResume(ctx, execution.ID, instruction); err != nil {
		return err
	}

	if err := s.runs.SaveWatch(ctx, execution.ID, watch); err != nil {
		return err
	}

	for _, found := range news {
		s.complain(ctx, execution.ID, s.note(ctx, execution.ID, channelv1.EventNote, entity.NewsLine(found)))
	}

	s.admit(ctx, resuming(execution.ID, instruction))

	return nil
}

func (s *executionsService) baseline(
	ctx context.Context,
	execution entity.Execution,
	publication entity.Publication,
) error {
	watch, err := s.runs.LoadWatch(ctx, execution.ID)
	if err != nil || watch.Cursor.Baselined {
		return err
	}

	snapshot, err := s.runs.Load(ctx, execution.ID)
	if err != nil {
		return err
	}

	for _, status := range s.statuses(ctx, execution, watchedOf(snapshot, publication)) {
		watch.Cursor = watch.Cursor.Absorb(status, entity.PullRequestNews{})
	}

	return s.runs.SaveWatch(ctx, execution.ID, watch)
}

func watchedOf(snapshot entity.Snapshot, publication entity.Publication) []watchedPullRequest {
	var watched []watchedPullRequest

	for _, published := range publication.Repositories {
		if published.PullRequest == "" {
			continue
		}

		for _, repository := range snapshot.Repositories {
			if repository.Name == published.Repository {
				watched = append(watched, watchedPullRequest{address: published.PullRequest, repository: repository})
			}
		}
	}

	return watched
}

func (s *executionsService) statuses(
	ctx context.Context,
	execution entity.Execution,
	watched []watchedPullRequest,
) []entity.PullRequestStatus {
	statuses := make([]entity.PullRequestStatus, 0, len(watched))

	for _, pullRequest := range watched {
		status, err := s.forges.Status(ctx, pullRequest.repository.Path, pullRequest.address)
		if err != nil {
			logging.From(ctx).WarnContext(
				ctx,
				"this machine could not read a pull request it is watching",
				slog.String("execution_id", execution.ID),
				slog.String("pull_request", pullRequest.address),
				slog.String("error", err.Error()),
			)

			status = entity.PullRequestStatus{URL: pullRequest.address, State: entity.PullRequestOpen}
		}

		status.Repository = pullRequest.repository.RelPath
		status.Dir = pullRequest.repository.Path
		statuses = append(statuses, status)
	}

	return statuses
}

func (s *executionsService) withLogs(ctx context.Context, news entity.PullRequestNews) entity.PullRequestNews {
	for index, failed := range news.Failed {
		if log, err := s.forges.FailedLog(ctx, news.Dir, failed); err == nil {
			news.Failed[index].Log = log
		}
	}

	return news
}

func (s *executionsService) answer(
	ctx context.Context,
	execution entity.Execution,
	publication entity.Publication,
) {
	watch, err := s.runs.LoadWatch(ctx, execution.ID)
	if err != nil || len(watch.Replies) == 0 {
		return
	}

	snapshot, err := s.runs.Load(ctx, execution.ID)
	if err != nil {
		return
	}

	dirs := map[string]string{}
	for _, pullRequest := range watchedOf(snapshot, publication) {
		dirs[pullRequest.address] = pullRequest.repository.Path
	}

	for _, reply := range watch.Replies {
		thread, err := entity.ParsePullRequestThread(reply.Thread)
		if err != nil {
			continue
		}

		posted, err := s.forges.Reply(ctx, dirs[thread.PullRequest], thread, reply.Body)
		if err != nil {
			s.complain(ctx, execution.ID, s.note(ctx, execution.ID, channelv1.EventNote,
				entity.ReplyNotPosted(thread, err)))

			continue
		}

		watch.Cursor = watch.Cursor.Posted(thread.PullRequest, posted)
	}

	watch.Replies = nil

	s.complain(ctx, execution.ID, s.runs.SaveWatch(ctx, execution.ID, watch))
}
