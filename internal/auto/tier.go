// Package auto implements unattended cache trimming driven by free disk space.
package auto

import (
	"fmt"
	"math"
	"os"
	"syscall"

	"github.com/Automaat/cache-buster/internal/config"
)

// CriticalFree is the free-space level under which the stale-directory
// sweeps run as well.
const CriticalFree int64 = 5 << 30

// Tier is the disk-pressure level: TierOK trims only providers over their
// limit, TierLow trims every enabled provider, TierCritical also sweeps
// stale directories.
type Tier int

// Pressure tiers, lowest to highest.
const (
	TierOK Tier = iota
	TierLow
	TierCritical
)

func (t Tier) String() string {
	switch t {
	case TierOK:
		return "ok"
	case TierLow:
		return "low"
	case TierCritical:
		return "critical"
	default:
		return "unknown"
	}
}

// FreeSpace is the free and total bytes of a volume.
type FreeSpace struct {
	Free  int64
	Total int64
}

// FreeFunc reports the current free space; tests inject a fixed value.
type FreeFunc func() (FreeSpace, error)

const dataVolume = "/System/Volumes/Data"

// DataVolumePath returns the path whose statfs reflects the user data volume.
// Non-macOS hosts fall back to the root.
func DataVolumePath() string {
	if info, err := os.Stat(dataVolume); err == nil && info.IsDir() {
		return dataVolume
	}
	return "/"
}

// StatfsFree returns a FreeFunc reading statfs of path. Free counts the space
// available to unprivileged users.
func StatfsFree(path string) FreeFunc {
	return func() (FreeSpace, error) {
		var st syscall.Statfs_t
		if err := syscall.Statfs(path, &st); err != nil {
			return FreeSpace{}, fmt.Errorf("statfs %s: %w", path, err)
		}
		bsize := clampInt64(st.Bsize)
		return FreeSpace{
			Free:  clampInt64(st.Bavail) * bsize,
			Total: clampInt64(st.Blocks) * bsize,
		}, nil
	}
}

type statfsInt interface {
	~int32 | ~int64 | ~uint32 | ~uint64
}

func clampInt64[T statfsInt](v T) int64 {
	if v <= 0 {
		return 0
	}
	if uint64(v) > math.MaxInt64 {
		return math.MaxInt64
	}
	return int64(v)
}

// BelowFloor reports whether free space is under min_free or the min_free_pct
// floor, ignoring the fixed critical level.
func BelowFloor(fs FreeSpace, cfg config.Auto) (bool, error) {
	minFree, err := cfg.MinFreeBytes()
	if err != nil {
		return false, err
	}
	if fs.Free < minFree {
		return true, nil
	}
	return cfg.MinFreePct > 0 && fs.Total > 0 && float64(fs.Free)*100 < cfg.MinFreePct*float64(fs.Total), nil
}

// ChooseTier maps free space to a pressure tier. Free space under either the
// absolute or the percentage floor is low; under CriticalFree it is critical.
func ChooseTier(fs FreeSpace, cfg config.Auto) (Tier, error) {
	below, err := BelowFloor(fs, cfg)
	if err != nil {
		return TierOK, err
	}

	switch {
	case fs.Free < CriticalFree:
		return TierCritical, nil
	case below:
		return TierLow, nil
	default:
		return TierOK, nil
	}
}
