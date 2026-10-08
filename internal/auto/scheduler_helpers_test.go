package auto

import (
	"bytes"
	"context"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var updateGolden = flag.Bool("update", false, "rewrite golden files")

func assertGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name+".golden")
	if *updateGolden {
		require.NoError(t, os.WriteFile(path, []byte(got), 0o600))
	}
	want, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, strings.ReplaceAll(string(want), "\r\n", "\n"), got)
}

type scriptedExec struct {
	mu      sync.Mutex
	calls   [][]string
	respond func(name string, args []string) ([]byte, error)
}

func (s *scriptedExec) exec(_ context.Context, name string, args ...string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, append([]string{name}, args...))
	if s.respond == nil {
		return nil, nil
	}
	return s.respond(name, args)
}

func (s *scriptedExec) reset() { s.calls = nil }

func (s *scriptedExec) commands() []string {
	out := make([]string, len(s.calls))
	for i, c := range s.calls {
		out[i] = strings.Join(c, " ")
	}
	return out
}

func newOSAgent(t *testing.T, goos, exe string, s *scriptedExec) Agent {
	t.Helper()
	home := t.TempDir()
	return Agent{
		Exec:       s.exec,
		Out:        &bytes.Buffer{},
		Home:       home,
		Exe:        exe,
		StateDir:   filepath.Join(home, "state"),
		UID:        501,
		Interval:   45 * time.Minute,
		RetryDelay: time.Nanosecond,
		OS:         goos,
		Now:        func() time.Time { return time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC) },
	}
}

func output(a Agent) string { return a.Out.(*bytes.Buffer).String() }
