package entity

import (
	"fmt"
	"strings"
)

const RemoteConflictsShown = 20

type Divergence struct {
	Ahead  int
	Behind int
}

type RemoteState struct {
	Repository string
	Default    string
	DefaultTip string
	Behind     int
	Ahead      int
	Branch     string
	BranchTip  string
	Unmerged   int
	Conflicts  []string
	Failure    string
}

func (s RemoteState) Moved() bool {
	return s.Behind > 0 || s.Unmerged > 0 || len(s.Conflicts) > 0 || s.Failure != ""
}

func (s RemoteState) Line() string {
	if s.Failure != "" {
		return fmt.Sprintf("%s: could not be fetched, so origin/%s is what this machine already had: %s",
			s.Repository, s.Default, s.Failure)
	}

	parts := []string{fmt.Sprintf(
		"%s: origin/%s is %s, %d commits this branch does not have yet, %d of this branch's not on it",
		s.Repository, s.Default, ShortSHA(s.DefaultTip), s.Behind, s.Ahead,
	)}

	if s.Unmerged > 0 {
		parts = append(parts, fmt.Sprintf(
			"origin/%s has %d commits somebody pushed to this branch that it does not have yet",
			s.Branch, s.Unmerged,
		))
	}

	if len(s.Conflicts) > 0 {
		shown := s.Conflicts[:min(len(s.Conflicts), RemoteConflictsShown)]
		parts = append(parts, fmt.Sprintf(
			"merging origin/%s would conflict in %s", s.Default, strings.Join(shown, ", "),
		))
	}

	return strings.Join(parts, "; ")
}

func RemoteBriefing(states []RemoteState) string {
	moved := make([]string, 0, len(states))

	for _, state := range states {
		if state.Moved() {
			moved = append(moved, "- "+state.Line())
		}
	}

	if len(moved) == 0 {
		return ""
	}

	return strings.Join(append([]string{
		"Norn fetched every repository's remote before handing this back to you:",
	}, append(moved,
		"Bring in what the remote has with a local `git merge` or `git rebase` of the origin "+
			"branches named above, resolve any conflict keeping the intent of both sides, and "+
			"commit. Call `refresh_remote` to fetch again; never fetch or push yourself.",
	)...), "\n")
}
