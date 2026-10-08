//go:build !linux && !windows

package osshim

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os/exec"
	"path/filepath"
	"strings"
)

// OpenFilesUnder reports, for each dir, whether any process has a file open
// inside it, from one lsof listing of every open file matched in Go. An error
// means the answer is unknown and every dir must count as open.
func OpenFilesUnder(ctx context.Context, dirs []string) (map[string]bool, error) {
	path, err := exec.LookPath("lsof")
	if err != nil {
		return nil, errors.New("lsof not found")
	}
	var live, resolved []string
	for _, dir := range dirs {
		r, evalErr := filepath.EvalSymlinks(dir)
		if errors.Is(evalErr, fs.ErrNotExist) {
			continue
		}
		if evalErr != nil {
			return nil, evalErr
		}
		live, resolved = append(live, dir), append(resolved, r)
	}
	open := make(map[string]bool, len(dirs))
	if len(live) == 0 {
		return open, nil
	}

	cmd := exec.CommandContext(ctx, path, "-n", "-P", "-Fn")
	var stderr strings.Builder
	cmd.Stderr = &stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for scanner.Scan() {
		name, ok := strings.CutPrefix(scanner.Text(), "n")
		if !ok {
			continue
		}
		for i, dir := range resolved {
			if !open[live[i]] && underDir(name, dir) {
				open[live[i]] = true
			}
		}
	}
	scanErr := scanner.Err()
	waitErr := cmd.Wait()
	if scanErr != nil {
		return nil, scanErr
	}
	if waitErr != nil {
		var exitErr *exec.ExitError
		if !errors.As(waitErr, &exitErr) || exitErr.ExitCode() != 1 {
			return nil, waitErr
		}
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return nil, fmt.Errorf("lsof could not inspect every file: %s", msg)
		}
	}
	return open, nil
}
