package size

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/dustin/go-humanize"
)

var sizeRegex = regexp.MustCompile(`(?i)^(\d+(?:\.\d+)?)\s*([KMGT]?I?B?)$`)

// ParseSize parses human-readable size string to bytes.
// Supports: B, K/KB, M/MB, G/GB, T/TB (case insensitive).
func ParseSize(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("empty size string")
	}

	matches := sizeRegex.FindStringSubmatch(s)
	if matches == nil {
		return 0, fmt.Errorf("invalid size format: %q", s)
	}

	value, err := strconv.ParseFloat(matches[1], 64)
	if err != nil {
		return 0, fmt.Errorf("parse size value: %w", err)
	}

	unit := strings.ToUpper(matches[2])
	var multiplier float64

	switch unit {
	case "", "B":
		multiplier = 1
	case "K", "KB", "KIB":
		multiplier = 1024
	case "M", "MB", "MIB":
		multiplier = 1024 * 1024
	case "G", "GB", "GIB":
		multiplier = 1024 * 1024 * 1024
	case "T", "TB", "TIB":
		multiplier = 1024 * 1024 * 1024 * 1024
	default:
		return 0, fmt.Errorf("unknown size unit: %q", unit)
	}

	return int64(value * multiplier), nil
}

// FormatSize formats bytes as human-readable string.
func FormatSize(bytes int64) string {
	if bytes < 0 {
		return "0 B"
	}
	return humanize.IBytes(uint64(bytes))
}
