//go:build !linux && !windows

package osshim

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParsePS(t *testing.T) {
	procs := parsePS("    1     0 /sbin/launchd\n  812   1 bilgie clean cargo\ngarbage")
	assert.Equal(t, []Process{
		{PID: 1, PPID: 0, CommandLine: "/sbin/launchd", Args: "/sbin/launchd"},
		{PID: 812, PPID: 1, CommandLine: "bilgie clean cargo", Args: "bilgie clean cargo"},
		{CommandLine: "garbage", Args: "garbage"},
	}, procs)
}
