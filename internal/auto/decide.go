package auto

import (
	"fmt"
	"slices"
	"time"

	"github.com/smykla-skalski/bilgie/internal/config"
	"github.com/smykla-skalski/bilgie/pkg/size"
)

// Forecast tuning: enough readings to tell a trend from a blip, and a slope
// big enough to matter.
const (
	maxSamples       = 10
	minSamples       = 5
	maxSampleAge     = 30 * time.Minute
	minSlopePerMin   = 50 << 20
	sampleGapFloor   = 10 * time.Minute
	escalationGap    = 30 * time.Second
	staleTierMemory  = time.Hour
	gapTickMultiples = 5
	ageTickMultiples = 6
)

// Tick actions recorded in the tick state.
const (
	ActionIdle = "idle"
	ActionPass = "pass"
	ActionSkip = "skip"
)

// TickInput is everything Decide looks at.
type TickInput struct {
	Now    time.Time
	Free   FreeSpace
	Limits config.Limits
	Tick   TickState
	Pass   PassState
}

// Decision is the outcome of one tick. Samples is the pruned history
// including this reading; Run asks for a full pass; Predicted marks a pass
// started by the forecast rather than by the tier.
type Decision struct {
	Reason    string
	Samples   []Sample
	Tier      Tier
	Run       bool
	Predicted bool
}

// Decide chooses what a tick does from one free-space reading and the stored
// state. It is pure: no I/O, no scans.
func Decide(in TickInput) Decision {
	th := ThresholdsFor(in.Free.Total, in.Limits)
	prev := TierOK
	if !in.Tick.Time.IsZero() && in.Now.Sub(in.Tick.Time) < staleTierMemory {
		prev = ParseTier(in.Tick.Tier)
	}
	tier := th.Settle(prev, in.Free.Free)
	samples := addSample(in.Tick.Samples, Sample{Time: in.Now, Free: in.Free.Free}, in.Limits.TickInterval)
	d := Decision{Tier: tier, Samples: samples}

	sinceLast := time.Duration(1<<62 - 1)
	lastTier := TierOK
	if !in.Pass.Time.IsZero() && !in.Pass.Time.After(in.Now) {
		sinceLast = in.Now.Sub(in.Pass.Time)
		lastTier = ParseTier(in.Pass.Tier)
	}

	switch {
	case tier >= TierLow && th.Raw(in.Free.Free) != TierOK:
		cooldown := in.Limits.LowCooldown
		if tier >= TierCritical {
			cooldown = in.Limits.CriticalCooldown
		}
		switch {
		case sinceLast >= cooldown:
			d.Run = true
			d.Reason = fmt.Sprintf("free space %s is %s", size.FormatSize(in.Free.Free), tier)
		case tier > lastTier && sinceLast >= escalationGap:
			d.Run = true
			d.Reason = fmt.Sprintf("free space %s escalated to %s", size.FormatSize(in.Free.Free), tier)
		default:
			d.Reason = fmt.Sprintf("%s: next pass in %s", tier, (cooldown - sinceLast).Round(time.Second))
		}
	case forecastCrosses(samples, in.Limits.Forecast, th.Low) && sinceLast >= in.Limits.LowCooldown:
		d.Run, d.Predicted = true, true
		d.Reason = "free space is falling: the low threshold is projected within " + in.Limits.Forecast.String()
	case sinceLast >= in.Limits.Interval:
		d.Run = true
		d.Reason = "routine pass is due"
	case tier >= TierLow:
		d.Reason = fmt.Sprintf("%s easing: free space is above the threshold but inside the hysteresis band", tier)
	default:
		d.Reason = "healthy"
	}
	return d
}

// addSample appends s and drops readings that are too old, too many, or from
// before a long gap (sleep, agent down): a trend across a gap means nothing.
func addSample(old []Sample, s Sample, tick time.Duration) []Sample {
	gap := max(sampleGapFloor, gapTickMultiples*tick)
	maxAge := max(maxSampleAge, ageTickMultiples*tick)
	out := make([]Sample, 0, len(old)+1)
	for _, o := range old {
		if s.Time.Sub(o.Time) <= maxAge && o.Time.Before(s.Time) {
			out = append(out, o)
		}
	}
	if n := len(out); n > 0 && s.Time.Sub(out[n-1].Time) > gap {
		out = out[:0]
	}
	out = append(out, s)
	if len(out) > maxSamples {
		out = out[len(out)-maxSamples:]
	}
	return out
}

// forecastCrosses reports whether the median rate of change projects free
// space under low within horizon. The median of the per-interval rates keeps
// one sudden drop (a big file written once) from looking like a trend.
func forecastCrosses(samples []Sample, horizon time.Duration, low int64) bool {
	if horizon <= 0 || len(samples) < minSamples {
		return false
	}
	rates := make([]float64, 0, len(samples)-1)
	for i := 1; i < len(samples); i++ {
		dt := samples[i].Time.Sub(samples[i-1].Time).Seconds()
		if dt <= 0 {
			continue
		}
		rates = append(rates, float64(samples[i].Free-samples[i-1].Free)/dt)
	}
	if len(rates) < minSamples-1 {
		return false
	}
	rate := median(rates)
	if rate > -float64(minSlopePerMin)/60 {
		return false
	}
	projected := float64(samples[len(samples)-1].Free) + rate*horizon.Seconds()
	return projected < float64(low)
}

func median(values []float64) float64 {
	sorted := slices.Clone(values)
	slices.Sort(sorted)
	n := len(sorted)
	if n%2 == 1 {
		return sorted[n/2]
	}
	return (sorted[n/2-1] + sorted[n/2]) / 2
}
