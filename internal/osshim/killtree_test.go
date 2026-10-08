package osshim

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDescendants(t *testing.T) {
	procs := []procInfo{
		{pid: 1, ppid: 0},
		{pid: 10, ppid: 1},
		{pid: 11, ppid: 1},
		{pid: 100, ppid: 10},
		{pid: 1000, ppid: 100},
		{pid: 20, ppid: 2},
		{pid: 30, ppid: 30},
	}

	assert.Equal(t, []uint32{1000, 100, 11, 10}, descendants(1, procs), "deepest first")
	assert.Equal(t, []uint32{1000, 100}, descendants(10, procs))
	assert.Empty(t, descendants(11, procs))
	assert.Empty(t, descendants(999, procs))
	assert.Empty(t, descendants(30, procs), "self-parent rows must not loop")
}

func TestDescendantsSurvivesCycles(t *testing.T) {
	procs := []procInfo{{pid: 2, ppid: 1}, {pid: 3, ppid: 2}, {pid: 1, ppid: 3}}
	assert.ElementsMatch(t, []uint32{2, 3}, descendants(1, procs))
}

func TestKillTreeOnCancelKillsDescendants(t *testing.T) {
	beat := heartbeatPath(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0])
	cmd.Env = append(os.Environ(), helperEnv+"=parent", "OSSHIM_HEARTBEAT="+beat)
	KillTreeOnCancel(cmd)
	cmd.WaitDelay = 5 * time.Second
	require.NoError(t, cmd.Start())

	require.Eventually(t, func() bool {
		_, err := os.Stat(beat)
		return err == nil
	}, 10*time.Second, 50*time.Millisecond, "grandchild never started")

	cancel()
	_ = cmd.Wait()

	require.Eventually(t, func() bool {
		before, err := os.ReadFile(beat)
		if err != nil {
			return false
		}
		time.Sleep(400 * time.Millisecond)
		after, err := os.ReadFile(beat)
		return err == nil && bytes.Equal(before, after)
	}, 10*time.Second, 50*time.Millisecond, "grandchild kept running after cancel")
}
