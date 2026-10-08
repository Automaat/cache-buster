package osshim

import (
	"errors"
	"strconv"
	"strings"
)

// ErrCwdUnsupported is returned where the working directory of another
// process cannot be read; callers must treat the directory as unknown.
var ErrCwdUnsupported = errors.New("process working directories cannot be read on windows")

// parseLsofCwd parses `lsof -Fpn` output: a "p<pid>" line starts a process
// and the following "n<path>" line is its cwd.
func parseLsofCwd(out string) map[int]string {
	cwds := map[int]string{}
	pid := 0
	for line := range strings.SplitSeq(out, "\n") {
		line = strings.TrimRight(line, "\r")
		switch {
		case strings.HasPrefix(line, "p"):
			n, err := strconv.Atoi(line[1:])
			if err != nil {
				pid = 0
				continue
			}
			pid = n
		case strings.HasPrefix(line, "n") && pid != 0:
			cwds[pid] = line[1:]
			pid = 0
		}
	}
	return cwds
}
