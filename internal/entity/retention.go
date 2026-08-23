package entity

import (
	"sort"
	"time"
)

type RunsReport struct {
	Runs    int
	Bytes   int64
	SweptAt time.Time
}

type RunUsage struct {
	Name      string
	Bytes     int64
	Settled   time.Time
	Finished  bool
	Held      bool
	Workspace bool
}

func (u RunUsage) settled(now time.Time, after time.Duration) bool {
	if u.Held || !u.Finished || u.Settled.IsZero() {
		return false
	}

	return !now.Before(u.Settled.Add(after))
}

func Occupied(runs []RunUsage) int64 {
	var held int64

	for _, run := range runs {
		held += run.Bytes
	}

	return held
}

func Retirable(runs []RunUsage, now time.Time, window time.Duration) []string {
	names := make([]string, 0, len(runs))

	for _, run := range oldestFirst(runs) {
		if run.Workspace && run.settled(now, window) {
			names = append(names, run.Name)
		}
	}

	return names
}

func Reapable(runs []RunUsage, now time.Time, maxAge time.Duration, budget int64) []string {
	over := Occupied(runs) - budget
	names := make([]string, 0, len(runs))

	for _, run := range oldestFirst(runs) {
		if !run.settled(now, 0) {
			continue
		}

		if !run.settled(now, maxAge) && over <= 0 {
			continue
		}

		names = append(names, run.Name)
		over -= run.Bytes
	}

	return names
}

func Left(runs []RunUsage, reaped []string) int64 {
	gone := make(map[string]bool, len(reaped))

	for _, name := range reaped {
		gone[name] = true
	}

	var left int64

	for _, run := range runs {
		if !gone[run.Name] {
			left += run.Bytes
		}
	}

	return left
}

func oldestFirst(runs []RunUsage) []RunUsage {
	ordered := make([]RunUsage, len(runs))
	copy(ordered, runs)

	sort.SliceStable(ordered, func(i, j int) bool {
		return ordered[i].Settled.Before(ordered[j].Settled)
	})

	return ordered
}
