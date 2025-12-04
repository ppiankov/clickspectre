package config

import (
	"fmt"
	"regexp"
	"strconv"
	"time"
)

// ParseDuration parses a duration string with support for days (d)
// Supports: s (seconds), m (minutes), h (hours), d (days)
// Examples: "30d", "7d", "168h", "5m", "30s"
func ParseDuration(s string) (time.Duration, error) {
	// Pattern to match number followed by unit
	pattern := regexp.MustCompile(`^(\d+)([smhd])$`)
	matches := pattern.FindStringSubmatch(s)

	if matches == nil {
		// Fall back to standard Go duration parsing
		return time.ParseDuration(s)
	}

	value, err := strconv.Atoi(matches[1])
	if err != nil {
		return 0, fmt.Errorf("invalid duration value: %s", matches[1])
	}

	unit := matches[2]

	switch unit {
	case "s":
		return time.Duration(value) * time.Second, nil
	case "m":
		return time.Duration(value) * time.Minute, nil
	case "h":
		return time.Duration(value) * time.Hour, nil
	case "d":
		return time.Duration(value) * 24 * time.Hour, nil
	default:
		return 0, fmt.Errorf("unknown time unit: %s", unit)
	}
}
