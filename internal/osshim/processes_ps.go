//go:build !linux && !windows

package osshim

import (
	"bytes"
	"context"
	"os/exec"
	"strconv"
	"strings"
)

// ProcessTable returns every running process, listed with ps.
func ProcessTable(ctx context.Context) ([]Process, error) {
	out, err := exec.CommandContext(ctx, "ps", "-axo", "pid=,ppid=,command=").Output()
	if err != nil {
		return nil, err
	}
	return parsePS(string(bytes.TrimSpace(out))), nil
}

// parsePS parses "pid ppid command" rows. A row it cannot parse keeps its
// text as the command line, so it still counts toward a busy match.
func parsePS(out string) []Process {
	var procs []Process
	for line := range strings.SplitSeq(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 3 {
			pid, errPID := strconv.Atoi(fields[0])
			ppid, errPPID := strconv.Atoi(fields[1])
			if errPID == nil && errPPID == nil {
				rest := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), fields[0]))
				rest = strings.TrimSpace(strings.TrimPrefix(rest, fields[1]))
				procs = append(procs, Process{PID: pid, PPID: ppid, CommandLine: rest})
				continue
			}
		}
		procs = append(procs, Process{CommandLine: strings.TrimSpace(line)})
	}
	return procs
}
