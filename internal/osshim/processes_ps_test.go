//go:build !linux && !windows

package osshim

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParsePS(t *testing.T) {
	procs := parsePS("    1     0 /sbin/launchd\n  812   1 cache-buster clean cargo\ngarbage")
	assert.Equal(t, []Process{
		{PID: 1, PPID: 0, CommandLine: "/sbin/launchd", Args: "/sbin/launchd"},
		{PID: 812, PPID: 1, CommandLine: "cache-buster clean cargo", Args: "cache-buster clean cargo"},
		{CommandLine: "garbage", Args: "garbage"},
	}, procs)
}
