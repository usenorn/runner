package entity

import (
	"errors"
	"fmt"
	"slices"
	"strings"
)

const (
	ResumePullRequest = "pull_request"

	PullRequestThreadPrefix = "github:"
	PullRequestCommentMax   = 2000
	PullRequestCommentsMax  = 30
	CheckLogMax             = 6000
)

var (
	ErrPullRequestUnreadable = errors.New("this machine cannot read that pull request")
	ErrPullRequestThread     = errors.New("that is not a pull request thread this run can answer")
)

type PullRequestState string

const (
	PullRequestOpen   PullRequestState = "open"
	PullRequestMerged PullRequestState = "merged"
	PullRequestClosed PullRequestState = "closed"
)

type PullRequestCommentKind string

const (
	CommentInline       PullRequestCommentKind = "inline"
	CommentConversation PullRequestCommentKind = "conversation"
	CommentReview       PullRequestCommentKind = "review"
)

type PullRequestComment struct {
	Kind   PullRequestCommentKind
	ID     string
	Parent string
	URL    string
	Author string
	Body   string
	Path   string
	Line   int
}

func (c PullRequestComment) Key() string {
	return string(c.Kind) + ":" + c.ID
}

func (c PullRequestComment) Thread(pullRequest string) string {
	target := c.ID
	if c.Parent != "" {
		target = c.Parent
	}

	return PullRequestThreadPrefix + string(c.Kind) + ":" + target + "@" + pullRequest
}

type PullRequestThread struct {
	Kind        PullRequestCommentKind
	ID          string
	PullRequest string
}

func IsPullRequestThread(thread string) bool {
	return strings.HasPrefix(thread, PullRequestThreadPrefix)
}

func ParsePullRequestThread(thread string) (PullRequestThread, error) {
	rest, found := strings.CutPrefix(thread, PullRequestThreadPrefix)
	if !found {
		return PullRequestThread{}, fmt.Errorf("%w: %q", ErrPullRequestThread, thread)
	}

	key, address, found := strings.Cut(rest, "@")
	if !found {
		return PullRequestThread{}, fmt.Errorf("%w: %q", ErrPullRequestThread, thread)
	}

	kind, id, found := strings.Cut(key, ":")
	if !found || id == "" || address == "" {
		return PullRequestThread{}, fmt.Errorf("%w: %q", ErrPullRequestThread, thread)
	}

	return PullRequestThread{Kind: PullRequestCommentKind(kind), ID: id, PullRequest: address}, nil
}

type FailedCheck struct {
	Name string
	URL  string
	Log  string
}

type PullRequestStatus struct {
	Repository  string
	URL         string
	Dir         string
	State       PullRequestState
	Conflicting bool
	Head        string
	Comments    []PullRequestComment
	Failed      []FailedCheck
}

type PullRequestNews struct {
	Repository string
	URL        string
	Dir        string
	Head       string
	Base       string
	Comments   []PullRequestComment
	Failed     []FailedCheck
	Conflicts  []string
}

func (n PullRequestNews) Empty() bool {
	return len(n.Comments) == 0 && len(n.Failed) == 0 && len(n.Conflicts) == 0
}

type PullRequestReply struct {
	Thread string
	Body   string
}

type WatchCursor struct {
	Baselined bool
	Seen      []string
}

type Watch struct {
	Cursor  WatchCursor
	Replies []PullRequestReply
}

func checkKey(url, head, name string) string {
	return "check:" + url + "@" + head + ":" + name
}

func conflictKey(url, head, base string) string {
	return "conflict:" + url + "@" + head + ".." + base
}

func commentKey(url string, comment PullRequestComment) string {
	return url + "#" + comment.Key()
}

func NewsOf(status PullRequestStatus, refreshed RemoteState, cursor WatchCursor) PullRequestNews {
	news := PullRequestNews{
		Repository: status.Repository,
		URL:        status.URL,
		Dir:        status.Dir,
		Head:       status.Head,
		Base:       refreshed.DefaultTip,
	}

	if cursor.Baselined {
		for _, comment := range status.Comments {
			if !slices.Contains(cursor.Seen, commentKey(status.URL, comment)) {
				news.Comments = append(news.Comments, comment)
			}
		}
	}

	for _, failed := range status.Failed {
		if !slices.Contains(cursor.Seen, checkKey(status.URL, status.Head, failed.Name)) {
			news.Failed = append(news.Failed, failed)
		}
	}

	conflicts := refreshed.Conflicts
	if len(conflicts) == 0 && status.Conflicting {
		conflicts = []string{"(the forge reports a conflict with the base branch)"}
	}

	if len(conflicts) > 0 && !slices.Contains(cursor.Seen, conflictKey(status.URL, status.Head, news.Base)) {
		news.Conflicts = conflicts
	}

	if len(news.Comments) > PullRequestCommentsMax {
		news.Comments = news.Comments[:PullRequestCommentsMax]
	}

	return news
}

func (c WatchCursor) Absorb(status PullRequestStatus, news PullRequestNews) WatchCursor {
	seen := slices.Clone(c.Seen)

	mark := func(key string) {
		if !slices.Contains(seen, key) {
			seen = append(seen, key)
		}
	}

	for _, comment := range status.Comments {
		if !c.Baselined || slices.ContainsFunc(news.Comments, func(held PullRequestComment) bool {
			return held.Key() == comment.Key()
		}) {
			mark(commentKey(status.URL, comment))
		}
	}

	for _, failed := range news.Failed {
		mark(checkKey(status.URL, status.Head, failed.Name))
	}

	if len(news.Conflicts) > 0 {
		mark(conflictKey(status.URL, status.Head, news.Base))
	}

	return WatchCursor{Baselined: true, Seen: seen}
}

func (c WatchCursor) Posted(url string, comment PullRequestComment) WatchCursor {
	key := commentKey(url, comment)
	if slices.Contains(c.Seen, key) {
		return c
	}

	return WatchCursor{Baselined: c.Baselined, Seen: append(slices.Clone(c.Seen), key)}
}

type WatchOutcome string

const (
	WatchGoesOn WatchOutcome = "watching"
	WatchMerged WatchOutcome = "merged"
	WatchClosed WatchOutcome = "closed"
)

func OutcomeOf(statuses []PullRequestStatus) WatchOutcome {
	if len(statuses) == 0 {
		return WatchGoesOn
	}

	merged := 0

	for _, status := range statuses {
		switch status.State {
		case PullRequestOpen:
			return WatchGoesOn
		case PullRequestMerged:
			merged++
		}
	}

	if merged == len(statuses) {
		return WatchMerged
	}

	return WatchClosed
}

func PullRequestsMerged() string {
	return "every pull request this run opened was merged, so the run is finished"
}

func PullRequestsClosed() string {
	return "a pull request this run opened was closed without being merged, so the run stops here"
}

func Watching(addresses []string) string {
	return "the pull requests are open, and this machine watches them for review comments, a " +
		"conflict with the base branch and failed checks: " + strings.Join(addresses, ", ")
}

func NewsLine(news PullRequestNews) string {
	var parts []string

	if count := len(news.Comments); count > 0 {
		parts = append(parts, plural(count, "new review comment"))
	}

	if len(news.Conflicts) > 0 {
		parts = append(parts, "a conflict with the base branch")
	}

	for _, failed := range news.Failed {
		parts = append(parts, "a failed check, "+failed.Name)
	}

	return news.URL + " has " + strings.Join(parts, ", ")
}

func PullRequestBriefing(news []PullRequestNews) string {
	var built strings.Builder

	built.WriteString("The pull requests this run opened need more work. Norn fetched every " +
		"repository first, so origin/<default branch> and origin/<your branch> are current.\n")

	for _, held := range news {
		fmt.Fprintf(&built, "\n## %s — %s\n", held.Repository, held.URL)

		if len(held.Conflicts) > 0 {
			fmt.Fprintf(&built,
				"\nThe branch conflicts with the base branch in: %s. Merge the base branch into "+
					"this branch, resolve each conflict keeping the intent of both sides, run the "+
					"tests, and commit.\n",
				strings.Join(held.Conflicts, ", "),
			)
		}

		for _, failed := range held.Failed {
			fmt.Fprintf(&built, "\nThe check %q failed", failed.Name)

			if failed.URL != "" {
				fmt.Fprintf(&built, " (%s)", failed.URL)
			}

			built.WriteString(". Find the cause, fix it and commit.")

			if log := strings.TrimSpace(failed.Log); log != "" {
				fmt.Fprintf(&built, " The end of its log:\n\n```\n%s\n```\n", tail(log, CheckLogMax))
			} else {
				built.WriteString("\n")
			}
		}

		for _, comment := range held.Comments {
			built.WriteString("\n")
			writeComment(&built, held.URL, comment)
		}
	}

	built.WriteString("\nAddress every comment and fix every failure, commit, and answer each " +
		"comment with `reply_to_review` using its thread id. Your answers are posted on the pull " +
		"request once a person approves the changes in norn. Then call `complete_task`.")

	return built.String()
}

func writeComment(built *strings.Builder, pullRequest string, comment PullRequestComment) {
	fmt.Fprintf(built, "- Thread %s", comment.Thread(pullRequest))

	if comment.Author != "" {
		fmt.Fprintf(built, ", from %s", comment.Author)
	}

	if comment.Path != "" {
		fmt.Fprintf(built, ", on %s", comment.Path)

		if comment.Line > 0 {
			fmt.Fprintf(built, " line %d", comment.Line)
		}
	}

	fmt.Fprintf(built, ":\n  %s\n", strings.ReplaceAll(fit(comment.Body, PullRequestCommentMax), "\n", "\n  "))
}

func PullRequestThreads(news []PullRequestNews) []string {
	var threads []string

	for _, held := range news {
		for _, comment := range held.Comments {
			threads = append(threads, comment.Thread(held.URL))
		}
	}

	return threads
}

func tail(text string, limit int) string {
	if len(text) <= limit {
		return text
	}

	return "…" + text[len(text)-limit:]
}

func ReplyNotPosted(thread PullRequestThread, err error) string {
	return fmt.Sprintf("the answer to %s could not be posted on the pull request: %s", thread.PullRequest, err)
}
