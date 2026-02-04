package size

import (
	"testing"
)

func TestParseSize(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    int64
		wantErr bool
	}{
		// Basic units
		{"bytes", "100B", 100, false},
		{"bytes no unit", "100", 100, false},
		{"kilobytes K", "10K", 10 * 1024, false},
		{"kilobytes KB", "10KB", 10 * 1024, false},
		{"megabytes M", "5M", 5 * 1024 * 1024, false},
		{"megabytes MB", "5MB", 5 * 1024 * 1024, false},
		{"gigabytes G", "2G", 2 * 1024 * 1024 * 1024, false},
		{"gigabytes GB", "2GB", 2 * 1024 * 1024 * 1024, false},
		{"terabytes T", "1T", 1024 * 1024 * 1024 * 1024, false},
		{"terabytes TB", "1TB", 1024 * 1024 * 1024 * 1024, false},

		// Case insensitive
		{"lowercase kb", "10kb", 10 * 1024, false},
		{"lowercase mb", "5mb", 5 * 1024 * 1024, false},
		{"lowercase gb", "2gb", 2 * 1024 * 1024 * 1024, false},
		{"lowercase g", "2g", 2 * 1024 * 1024 * 1024, false},
		{"mixed case Gb", "2Gb", 2 * 1024 * 1024 * 1024, false},

		// Whitespace
		{"with spaces", "  10GB  ", 10 * 1024 * 1024 * 1024, false},
		{"space before unit", "10 GB", 10 * 1024 * 1024 * 1024, false},

		// Decimals
		{"decimal gigabytes", "1.5G", 1.5 * 1024 * 1024 * 1024, false},
		{"decimal megabytes", "2.5MB", 2.5 * 1024 * 1024, false},

		// Edge cases
		{"zero", "0", 0, false},
		{"zero bytes", "0B", 0, false},

		// Errors
		{"empty string", "", 0, true},
		{"invalid format", "abc", 0, true},
		{"negative", "-10GB", 0, true},
		{"invalid unit", "10XB", 0, true},
		{"no number", "GB", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseSize(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseSize(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("ParseSize(%q) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}

func TestFormatSize(t *testing.T) {
	tests := []struct {
		name  string
		want  string
		bytes int64
	}{
		{"zero", "0 B", 0},
		{"bytes", "100 B", 100},
		{"kilobytes", "1.0 KiB", 1024},
		{"megabytes", "5.0 MiB", 5 * 1024 * 1024},
		{"gigabytes", "2.0 GiB", 2 * 1024 * 1024 * 1024},
		{"terabytes", "1.0 TiB", 1024 * 1024 * 1024 * 1024},
		{"fractional", "1.5 GiB", 1536 * 1024 * 1024},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatSize(tt.bytes)
			if got != tt.want {
				t.Errorf("FormatSize(%d) = %q, want %q", tt.bytes, got, tt.want)
			}
		})
	}
}

func TestParseSizeFormatSizeRoundTrip(t *testing.T) {
	sizes := []int64{
		0,
		1024,
		5 * 1024 * 1024,
		2 * 1024 * 1024 * 1024,
	}

	for _, original := range sizes {
		formatted := FormatSize(original)
		parsed, err := ParseSize(formatted)
		if err != nil {
			t.Errorf("round-trip failed for %d: format=%q, parse error=%v", original, formatted, err)
			continue
		}
		if parsed != original {
			t.Errorf("round-trip mismatch: original=%d, formatted=%q, parsed=%d", original, formatted, parsed)
		}
	}
}
