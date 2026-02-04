package config

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var durationRegex = regexp.MustCompile(`(?i)^(\d+)\s*([dhms]?)$`)

// DefaultMaxAge is the default maximum age for cache files (30 days).
const DefaultMaxAge = 30 * 24 * time.Hour

// ParseDuration parses duration strings like "30d", "24h", "60m", "3600s".
// Supports: d (days), h (hours), m (minutes), s (seconds).
// If empty string, returns DefaultMaxAge.
func ParseDuration(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return DefaultMaxAge, nil
	}

	matches := durationRegex.FindStringSubmatch(s)
	if matches == nil {
		return 0, fmt.Errorf("invalid duration format: %q", s)
	}

	value, err := strconv.ParseInt(matches[1], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse duration value: %w", err)
	}

	unit := strings.ToLower(matches[2])
	var multiplier time.Duration

	switch unit {
	case "", "s":
		multiplier = time.Second
	case "m":
		multiplier = time.Minute
	case "h":
		multiplier = time.Hour
	case "d":
		multiplier = 24 * time.Hour
	default:
		return 0, fmt.Errorf("unknown duration unit: %q", unit)
	}

	return time.Duration(value) * multiplier, nil
}
