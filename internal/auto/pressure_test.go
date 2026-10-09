package auto

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/smykla-skalski/bilgie/internal/config"
	"github.com/smykla-skalski/bilgie/internal/osshim"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const swapGiB = uint64(1) << 30

func limitsWithSwap(t *testing.T, warn string) config.Limits {
	t.Helper()
	a := config.DefaultAuto()
	a.SwapWarn = warn
	l, err := a.Limits()
	require.NoError(t, err)
	return l
}

func TestSwapHigh(t *testing.T) {
	m := osshim.Memory{SwapUsed: 9 * swapGiB}
	assert.True(t, SwapHigh(m, int64(8*swapGiB)))
	assert.False(t, SwapHigh(m, int64(9*swapGiB)))
	assert.False(t, SwapHigh(m, 0))
}

func TestNotifySwap(t *testing.T) {
	now := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	high := osshim.Memory{SwapUsed: 12 * swapGiB}
	healthy := osshim.Memory{SwapUsed: 1 * swapGiB}
	tests := []struct {
		name string
		mem  osshim.Memory
		warn string
		last time.Time
		want bool
	}{
		{"high and never notified", high, "8G", time.Time{}, true},
		{"high within cooldown", high, "8G", now.Add(-time.Hour), false},
		{"high after cooldown", high, "8G", now.Add(-4 * time.Hour), true},
		{"clock moved back", high, "8G", now.Add(time.Hour), true},
		{"healthy", healthy, "8G", time.Time{}, false},
		{"disabled", high, "0", time.Time{}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var sent []string
			notify := func(_ context.Context, title, msg string) error {
				sent = append(sent, title+"|"+msg)
				return nil
			}
			ok, err := NotifySwap(t.Context(), notify, tt.mem, limitsWithSwap(t, tt.warn), tt.last, now)
			require.NoError(t, err)
			assert.Equal(t, tt.want, ok)
			if tt.want {
				require.Len(t, sent, 1)
				assert.Contains(t, sent[0], "12 GiB of swap")
			} else {
				assert.Empty(t, sent)
			}
		})
	}
}

func TestNotifySwapReportsNotifierFailure(t *testing.T) {
	notify := func(context.Context, string, string) error { return errors.New("no display") }
	ok, err := NotifySwap(t.Context(), notify, osshim.Memory{SwapUsed: 12 * swapGiB}, limitsWithSwap(t, "8G"), time.Time{}, time.Now())
	require.ErrorContains(t, err, "no display")
	assert.False(t, ok)
}

func TestNotifySwapNilNotifier(t *testing.T) {
	ok, err := NotifySwap(t.Context(), nil, osshim.Memory{SwapUsed: 12 * swapGiB}, limitsWithSwap(t, "8G"), time.Time{}, time.Now())
	require.NoError(t, err)
	assert.False(t, ok)
}

func TestProcessFamilies(t *testing.T) {
	var procs []osshim.Process
	add := func(n, ppid int, args string) {
		for range n {
			procs = append(procs, osshim.Process{PID: 100 + len(procs), PPID: ppid, Args: args})
		}
	}
	add(1, 0, "/sbin/launchd")
	add(12, 50, "node worker.js")
	add(5, 1, "node worker.js")
	add(6, 50, "go test ./...")
	add(3, 1, "python stale.py")
	add(2, 1, "daemon --once")
	add(3, 50, "rare tool")
	add(9, 2, "")
	add(4, 50, "/very/long/"+strings.Repeat("x", 100))

	got := ProcessFamilies(procs)
	require.Len(t, got, 3)
	assert.Equal(t, "17 x node worker.js (5 orphaned)", got[0].String())
	assert.Equal(t, "6 x go test ./...", got[1].String())
	assert.Equal(t, 4, got[2].Count)
	assert.LessOrEqual(t, len([]rune(got[2].Command)), familyCmdWidth)
	assert.True(t, strings.HasSuffix(got[2].Command, "…"))
}

func TestProcessFamiliesOrphansAlone(t *testing.T) {
	procs := []osshim.Process{
		{PID: 1, PPID: 0, Args: "init"},
		{PID: 10, PPID: 1, Args: "python stale.py"},
		{PID: 11, PPID: 1, Args: "python stale.py"},
		{PID: 12, PPID: 1, Args: "python stale.py"},
	}
	got := ProcessFamilies(procs)
	require.Len(t, got, 1)
	assert.Equal(t, 3, got[0].Orphans)
}

func TestProcessFamiliesNoneFound(t *testing.T) {
	assert.Empty(t, ProcessFamilies([]osshim.Process{{PID: 2, PPID: 1, Args: "a"}, {PID: 3, PPID: 1, Args: "b"}}))
}
