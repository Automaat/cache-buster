package osshim

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseLsofCwd(t *testing.T) {
	out := "p100\nfcwd\nn/Users/u/proj\np200\nfcwd\nn/tmp/with space\npbad\nn/ignored\n"
	assert.Equal(t, map[int]string{100: "/Users/u/proj", 200: "/tmp/with space"}, parseLsofCwd(out))
	assert.Empty(t, parseLsofCwd(""))
}

func TestProcessCwdsSelf(t *testing.T) {
	cwds, err := ProcessCwds(context.Background(), []int{os.Getpid()})
	if err != nil {
		t.Skipf("cwd lookup unavailable here: %v", err)
	}
	want, err := os.Getwd()
	require.NoError(t, err)
	if got, ok := cwds[os.Getpid()]; ok {
		wantReal, _ := filepathEval(want)
		gotReal, _ := filepathEval(got)
		assert.Equal(t, wantReal, gotReal)
	}
}

func filepathEval(p string) (string, error) { return filepath.EvalSymlinks(p) }
