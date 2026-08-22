package entity

import "fmt"

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
