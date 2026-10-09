package cli

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/smykla-skalski/bilgie/internal/auto"
	"github.com/smykla-skalski/bilgie/internal/osshim"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func fixedMemory(swapUsed uint64) auto.MemoryFunc {
	return func(context.Context) (osshim.Memory, error) {
		return osshim.Memory{
			SwapUsed: swapUsed * uint64(autoGiB), SwapTotal: 20 * uint64(autoGiB),
			MemTotal: 16 * uint64(autoGiB), MemAvailable: uint64(autoGiB), FreePercent: -1,
		}, nil
	}
}

func runawayProcesses() auto.ProcessesFunc {
	return func(context.Context) ([]osshim.Process, error) {
		procs := []osshim.Process{{PID: 1, PPID: 0, Args: "/sbin/init"}}
		for i := range 30 {
			ppid := 50
			if i < 10 {
				ppid = 1
			}
			procs = append(procs, osshim.Process{PID: 100 + i, PPID: ppid, Args: "node runaway.js"})
		}
		return procs, nil
	}
}

func TestTick_SwapIsRecordedAndNotifiedOncePerCooldown(t *testing.T) {
	r := newTickRig(t, "")
	var notes noteLog
	r.env.notify = notes.notifier(nil)
	r.env.memory = fixedMemory(14)

	r.tick(400*autoGiB, 0)
	assert.Equal(t, []string{"bilgie: swap is high"}, notes.titles)

	state, err := auto.ReadTickState(r.env.stateDir)
	require.NoError(t, err)
	assert.Equal(t, 14*autoGiB, state.SwapUsed)
	assert.False(t, state.SwapNotified.IsZero())

	r.tick(400*autoGiB, 2*time.Minute)
	r.tick(400*autoGiB, time.Hour)
	assert.Len(t, notes.titles, 1, "still high inside the cooldown")

	r.tick(400*autoGiB, 3*time.Hour+time.Minute)
	assert.Len(t, notes.titles, 2, "the cooldown ended")
}

func TestTick_NoSwapNotificationWhileSwapIsHealthy(t *testing.T) {
	r := newTickRig(t, "")
	var notes noteLog
	r.env.notify = notes.notifier(nil)
	r.env.memory = fixedMemory(2)

	r.tick(400*autoGiB, 0)

	assert.Empty(t, notes.titles)
	state, err := auto.ReadTickState(r.env.stateDir)
	require.NoError(t, err)
	assert.Equal(t, 2*autoGiB, state.SwapUsed)
}

func TestTick_SwapWarnZeroDisablesTheNotification(t *testing.T) {
	r := newTickRig(t, "auto:\n  swap_warn: 0\n")
	var notes noteLog
	r.env.notify = notes.notifier(nil)
	r.env.memory = fixedMemory(19)

	r.tick(400*autoGiB, 0)

	assert.Empty(t, notes.titles)
}

func TestTick_UnreadableSwapNeverStopsTheFreeSpaceCheck(t *testing.T) {
	r := newTickRig(t, "")
	var notes noteLog
	r.env.notify = notes.notifier(nil)
	r.env.memory = func(context.Context) (osshim.Memory, error) { return osshim.Memory{}, errors.New("no sysctl") }
	r.seedPass(-time.Hour, "ok")

	r.tick(5*autoGiB, 0)

	assert.Equal(t, 1, r.passes(), "the low-space pass still ran")
	assert.Equal(t, []string{"bilgie: disk space is low"}, notes.titles)
}

func TestTick_FailedSwapNotificationIsRetriedNextTick(t *testing.T) {
	r := newTickRig(t, "")
	var notes noteLog
	r.env.notify = notes.notifier(errors.New("no display"))
	r.env.memory = fixedMemory(14)

	r.tick(400*autoGiB, 0)
	r.tick(400*autoGiB, 2*time.Minute)

	assert.Len(t, notes.titles, 2)
	state, err := auto.ReadTickState(r.env.stateDir)
	require.NoError(t, err)
	assert.True(t, state.SwapNotified.IsZero())
}

func TestTick_DryRunReadsSwapButNeverNotifiesOrWrites(t *testing.T) {
	r := newTickRig(t, "")
	var notes noteLog
	r.env.notify = notes.notifier(nil)
	r.env.memory = fixedMemory(14)

	require.NoError(t, runTickWithLoader(t.Context(), r.loader, r.env, true))

	assert.Empty(t, notes.titles)
	state, err := auto.ReadTickState(r.env.stateDir)
	require.NoError(t, err)
	assert.True(t, state.Time.IsZero())
}

func TestAuto_RunRecordKeepsSwapUsed(t *testing.T) {
	f := newAutoFixture(t, 40*autoGiB, "")
	f.env.memory = fixedMemory(7)

	require.NoError(t, runAutoWithLoader(t.Context(), f.loader, f.env, false))

	runs := readRecords(t, f)
	require.Len(t, runs, 1)
	assert.Equal(t, int32(7<<10), runs[0].SwapUsedMiB)
}

func TestAuto_RunRecordOmitsSwapWhenUnread(t *testing.T) {
	f := newAutoFixture(t, 40*autoGiB, "")

	require.NoError(t, runAutoWithLoader(t.Context(), f.loader, f.env, false))

	runs := readRecords(t, f)
	require.Len(t, runs, 1)
	assert.Zero(t, runs[0].SwapUsedMiB)
}

func TestHistory_ShowsSwapTrend(t *testing.T) {
	f := newAutoFixture(t, 40*autoGiB, "")
	f.env.memory = fixedMemory(7)
	require.NoError(t, runAutoWithLoader(t.Context(), f.loader, f.env, false))
	f.env.memory = nil
	require.NoError(t, runAutoWithLoader(t.Context(), f.loader, f.env, false))

	var out bytes.Buffer
	require.NoError(t, runHistoryIn(&out, f.env.stateDir, 10, false))

	assert.Contains(t, out.String(), "SWAP")
	assert.Contains(t, out.String(), "7.0 GiB")
	assert.Regexp(t, `\s-\s`, out.String())
}

func TestDoctor_WarnsWhenSwapIsHighAndNamesProcessFamilies(t *testing.T) {
	d := scheduled("darwin")
	f := doctorFixture(t, d, 100*autoGiB, recentRun())
	f.env.memory = fixedMemory(14)
	f.env.processes = runawayProcesses()

	err := runDoctorWithLoader(t.Context(), f.loader, f.env)

	require.Error(t, err)
	out := f.out.String()
	assert.Contains(t, out, "[warn] memory: swap 14 GiB of 20 GiB used")
	assert.Contains(t, out, "(warning above 8.0 GiB)")
	assert.Contains(t, out, "30 x node runaway.js (10 orphaned)")
	assert.Contains(t, out, "what to do: quit or restart the largest memory users")
}

func TestDoctor_SwapBelowThresholdIsOKAndSkipsTheProcessTable(t *testing.T) {
	d := scheduled("linux")
	f := doctorFixture(t, d, 100*autoGiB, recentRun())
	f.env.memory = fixedMemory(1)
	f.env.processes = func(context.Context) ([]osshim.Process, error) {
		t.Error("process table read while swap is healthy")
		return nil, nil
	}

	require.NoError(t, runDoctorWithLoader(t.Context(), f.loader, f.env), f.out.String())

	assert.Contains(t, f.out.String(), "[ok  ] memory: swap 1.0 GiB of 20 GiB used, 1.0 GiB of 16 GiB RAM available")
}

func TestDoctor_HighSwapWithUnlistableProcessesStillWarns(t *testing.T) {
	d := scheduled("windows")
	f := doctorFixture(t, d, 100*autoGiB, recentRun())
	f.env.memory = fixedMemory(14)
	f.env.processes = func(context.Context) ([]osshim.Process, error) { return nil, errors.New("access denied") }

	require.Error(t, runDoctorWithLoader(t.Context(), f.loader, f.env))

	assert.Contains(t, f.out.String(), "cannot list processes: access denied")
}

func TestDoctor_UnreadableMemoryIsOnlyANote(t *testing.T) {
	d := scheduled("linux")
	f := doctorFixture(t, d, 100*autoGiB, recentRun())
	f.env.memory = func(context.Context) (osshim.Memory, error) { return osshim.Memory{}, errors.New("no /proc") }

	require.NoError(t, runDoctorWithLoader(t.Context(), f.loader, f.env), f.out.String())

	assert.Contains(t, f.out.String(), "[note] memory: cannot read swap and memory: no /proc")
}
