// Package auto implements unattended cache trimming driven by free disk space.
package auto

import (
	"fmt"
	"math"
	"os"
	"runtime"

	"github.com/Automaat/cache-buster/internal/config"
	"github.com/Automaat/cache-buster/internal/osshim"
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
// Other hosts fall back to the root, or the system drive on Windows.
func DataVolumePath() string {
	if runtime.GOOS == "windows" {
		return systemDrive()
	}
	if info, err := os.Stat(dataVolume); err == nil && info.IsDir() {
		return dataVolume
	}
	return "/"
}

func systemDrive() string {
	if drive := os.Getenv("SystemDrive"); drive != "" {
		return drive + `\`
	}
	return `C:\`
}

// StatfsFree returns a FreeFunc reading the filesystem space of path. Free
// counts the space available to unprivileged users.
func StatfsFree(path string) FreeFunc {
	return func() (FreeSpace, error) {
		space, err := osshim.QueryDiskSpace(path)
		if err != nil {
			return FreeSpace{}, fmt.Errorf("statfs %s: %w", path, err)
		}
		return FreeSpace{Free: clampInt64(space.Free), Total: clampInt64(space.Total)}, nil
	}
}

func clampInt64(v uint64) int64 {
	if v > math.MaxInt64 {
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
