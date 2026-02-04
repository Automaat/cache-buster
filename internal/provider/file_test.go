package provider

import (
	"testing"

	"github.com/Automaat/cache-buster/internal/cache"
)

func TestFormatResultWithErrors(t *testing.T) {
	tests := []struct {
		name    string
		base    string
		want    string
		errors  []cache.AccessError
		deleted int64
	}{
		{
			name:    "no errors returns base",
			base:    "deleted 5 files",
			deleted: 5,
			errors:  nil,
			want:    "deleted 5 files",
		},
		{
			name:    "empty errors returns base",
			base:    "deleted 5 files",
			deleted: 5,
			errors:  []cache.AccessError{},
			want:    "deleted 5 files",
		},
		{
			name:    "permission denied only",
			base:    "deleted 5 files",
			deleted: 5,
			errors: []cache.AccessError{
				{Path: "/a", Reason: cache.ReasonPermissionDenied},
				{Path: "/b", Reason: cache.ReasonPermissionDenied},
			},
			want: "deleted 5 files (2 permission denied)",
		},
		{
			name:    "locked only",
			base:    "deleted 3 files",
			deleted: 3,
			errors: []cache.AccessError{
				{Path: "/a", Reason: cache.ReasonFileLocked},
			},
			want: "deleted 3 files (1 locked)",
		},
		{
			name:    "other errors only",
			base:    "deleted 2 files",
			deleted: 2,
			errors: []cache.AccessError{
				{Path: "/a", Reason: cache.ReasonUnknown},
				{Path: "/b", Reason: cache.ReasonNotFound},
			},
			want: "deleted 2 files (2 other errors)",
		},
		{
			name:    "mixed errors",
			base:    "deleted 10 files",
			deleted: 10,
			errors: []cache.AccessError{
				{Path: "/a", Reason: cache.ReasonPermissionDenied},
				{Path: "/b", Reason: cache.ReasonFileLocked},
				{Path: "/c", Reason: cache.ReasonUnknown},
			},
			want: "deleted 10 files (1 permission denied, 1 locked, 1 other errors)",
		},
		{
			name:    "empty base with errors",
			base:    "",
			deleted: 5,
			errors: []cache.AccessError{
				{Path: "/a", Reason: cache.ReasonPermissionDenied},
			},
			want: "deleted 5 files (1 permission denied)",
		},
		{
			name:    "preserves non-standard base",
			base:    "trimmed old files",
			deleted: 3,
			errors: []cache.AccessError{
				{Path: "/a", Reason: cache.ReasonPermissionDenied},
			},
			want: "trimmed old files (1 permission denied)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatResultWithErrors(tt.base, tt.deleted, tt.errors)
			if got != tt.want {
				t.Errorf("formatResultWithErrors() = %q, want %q", got, tt.want)
			}
		})
	}
}
