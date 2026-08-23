package entity

import (
	"fmt"
	"time"
)

const byteUnit = 1024

func ByteSize(bytes int64) string {
	if bytes < byteUnit {
		return fmt.Sprintf("%d B", bytes)
	}

	value, exponent := float64(bytes)/byteUnit, 0

	for value >= byteUnit && exponent < 3 {
		value /= byteUnit
		exponent++
	}

	return fmt.Sprintf("%.1f %s", value, [...]string{"KB", "MB", "GB", "TB"}[exponent])
}

func Span(held time.Duration) string {
	switch {
	case held >= 48*time.Hour:
		return plural(int(held.Hours())/24, "day")
	case held >= 2*time.Hour:
		return plural(int(held.Hours()), "hour")
	case held >= time.Minute:
		return plural(int(held.Minutes()), "minute")
	default:
		return held.Round(time.Second).String()
	}
}
