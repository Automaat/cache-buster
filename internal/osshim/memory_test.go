package osshim

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const linuxMeminfo = `MemTotal:       16384000 kB
MemFree:          200000 kB
MemAvailable:    1024000 kB
SwapTotal:       4194304 kB
SwapFree:        1048576 kB
HugePages_Total:       0
`

func TestParseMeminfo(t *testing.T) {
	m, err := ParseMeminfo(strings.NewReader(linuxMeminfo))
	require.NoError(t, err)
	assert.Equal(t, uint64(16384000)<<10, m.MemTotal)
	assert.Equal(t, uint64(1024000)<<10, m.MemAvailable)
	assert.Equal(t, uint64(4194304)<<10, m.SwapTotal)
	assert.Equal(t, uint64(3145728)<<10, m.SwapUsed)
	assert.Equal(t, -1, m.FreePercent)
}

func TestParseMeminfoWithoutMemAvailable(t *testing.T) {
	m, err := ParseMeminfo(strings.NewReader("MemTotal: 100 kB\nMemFree: 40 kB\n"))
	require.NoError(t, err)
	assert.Equal(t, uint64(40<<10), m.MemAvailable)
	assert.Zero(t, m.SwapTotal)
	assert.Zero(t, m.SwapUsed)
}

func TestParseMeminfoRejectsGarbage(t *testing.T) {
	_, err := ParseMeminfo(strings.NewReader("hello\nworld: x\n"))
	require.Error(t, err)
}

func TestParseSwapUsage(t *testing.T) {
	tests := []struct {
		name        string
		in          string
		used, total uint64
		wantErr     bool
	}{
		{
			name:  "megabytes",
			in:    "vm.swapusage: total = 4096.00M  used = 2048.50M  free = 2047.50M  (encrypted)\n",
			used:  2048<<20 + 1<<19,
			total: 4096 << 20,
		},
		{
			name:  "gigabytes",
			in:    "vm.swapusage: total = 20.00G  used = 18.25G  free = 1.75G  (encrypted)",
			used:  18<<30 + 1<<28,
			total: 20 << 30,
		},
		{name: "no swap", in: "vm.swapusage: total = 0.00M  used = 0.00M  free = 0.00M  (encrypted)"},
		{name: "empty", in: "", wantErr: true},
		{name: "unrelated", in: "sysctl: unknown oid 'vm.swapusage'", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			used, total, err := ParseSwapUsage(tt.in)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.used, used)
			assert.Equal(t, tt.total, total)
		})
	}
}

const memoryPressureOut = `The system has 17179869184 (1048576 pages with a page size of 16384).

Stats:
Pages free: 12345
Pages purgeable: 100

System-wide memory free percentage: 3%
`

func TestParseMemoryPressure(t *testing.T) {
	pct, err := ParseMemoryPressure(memoryPressureOut)
	require.NoError(t, err)
	assert.Equal(t, 3, pct)

	_, err = ParseMemoryPressure("nothing here")
	require.Error(t, err)
	_, err = ParseMemoryPressure("System-wide memory free percentage: 400%")
	require.Error(t, err)
}

func TestReadMemoryWithDarwinCommands(t *testing.T) {
	run := func(_ context.Context, name string, _ ...string) (string, error) {
		switch name {
		case "sysctl":
			return "vm.swapusage: total = 2048.00M  used = 1024.00M  free = 1024.00M  (encrypted)", nil
		case "memory_pressure":
			return memoryPressureOut, nil
		}
		return "", errors.New("unexpected " + name)
	}
	m, err := readMemoryWith(context.Background(), run)
	require.NoError(t, err)
	assert.Equal(t, uint64(1024<<20), m.SwapUsed)
	assert.Equal(t, uint64(2048<<20), m.SwapTotal)
	assert.Equal(t, 3, m.FreePercent)
}

func TestReadMemoryWithKeepsSwapWhenPressureFails(t *testing.T) {
	run := func(_ context.Context, name string, _ ...string) (string, error) {
		if name == "memory_pressure" {
			return "", errors.New("not found")
		}
		return "total = 100.00M used = 50.00M free = 50.00M", nil
	}
	m, err := readMemoryWith(context.Background(), run)
	require.NoError(t, err)
	assert.Equal(t, uint64(50<<20), m.SwapUsed)
	assert.Equal(t, -1, m.FreePercent)
}

func TestReadMemoryWithFailsWhenSysctlFails(t *testing.T) {
	run := func(context.Context, string, ...string) (string, error) { return "", errors.New("boom") }
	_, err := readMemoryWith(context.Background(), run)
	require.Error(t, err)
}

func TestMemoryFromCommit(t *testing.T) {
	const gib = uint64(1) << 30
	tests := []struct {
		name string
		in   CommitStatus
		want Memory
	}{
		{
			name: "commit beyond RAM lands in the page file",
			in:   CommitStatus{TotalPhys: 16 * gib, AvailPhys: 1 * gib, TotalPageFile: 24 * gib, AvailPageFile: 2 * gib},
			want: Memory{MemTotal: 16 * gib, MemAvailable: 1 * gib, SwapTotal: 8 * gib, SwapUsed: 7 * gib, FreePercent: -1},
		},
		{
			name: "commit inside RAM uses no page file",
			in:   CommitStatus{TotalPhys: 16 * gib, AvailPhys: 8 * gib, TotalPageFile: 24 * gib, AvailPageFile: 16 * gib},
			want: Memory{MemTotal: 16 * gib, MemAvailable: 8 * gib, SwapTotal: 8 * gib, FreePercent: -1},
		},
		{
			name: "inconsistent counters report no swap",
			in:   CommitStatus{TotalPhys: 4 * gib, AvailPhys: 8 * gib, TotalPageFile: 4 * gib, AvailPageFile: 8 * gib},
			want: Memory{MemTotal: 4 * gib, MemAvailable: 8 * gib, FreePercent: -1},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, MemoryFromCommit(tt.in))
		})
	}
}
