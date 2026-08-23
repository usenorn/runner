package entity

import (
	"errors"
	"fmt"
	"strings"
	"time"

	channelv1 "github.com/usenorn/norn/pkg/channel/v1"
)

const (
	ChangeSetMaxRepositories = 50
	ChangeSetRepositoryMax   = 200
	ChangeSetBranchMax       = 255
	ChangeSetRevisionMax     = 64
	ChangeSetPullRequestMax  = 2000
	ChangeSetSummaryMax      = 16000
)

var (
	ErrWorkUncommitted = errors.New(
		"the coding agent left work uncommitted and norn does not commit on its behalf",
	)
	ErrPushNowhere = errors.New("that repository has no remote to push to")
	ErrForgeAbsent = errors.New("no pull request tool on this machine is signed in")
)

type ForgeKind string

const (
	ForgeGitHub ForgeKind = "gh"
	ForgeGitLab ForgeKind = "glab"
)

type Diffstat struct {
	Additions int
	Deletions int
	Files     int
}

type RepositoryChange struct {
	Repository   string
	Branch       string
	BaseSHA      string
	HeadSHA      string
	Commits      int
	Diffstat     Diffstat
	DiffArtifact string
	PullRequest  string
}

type ChangeSet struct {
	Repositories []RepositoryChange
}

func (c ChangeSet) Empty() bool {
	return len(c.Repositories) == 0
}

func (c ChangeSet) Beyond() int {
	return max(0, len(c.Repositories)-ChangeSetMaxRepositories)
}

func (c ChangeSet) Wire() channelv1.ChangeSet {
	kept := c.Repositories
	if len(kept) > ChangeSetMaxRepositories {
		kept = kept[:ChangeSetMaxRepositories]
	}

	repos := make([]channelv1.RepoChange, 0, len(kept))

	for _, change := range kept {
		repos = append(repos, change.wire())
	}

	return channelv1.ChangeSet{Repos: repos}
}

func (r RepositoryChange) wire() channelv1.RepoChange {
	return channelv1.RepoChange{
		Repository:  clip(r.Repository, ChangeSetRepositoryMax),
		Branch:      clip(r.Branch, ChangeSetBranchMax),
		BaseSHA:     clip(r.BaseSHA, ChangeSetRevisionMax),
		HeadSHA:     clip(r.HeadSHA, ChangeSetRevisionMax),
		Commits:     max(r.Commits, 0),
		Additions:   max(r.Diffstat.Additions, 0),
		Deletions:   max(r.Diffstat.Deletions, 0),
		Files:       max(r.Diffstat.Files, 0),
		Diff:        r.DiffArtifact,
		PullRequest: clip(r.PullRequest, ChangeSetPullRequestMax),
	}
}

func ResultOf(summary string, changes ChangeSet, reported time.Time) channelv1.Result {
	return channelv1.Result{
		Summary:   clip(summary, ChangeSetSummaryMax),
		ChangeSet: changes.Wire(),
		Reported:  reported.UTC(),
	}
}

type PullRequest struct {
	Title  string
	Body   string
	Branch string
}

type UncommittedWork struct {
	Repository string
	Files      []string
}

const uncommittedShown = 20

func CommitInjection(left []UncommittedWork) string {
	var said strings.Builder

	said.WriteString(
		"Stop and commit your work. norn collects a run from what is committed on its branches, " +
			"so anything left uncommitted is thrown away when this run finishes. These files are " +
			"still uncommitted:\n",
	)

	for _, held := range left {
		said.WriteString("\n" + held.Repository + ":\n")

		for index, file := range held.Files {
			if index == uncommittedShown {
				fmt.Fprintf(&said, "  and %d more\n", len(held.Files)-uncommittedShown)

				break
			}

			said.WriteString("  " + file + "\n")
		}
	}

	said.WriteString(
		"\nCommit them in each repository with the conventions that repository already uses, " +
			"then end your turn. Do not call complete_task again.",
	)

	return said.String()
}

func UncommittedReason(left []UncommittedWork) string {
	named := make([]string, 0, len(left))

	for _, held := range left {
		named = append(named, fmt.Sprintf("%s (%d files)", held.Repository, len(held.Files)))
	}

	return fmt.Sprintf(
		"%s. It was asked once and left work behind again in %s",
		ErrWorkUncommitted, strings.Join(named, ", "),
	)
}

const attributionLine = "Opened by norn."

func PullRequestTitle(issueKey, title string) string {
	named := strings.TrimSpace(issueKey + " " + strings.TrimSpace(title))
	if named == "" {
		return "Changes from a norn run"
	}

	return clip(named, ChangeSetRepositoryMax)
}

func PullRequestBody(
	issueKey, issueTitle string,
	completion Completion,
	change RepositoryChange,
	attributed bool,
) string {
	var said strings.Builder

	if summary := strings.TrimSpace(completion.Summary); summary != "" {
		said.WriteString(summary + "\n\n")
	}

	if notes := strings.TrimSpace(completion.Notes); notes != "" {
		said.WriteString("For whoever reviews this: " + notes + "\n\n")
	}

	fmt.Fprintf(
		&said, "Issue: %s %s\nBranch: %s\nChanges: %s\n",
		issueKey, strings.TrimSpace(issueTitle), change.Branch, change.Rendered(),
	)

	if attributed {
		said.WriteString("\n" + attributionLine + "\n")
	}

	return said.String()
}

func (r RepositoryChange) Rendered() string {
	return fmt.Sprintf(
		"%s, +%d -%d across %s",
		plural(r.Commits, "commit"), r.Diffstat.Additions, r.Diffstat.Deletions,
		plural(r.Diffstat.Files, "file"),
	)
}

func plural(count int, noun string) string {
	if count == 1 {
		return fmt.Sprintf("%d %s", count, noun)
	}

	return fmt.Sprintf("%d %ss", count, noun)
}

func clip(value string, limit int) string {
	trimmed := strings.TrimSpace(value)

	runes := []rune(trimmed)
	if len(runes) <= limit {
		return trimmed
	}

	return string(runes[:limit])
}
