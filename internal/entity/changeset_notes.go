package entity

import (
	"fmt"
	"strings"
)

func DiffLabel(repository string) string {
	return sanitiseRef(repository) + ".diff.gz"
}

func ChangeSetOverflow(beyond int) string {
	return fmt.Sprintf(
		"this run touched %d more repositories than norn records on one change set, so the %d "+
			"largest are on it and the rest are on their branches only",
		beyond, ChangeSetMaxRepositories,
	)
}

func DiffUnreadable(repository string, cause error) string {
	return fmt.Sprintf(
		"the diff for %s could not be read, so the change set carries its numbers without the "+
			"patch: %s",
		repository, cause,
	)
}

func DiffTooLarge(repository string, size, limit int64) string {
	return fmt.Sprintf(
		"the diff for %s is %s compressed and norn keeps %s, so it is not attached; the branch "+
			"carries the whole of it",
		repository, ByteSize(size), ByteSize(limit),
	)
}

func DiffUnkept(repository string, cause error) string {
	return fmt.Sprintf(
		"the diff for %s could not be sent to norn, so review it on the branch: %s",
		repository, cause,
	)
}

func CommitAsked(left []UncommittedWork) string {
	named := make([]string, 0, len(left))

	for _, held := range left {
		named = append(named, fmt.Sprintf("%s (%d files)", held.Repository, len(held.Files)))
	}

	return "the coding agent left work uncommitted in " + strings.Join(named, ", ") +
		", so it has been asked to commit it rather than have norn commit for it"
}

func Pushed(repository, branch string) string {
	return fmt.Sprintf("pushed %s to %s", branch, repository)
}

func PushSkipped(repository string, cause error) string {
	return fmt.Sprintf(
		"%s was not pushed, so its work stays on this machine: %s", repository, cause,
	)
}

func PushRefused(repository, branch string, cause error) string {
	return fmt.Sprintf(
		"pushing %s to %s was refused, so its work stays on this machine: %s",
		branch, repository, cause,
	)
}

func PullRequestOpened(repository, address string) string {
	return fmt.Sprintf("opened a pull request for %s: %s", repository, address)
}

func PullRequestAmended(repository, address string) string {
	return fmt.Sprintf(
		"%s already had a pull request open, so the new commits went onto it: %s",
		repository, address,
	)
}

func PullRequestSkipped(repository string) string {
	return fmt.Sprintf(
		"%s: %s, so the branch is pushed and nobody opened a pull request for it",
		repository, ErrForgeAbsent,
	)
}

func PullRequestScrubbed(repository string, kinds []string) string {
	return fmt.Sprintf(
		"%s: the issue title carried %s, so it was taken out before the pull "+
			"request was opened",
		repository, strings.Join(kinds, " and "),
	)
}

func PullRequestRefused(repository string, cause error) string {
	return fmt.Sprintf(
		"no pull request was opened for %s, so open one from its branch: %s", repository, cause,
	)
}

func Finalised(changes ChangeSet) string {
	if changes.Empty() {
		return "the run finished without changing any repository, so there is nothing to review " +
			"but what it said"
	}

	named := make([]string, 0, len(changes.Repositories))

	for _, change := range changes.Repositories {
		named = append(named, fmt.Sprintf("%s (%s)", change.Repository, change.Rendered()))
	}

	return "collected what the run changed in " + strings.Join(named, ", ")
}
