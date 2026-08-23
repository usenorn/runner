package entity

import (
	"fmt"
	"time"
)

func Approved() string {
	return "somebody approved this run, so norn has everything it needs and the machine is done " +
		"with it"
}

func Keeping(window time.Duration) string {
	return fmt.Sprintf(
		"the workspace, its services and its previews are kept here for %s so somebody can still "+
			"look at them, and the branches stay whatever happens",
		window,
	)
}

func Retired(window time.Duration) string {
	return fmt.Sprintf(
		"this run's workspace was kept for %s and has now been given back: the worktrees are out "+
			"of the original folders, what it was running is stopped and its previews are closed. "+
			"What it did is on its branches, and what happened is still here",
		window,
	)
}

func Kept(until time.Time) string {
	return fmt.Sprintf(
		"somebody asked for longer to look at this run, so its workspace, its services and its "+
			"previews are kept here until %s",
		until.Format(time.RFC3339),
	)
}
