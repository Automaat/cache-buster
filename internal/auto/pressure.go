package auto

import (
	"cmp"
	"context"
	"fmt"
	"math"
	"slices"
	"time"

	"github.com/smykla-skalski/bilgie/internal/config"
	"github.com/smykla-skalski/bilgie/internal/osshim"
	"github.com/smykla-skalski/bilgie/pkg/size"
)

// MemoryFunc reads memory and swap; tests inject a fixed reading.
type MemoryFunc func(ctx context.Context) (osshim.Memory, error)

// ProcessesFunc lists running processes; tests inject a fixed table.
type ProcessesFunc func(ctx context.Context) ([]osshim.Process, error)

// Process-family reporting thresholds: a family is many processes with one
// command line, or several of them orphaned to PID 1.
const (
	familyMinCount   = 4
	familyMinOrphans = 3
	familyListed     = 3
	familyCmdWidth   = 60
	initPID          = 1
)

// SwapHigh reports whether swap in use is above the warn threshold. A zero
// threshold disables the check.
func SwapHigh(m osshim.Memory, warn int64) bool {
	return warn > 0 && m.SwapUsed > uint64(warn)
}

// ClampBytes converts an unsigned byte count to the signed one sizes use.
func ClampBytes(n uint64) int64 {
	return int64(min(n, uint64(1)<<62))
}

// SwapUsedBytes is the swap in use in bytes.
func SwapUsedBytes(m osshim.Memory) int64 {
	return ClampBytes(m.SwapUsed)
}

// SwapUsedMiB is the swap in use in whole MiB, the unit the run log keeps.
func SwapUsedMiB(m osshim.Memory) int32 {
	return int32(min(m.SwapUsed>>20, math.MaxInt32))
}

// SwapNotifyAllowed reports whether a swap notification may go out: none was
// sent yet, the cooldown since the last one passed, or the clock moved back.
func SwapNotifyAllowed(last, now time.Time, cooldown time.Duration) bool {
	return last.IsZero() || last.After(now) || now.Sub(last) >= cooldown
}

// NotifySwap sends one notification when swap is above the warn threshold
// and the cooldown since the previous one passed. It reports whether one
// went out; the caller records the time.
func NotifySwap(
	ctx context.Context, notify Notifier, m osshim.Memory, limits config.Limits, last, now time.Time,
) (bool, error) {
	if notify == nil || !SwapHigh(m, limits.SwapWarn) || !SwapNotifyAllowed(last, now, limits.NotifyCooldown) {
		return false, nil
	}
	msg := fmt.Sprintf("%s of swap in use (warning above %s). Run %s doctor to see the largest process families.",
		size.FormatSize(SwapUsedBytes(m)), size.FormatSize(limits.SwapWarn), agentName)
	if err := notify(ctx, agentName+": swap is high", msg); err != nil {
		return false, fmt.Errorf("send swap notification: %w", err)
	}
	return true, nil
}

// Family is a group of processes that share one command line. Orphans counts
// the members whose parent is PID 1.
type Family struct {
	Command string
	Count   int
	Orphans int
}

// ProcessFamilies returns the largest runaway process families: command
// lines repeated by many processes, or left behind by several orphans. A
// process with no command line (a kernel thread) is ignored.
func ProcessFamilies(procs []osshim.Process) []Family {
	byCmd := make(map[string]*Family)
	for i := range procs {
		p := &procs[i]
		if p.Args == "" {
			continue
		}
		f := byCmd[p.Args]
		if f == nil {
			f = &Family{Command: p.Args}
			byCmd[p.Args] = f
		}
		f.Count++
		if p.PPID == initPID && p.PID != initPID {
			f.Orphans++
		}
	}
	var out []Family
	for _, f := range byCmd {
		if f.Count >= familyMinCount || f.Orphans >= familyMinOrphans {
			out = append(out, *f)
		}
	}
	slices.SortFunc(out, func(a, b Family) int {
		return cmp.Or(cmp.Compare(b.Count, a.Count), cmp.Compare(b.Orphans, a.Orphans), cmp.Compare(a.Command, b.Command))
	})
	if len(out) > familyListed {
		out = out[:familyListed]
	}
	for i := range out {
		out[i].Command = shorten(out[i].Command, familyCmdWidth)
	}
	return out
}

func shorten(s string, width int) string {
	r := []rune(s)
	if len(r) <= width {
		return s
	}
	return string(r[:width-1]) + "…"
}

// String renders a family as "112 x <command> (40 with parent PID 1)".
func (f Family) String() string {
	s := fmt.Sprintf("%d x %s", f.Count, f.Command)
	if f.Orphans > 0 {
		s += fmt.Sprintf(" (%d with parent PID 1)", f.Orphans)
	}
	return s
}
