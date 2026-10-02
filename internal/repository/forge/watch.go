package forge

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/usenorn/runner/internal/entity"
)

var (
	pullRequestAddress  = regexp.MustCompile(`^https://([^/]+)/([^/]+)/([^/]+)/pull/(\d+)`)
	conversationComment = regexp.MustCompile(`#issuecomment-(\d+)$`)
	jobAddress          = regexp.MustCompile(`/actions/runs/\d+/job/(\d+)`)
)

type pullRequestRef struct {
	host   string
	owner  string
	name   string
	number string
}

func (p pullRequestRef) repository() string {
	return p.owner + "/" + p.name
}

func (p pullRequestRef) api(path string) string {
	return fmt.Sprintf("repos/%s/pulls/%s%s", p.repository(), p.number, path)
}

func refOf(address string) (pullRequestRef, error) {
	found := pullRequestAddress.FindStringSubmatch(address)
	if found == nil {
		return pullRequestRef{}, fmt.Errorf("%w: %s is not a GitHub pull request", entity.ErrPullRequestUnreadable, address)
	}

	return pullRequestRef{host: found[1], owner: found[2], name: found[3], number: found[4]}, nil
}

type viewed struct {
	State     string `json:"state"`
	Mergeable string `json:"mergeable"`
	Head      string `json:"headRefOid"`
	Checks    []struct {
		Name       string `json:"name"`
		Context    string `json:"context"`
		Conclusion string `json:"conclusion"`
		State      string `json:"state"`
		Details    string `json:"detailsUrl"`
		Target     string `json:"targetUrl"`
	} `json:"statusCheckRollup"`
}

type author struct {
	Login string `json:"login"`
	Type  string `json:"type"`
}

func (a author) bot() bool {
	return a.Type == "Bot"
}

type posted struct {
	ID        int64  `json:"id"`
	User      author `json:"user"`
	Body      string `json:"body"`
	State     string `json:"state"`
	Path      string `json:"path"`
	Line      int    `json:"line"`
	URL       string `json:"html_url"`
	InReplyTo int64  `json:"in_reply_to_id"`
}

func (r *cliForge) Status(ctx context.Context, dir, address string) (entity.PullRequestStatus, error) {
	ref, err := refOf(address)
	if err != nil {
		return entity.PullRequestStatus{}, err
	}

	out, err := r.run(ctx, dir, entity.ForgeGitHub, "pr", "view", address,
		"--json", "state,mergeable,headRefOid,statusCheckRollup")
	if err != nil {
		return entity.PullRequestStatus{}, fmt.Errorf("%w: %w", entity.ErrPullRequestUnreadable, err)
	}

	var seen viewed
	if err := json.Unmarshal([]byte(out), &seen); err != nil {
		return entity.PullRequestStatus{}, fmt.Errorf("%w: %w", entity.ErrPullRequestUnreadable, err)
	}

	status := entity.PullRequestStatus{
		URL:         address,
		Dir:         dir,
		State:       stateOf(seen.State),
		Conflicting: seen.Mergeable == "CONFLICTING",
		Head:        seen.Head,
	}

	for _, listed := range []struct {
		kind entity.PullRequestCommentKind
		path string
	}{
		{entity.CommentConversation, fmt.Sprintf("repos/%s/issues/%s/comments", ref.repository(), ref.number)},
		{entity.CommentReview, ref.api("/reviews")},
		{entity.CommentInline, ref.api("/comments")},
	} {
		comments, err := r.comments(ctx, dir, ref, listed.kind, listed.path)
		if err != nil {
			return entity.PullRequestStatus{}, err
		}

		status.Comments = append(status.Comments, comments...)
	}

	for _, check := range seen.Checks {
		if failing(check.Conclusion) || failing(check.State) {
			status.Failed = append(status.Failed, entity.FailedCheck{
				Name: firstOf(check.Name, check.Context),
				URL:  firstOf(check.Details, check.Target),
			})
		}
	}

	return status, nil
}

func (r *cliForge) comments(
	ctx context.Context,
	dir string,
	ref pullRequestRef,
	kind entity.PullRequestCommentKind,
	path string,
) ([]entity.PullRequestComment, error) {
	out, err := r.run(ctx, dir, entity.ForgeGitHub, "api", "--hostname", ref.host, "--paginate", path)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", entity.ErrPullRequestUnreadable, err)
	}

	var held []posted

	decoder := json.NewDecoder(strings.NewReader(out))

	for decoder.More() {
		var page []posted
		if err := decoder.Decode(&page); err != nil {
			return nil, fmt.Errorf("%w: %w", entity.ErrPullRequestUnreadable, err)
		}

		held = append(held, page...)
	}

	comments := make([]entity.PullRequestComment, 0, len(held))

	for _, comment := range held {
		if comment.User.bot() || (kind == entity.CommentReview && !worthAnswering(comment)) {
			continue
		}

		found := entity.PullRequestComment{
			Kind: kind, ID: fmt.Sprint(comment.ID), URL: comment.URL, Author: comment.User.Login,
			Body: firstOf(comment.Body, "requested changes"), Path: comment.Path, Line: comment.Line,
		}

		if comment.InReplyTo != 0 {
			found.Parent = fmt.Sprint(comment.InReplyTo)
		}

		comments = append(comments, found)
	}

	return comments, nil
}

func worthAnswering(review posted) bool {
	return strings.TrimSpace(review.Body) != "" || review.State == "CHANGES_REQUESTED"
}

func (r *cliForge) FailedLog(ctx context.Context, dir string, check entity.FailedCheck) (string, error) {
	job := idIn(jobAddress, check.URL)
	if job == "" {
		return "", nil
	}

	parts := strings.Split(strings.TrimPrefix(check.URL, "https://"), "/")
	if len(parts) < 3 {
		return "", nil
	}

	repository := parts[1] + "/" + parts[2]

	return r.run(ctx, dir, entity.ForgeGitHub, "run", "view", "--repo", repository, "--job", job, "--log-failed")
}

func (r *cliForge) Reply(
	ctx context.Context,
	dir string,
	thread entity.PullRequestThread,
	body string,
) (entity.PullRequestComment, error) {
	ref, err := refOf(thread.PullRequest)
	if err != nil {
		return entity.PullRequestComment{}, err
	}

	if thread.Kind == entity.CommentInline {
		out, err := r.run(ctx, dir, entity.ForgeGitHub, "api", "--hostname", ref.host, "--method", "POST",
			ref.api("/comments/"+thread.ID+"/replies"), "--field", "body="+body)
		if err != nil {
			return entity.PullRequestComment{}, err
		}

		var answered posted
		if err := json.Unmarshal([]byte(out), &answered); err != nil {
			return entity.PullRequestComment{}, fmt.Errorf("read the reply github posted: %w", err)
		}

		return entity.PullRequestComment{Kind: entity.CommentInline, ID: fmt.Sprint(answered.ID)}, nil
	}

	out, err := r.run(ctx, dir, entity.ForgeGitHub, "pr", "comment", thread.PullRequest, "--body", body)
	if err != nil {
		return entity.PullRequestComment{}, err
	}

	return entity.PullRequestComment{
		Kind: entity.CommentConversation, ID: idIn(conversationComment, strings.TrimSpace(out)),
	}, nil
}

func stateOf(state string) entity.PullRequestState {
	switch state {
	case "MERGED":
		return entity.PullRequestMerged
	case "CLOSED":
		return entity.PullRequestClosed
	default:
		return entity.PullRequestOpen
	}
}

func failing(conclusion string) bool {
	switch conclusion {
	case "FAILURE", "TIMED_OUT", "ACTION_REQUIRED", "STARTUP_FAILURE", "ERROR":
		return true
	default:
		return false
	}
}

func idIn(pattern *regexp.Regexp, address string) string {
	found := pattern.FindStringSubmatch(address)
	if found == nil {
		return ""
	}

	return found[1]
}

func firstOf(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}

	return ""
}
