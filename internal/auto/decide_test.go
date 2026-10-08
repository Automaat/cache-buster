package auto

import (
	"testing"
	"time"

	"github.com/smykla-skalski/bilgie/internal/config"
	"github.com/smykla-skalski/bilgie/internal/provider"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var t0 = time.Date(2026, 1, 2, 12, 0, 0, 0, time.UTC)

func defaultLimits(t *testing.T) config.Limits {
	t.Helper()
	l, err := config.DefaultAuto().Limits()
	require.NoError(t, err)
	return l
}

func disk(free int64) FreeSpace { return FreeSpace{Free: free, Total: 1000 * gib} }

func TestThresholdsFor(t *testing.T) {
	l := defaultLimits(t)
	tests := []struct {
		name  string
		total int64
		want  Thresholds
	}{
		{"big disk: 5 percent is 46GB", 926 * gib, Thresholds{Low: 926 * gib / 20, Critical: 10 * gib, Emergency: 5 * gib, Hysteresis: 2 * gib}},
		{"small disk: absolute floor wins", 200 * gib, Thresholds{Low: 30 * gib, Critical: 10 * gib, Emergency: 5 * gib, Hysteresis: 2 * gib}},
		{"huge disk: cap wins", 4000 * gib, Thresholds{Low: 100 * gib, Critical: 10 * gib, Emergency: 5 * gib, Hysteresis: 2 * gib}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ThresholdsFor(tt.total, l)
			assert.InDelta(t, tt.want.Low, got.Low, 1)
			assert.Equal(t, tt.want.Critical, got.Critical)
			assert.Equal(t, tt.want.Emergency, got.Emergency)
		})
	}
}

func TestThresholdsFor_ExplicitFifteenPercentKeepsItsValueUpToTheCap(t *testing.T) {
	l := defaultLimits(t)
	l.MinFreePct = 15
	assert.Equal(t, 100*gib, ThresholdsFor(926*gib, l).Low)
	assert.Equal(t, 60*gib, ThresholdsFor(400*gib, l).Low)
}

func TestThresholdsFor_LowerLevelsNeverExceedTheOneAbove(t *testing.T) {
	l := defaultLimits(t)
	l.MinFree, l.MinFreePct = 8*gib, 0
	th := ThresholdsFor(500*gib, l)
	assert.Equal(t, Thresholds{Low: 8 * gib, Critical: 8 * gib, Emergency: 5 * gib, Hysteresis: 2 * gib}, th)
}

func TestSettle_HysteresisPreventsFlapping(t *testing.T) {
	th := ThresholdsFor(1000*gib, defaultLimits(t))
	low := th.Low
	tests := []struct {
		name string
		prev Tier
		free int64
		want Tier
	}{
		{"healthy stays healthy at the edge", TierOK, low, TierOK},
		{"crossing below enters low at once", TierOK, low - 1, TierLow},
		{"low stays low just above the threshold", TierLow, low + gib, TierLow},
		{"low eases only past the band", TierLow, low + 2*gib, TierOK},
		{"critical stays inside its band", TierCritical, 11 * gib, TierCritical},
		{"critical eases to low past the band", TierCritical, 12 * gib, TierLow},
		{"emergency stays inside its band", TierEmergency, 6 * gib, TierEmergency},
		{"emergency eases to critical past the band", TierEmergency, 7 * gib, TierCritical},
		{"worsening skips levels", TierOK, gib, TierEmergency},
		{"a large recovery eases through every band", TierEmergency, 500 * gib, TierOK},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, th.Settle(tt.prev, tt.free))
		})
	}
}

func decide(t *testing.T, free int64, mutate func(*TickInput)) Decision {
	t.Helper()
	in := TickInput{Now: t0, Free: disk(free), Limits: defaultLimits(t)}
	in.Pass = PassState{Time: t0.Add(-time.Minute), Tier: "ok"}
	if mutate != nil {
		mutate(&in)
	}
	return Decide(in)
}

func TestDecide_Transitions(t *testing.T) {
	tests := []struct {
		name     string
		free     int64
		mutate   func(*TickInput)
		wantRun  bool
		wantTier Tier
	}{
		{"healthy with a recent pass does nothing", 400 * gib, nil, false, TierOK},
		{"healthy routine pass due", 400 * gib, func(in *TickInput) { in.Pass.Time = t0.Add(-31 * time.Minute) }, true, TierOK},
		{"healthy routine pass not yet due", 400 * gib, func(in *TickInput) { in.Pass.Time = t0.Add(-29 * time.Minute) }, false, TierOK},
		{"never ran: first tick passes", 400 * gib, func(in *TickInput) { in.Pass = PassState{} }, true, TierOK},
		{"low inside the 10 minute cooldown waits", 40 * gib, func(in *TickInput) { in.Pass = PassState{Time: t0.Add(-9 * time.Minute), Tier: "low"} }, false, TierLow},
		{"low after the cooldown passes", 40 * gib, func(in *TickInput) { in.Pass = PassState{Time: t0.Add(-10 * time.Minute), Tier: "low"} }, true, TierLow},
		{"critical uses the 2 minute cooldown", 8 * gib, func(in *TickInput) {
			in.Pass = PassState{Time: t0.Add(-2 * time.Minute), Tier: "critical"}
		}, true, TierCritical},
		{"critical inside its cooldown waits", 8 * gib, func(in *TickInput) {
			in.Pass = PassState{Time: t0.Add(-time.Minute), Tier: "critical"}
		}, false, TierCritical},
		{"escalation from low to critical bypasses the low cooldown", 8 * gib, func(in *TickInput) {
			in.Pass = PassState{Time: t0.Add(-time.Minute), Tier: "low"}
		}, true, TierCritical},
		{"escalation never beats the minimum gap", 8 * gib, func(in *TickInput) {
			in.Pass = PassState{Time: t0.Add(-10 * time.Second), Tier: "low"}
		}, false, TierCritical},
		{"emergency escalates at once", 3 * gib, func(in *TickInput) {
			in.Pass = PassState{Time: t0.Add(-time.Minute), Tier: "critical"}
		}, true, TierEmergency},
		{"hysteresis band just above the threshold does not start a pass", 51 * gib, func(in *TickInput) {
			in.Tick = TickState{Time: t0.Add(-time.Minute), Tier: "low"}
			in.Pass = PassState{Time: t0.Add(-time.Hour), Tier: "low"}
			in.Pass.Time = t0.Add(-20 * time.Minute)
		}, false, TierLow},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := decide(t, tt.free, tt.mutate)
			assert.Equal(t, tt.wantRun, d.Run, d.Reason)
			assert.Equal(t, tt.wantTier, d.Tier)
			assert.False(t, d.Predicted)
		})
	}
}

func TestDecide_TierMemoryExpires(t *testing.T) {
	d := decide(t, 51*gib, func(in *TickInput) {
		in.Tick = TickState{Time: t0.Add(-3 * time.Hour), Tier: "low"}
	})
	assert.Equal(t, TierOK, d.Tier)
}

func fallingSamples(start, step int64, n int) []Sample {
	out := make([]Sample, 0, n)
	for i := range n {
		out = append(out, Sample{Time: t0.Add(time.Duration(i-n) * 2 * time.Minute), Free: start - step*int64(i)})
	}
	return out
}

func TestDecide_SlopeTriggersOnAFallingSeries(t *testing.T) {
	d := decide(t, 60*gib, func(in *TickInput) {
		in.Free = disk(54 * gib)
		in.Tick = TickState{Time: t0.Add(-2 * time.Minute), Samples: fallingSamples(66*gib, gib, 6)}
		in.Pass = PassState{Time: t0.Add(-time.Hour), Tier: "ok"}
	})
	assert.True(t, d.Run, d.Reason)
	assert.True(t, d.Predicted)
	assert.Equal(t, TierOK, d.Tier)
}

func TestDecide_SlopeIgnoresASingleOutlier(t *testing.T) {
	steady := make([]Sample, 0, 6)
	for i := range 6 {
		free := 60 * gib
		if i >= 3 {
			free = 52 * gib
		}
		steady = append(steady, Sample{Time: t0.Add(time.Duration(i-6) * 2 * time.Minute), Free: free})
	}
	d := decide(t, 0, func(in *TickInput) {
		in.Free = disk(52 * gib)
		in.Tick = TickState{Time: t0.Add(-2 * time.Minute), Samples: steady}
		in.Pass = PassState{Time: t0.Add(-time.Hour), Tier: "ok"}
	})
	assert.False(t, d.Predicted, d.Reason)
}

func TestDecide_SlopeNeedsEnoughSamples(t *testing.T) {
	d := decide(t, 60*gib, func(in *TickInput) {
		in.Free = disk(54 * gib)
		in.Tick = TickState{Time: t0.Add(-2 * time.Minute), Samples: fallingSamples(60*gib, gib, 3)}
		in.Pass = PassState{Time: t0.Add(-time.Hour), Tier: "ok"}
	})
	assert.False(t, d.Predicted)
}

func TestDecide_SlopeNeedsASteepEnoughFall(t *testing.T) {
	d := decide(t, 60*gib, func(in *TickInput) {
		in.Free = disk(51*gib + 10<<20)
		in.Tick = TickState{Time: t0.Add(-2 * time.Minute), Samples: fallingSamples(51*gib+60<<20, 10<<20, 6)}
		in.Pass = PassState{Time: t0.Add(-time.Hour), Tier: "ok"}
	})
	assert.False(t, d.Predicted)
}

func TestDecide_SlopeProjectingBeyondTheLowThresholdDoesNotFire(t *testing.T) {
	d := decide(t, 400*gib, func(in *TickInput) {
		in.Free = disk(400*gib - 6*gib)
		in.Tick = TickState{Time: t0.Add(-2 * time.Minute), Samples: fallingSamples(400*gib, gib, 6)}
		in.Pass = PassState{Time: t0.Add(-time.Hour), Tier: "ok"}
	})
	assert.False(t, d.Predicted)
}

func TestDecide_ForecastRespectsCooldownAndCanBeDisabled(t *testing.T) {
	falling := func(in *TickInput) {
		in.Free = disk(54 * gib)
		in.Tick = TickState{Time: t0.Add(-2 * time.Minute), Samples: fallingSamples(66*gib, gib, 6)}
	}
	recent := decide(t, 0, func(in *TickInput) {
		falling(in)
		in.Pass = PassState{Time: t0.Add(-5 * time.Minute), Tier: "ok"}
	})
	assert.False(t, recent.Run)

	off := decide(t, 0, func(in *TickInput) {
		falling(in)
		in.Limits.Forecast = 0
		in.Pass = PassState{Time: t0.Add(-time.Hour), Tier: "ok"}
	})
	assert.False(t, off.Predicted)
}

func TestAddSample_DropsOldReadingsAndRestartsAfterAGap(t *testing.T) {
	tick := 2 * time.Minute
	old := []Sample{{Time: t0.Add(-40 * time.Minute), Free: 1}, {Time: t0.Add(-2 * time.Minute), Free: 2}}
	got := addSample(old, Sample{Time: t0, Free: 3}, tick)
	assert.Equal(t, []int64{2, 3}, []int64{got[0].Free, got[1].Free})

	gap := []Sample{{Time: t0.Add(-25 * time.Minute), Free: 1}, {Time: t0.Add(-20 * time.Minute), Free: 2}}
	got = addSample(gap, Sample{Time: t0, Free: 3}, tick)
	assert.Len(t, got, 1, "a sleep gap restarts the window")

	many := fallingSamples(100*gib, gib, 10)
	got = addSample(many, Sample{Time: t0, Free: 1}, tick)
	assert.Len(t, got, maxSamples)
}

func TestNotifyAllowed(t *testing.T) {
	cooldown := 3 * time.Hour
	var p PassState
	assert.True(t, p.NotifyAllowed(TierLow, 40*gib, t0, cooldown), "first notification")
	p.NoteNotified(TierLow, 40*gib, t0)

	assert.False(t, p.NotifyAllowed(TierLow, 39*gib, t0.Add(30*time.Minute), cooldown), "same tier inside the window")
	assert.False(t, p.NotifyAllowed(TierLow, 30*gib, t0.Add(time.Hour), cooldown), "a 10GB drop is not more than 10GB")
	assert.True(t, p.NotifyAllowed(TierLow, 29*gib, t0.Add(time.Hour), cooldown), "dropped by more than 10GB")
	assert.True(t, p.NotifyAllowed(TierCritical, 39*gib, t0.Add(time.Minute), cooldown), "a lower tier is new")
	assert.True(t, p.NotifyAllowed(TierLow, 40*gib, t0.Add(3*time.Hour), cooldown), "window over")
	assert.True(t, p.NotifyAllowed(TierLow, 40*gib, t0.Add(-time.Hour), cooldown), "a clock that went back never blocks")
}

func TestNotifyLimited_SendsOnceThenSuppressesThenAllowsWorsening(t *testing.T) {
	var sent []sentNote
	cfg := autoCfg()
	var pass PassState
	report := func(free int64) Report { return Report{End: FreeSpace{Free: free, Total: 1000 * gib}} }

	send := func(free int64, at time.Duration) bool {
		ok, err := NotifyLimited(t.Context(), noteRecorder(&sent, nil), report(free), cfg, &pass, t0.Add(at))
		require.NoError(t, err)
		return ok
	}

	assert.True(t, send(100*gib, 0))
	assert.False(t, send(99*gib, 30*time.Minute))
	assert.False(t, send(95*gib, 2*time.Hour))
	assert.True(t, send(80*gib, 2*time.Hour), "more than 10GB lower")
	assert.True(t, send(8*gib, 2*time.Hour+time.Minute), "critical is a new tier")
	assert.True(t, send(100*gib, 6*time.Hour), "the 3 hour window ended")
	assert.False(t, send(400*gib, 7*time.Hour), "recovered: nothing to say")
	assert.Len(t, sent, 4)
}

func (h *harness) runDeps(mutate func(*Deps), free int64) (Report, error) {
	h.t.Helper()
	deps := Deps{
		Free: func() (FreeSpace, error) { return FreeSpace{Free: free, Total: 1000 * gib}, nil },
		NewProvider: func(name string, _ config.Provider) (provider.Provider, error) {
			return h.fakes[name], nil
		},
		Out:  &h.out,
		Home: h.home,
	}
	mutate(&deps)
	return Run(h.t.Context(), h.cfg, false, deps)
}

func TestRun_PredictedPassTrimsEveryEnabledProviderAboveTheThreshold(t *testing.T) {
	h := newHarness(t)
	h.add("npm", true, false)
	h.add("go-build", true, false)

	report, err := h.runDeps(func(d *Deps) { d.Predicted = true }, 400*gib)

	require.NoError(t, err)
	assert.Equal(t, TierLow, report.Tier)
	assert.Equal(t, []string{"npm", "go-build"}, h.calls)
}

func TestRun_WithoutPredictionAHealthyDiskOnlyTrimsOverLimitProviders(t *testing.T) {
	h := newHarness(t)
	h.add("npm", true, false)

	report, err := h.runDeps(func(*Deps) {}, 400*gib)

	require.NoError(t, err)
	assert.Equal(t, TierOK, report.Tier)
	assert.Empty(t, h.calls)
}

func TestRun_MinTierRaisesThePassAndKeepsItInsideTheBand(t *testing.T) {
	h := newHarness(t)
	h.add("npm", true, false)
	h.add("sail-dirs", false, false, func(_ *fakeProvider, pc *config.Provider) { pc.Type = config.TypeDirPattern })

	report, err := h.runDeps(func(d *Deps) { d.MinTier = TierEmergency }, 6*gib)

	require.NoError(t, err)
	assert.Equal(t, TierEmergency, report.Tier)
	assert.Equal(t, []string{"sail-dirs", "npm"}, h.calls, "6GB is inside the emergency band, so the sweep still runs")
}

func TestRun_StopsOnlyAfterFreeSpaceClearsTheHysteresisBand(t *testing.T) {
	h := newHarness(t)
	h.add("npm", true, false)
	h.add("homebrew", true, false)

	i := 0
	reads := []int64{40 * gib, 151 * gib, 153 * gib, 153 * gib}
	_, err := h.runDeps(func(d *Deps) {
		d.Free = func() (FreeSpace, error) {
			v := reads[min(i, len(reads)-1)]
			i++
			return FreeSpace{Free: v, Total: 1000 * gib}, nil
		}
	}, 0)

	require.NoError(t, err)
	assert.Equal(t, []string{"npm"}, h.calls, "151GB is inside the band above the 150GB threshold; 153GB clears it")
}

func TestDecide_FuturePassTimeCountsAsNoPass(t *testing.T) {
	d := decide(t, 3*gib, func(in *TickInput) { in.Pass = PassState{Time: t0.Add(48 * time.Hour), Tier: "ok"} })
	assert.True(t, d.Run, d.Reason)
}

func TestAddSample_KeepsEnoughReadingsForSlowTickIntervals(t *testing.T) {
	tick := 10 * time.Minute
	var samples []Sample
	for i := range 6 {
		samples = addSample(samples, Sample{Time: t0.Add(time.Duration(i) * tick), Free: int64(100-i) * gib}, tick)
	}
	assert.Len(t, samples, 6)
}

func TestDecide_RoutinePassStillRunsInsideTheHysteresisBand(t *testing.T) {
	d := decide(t, 51*gib, func(in *TickInput) {
		in.Tick = TickState{Time: t0.Add(-time.Minute), Tier: "low"}
		in.Pass = PassState{Time: t0.Add(-31 * time.Minute), Tier: "low"}
	})
	assert.True(t, d.Run, d.Reason)
	assert.Equal(t, "routine pass is due", d.Reason)
}
