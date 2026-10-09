package provider

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const fakeToolEnv = "BILGIE_FAKE_TOOL"

// fakeReply is what a fake tool prints and returns for one invocation.
// SleepMS delays the reply so a timeout test has something to kill, Touch
// creates a file before replying, Log appends the arguments and the uv cache
// environment to a file, and IfExists switches to Then once that
// file exists.
type fakeReply struct {
	Stdout   string     `json:"stdout"`
	Stderr   string     `json:"stderr"`
	Exit     int        `json:"exit"`
	SleepMS  int        `json:"sleep_ms"`
	Touch    string     `json:"touch"`
	Log      string     `json:"log"`
	Remove   []string   `json:"remove"`
	IfExists string     `json:"if_exists"`
	Then     *fakeReply `json:"then"`
}

// fakeToolSpec maps an invocation to its reply. A key is the first two
// arguments joined by a space, or the first argument alone; Default answers
// anything else.
type fakeToolSpec struct {
	Default fakeReply            `json:"default"`
	Replies map[string]fakeReply `json:"replies"`
}

// TestMain doubles as the fake tool: a copy of the test binary named like the
// tool answers from the spec in the environment, on every OS, with no shell.
func TestMain(m *testing.M) {
	if spec := os.Getenv(fakeToolEnv); spec != "" {
		os.Exit(runFakeTool(spec, os.Args[1:], os.Stdout, os.Stderr))
	}
	os.Exit(m.Run())
}

func runFakeTool(raw string, args []string, stdout, stderr io.Writer) int {
	var spec fakeToolSpec
	if err := json.Unmarshal([]byte(raw), &spec); err != nil {
		fmt.Fprintln(stderr, "fake tool spec:", err)
		return 2
	}
	reply := spec.Default
	if len(args) >= 1 {
		if r, ok := spec.Replies[args[0]]; ok {
			reply = r
		}
	}
	if len(args) >= 2 {
		if r, ok := spec.Replies[args[0]+" "+args[1]]; ok {
			reply = r
		}
	}
	if reply.IfExists != "" && reply.Then != nil {
		if _, err := os.Stat(reply.IfExists); err == nil {
			reply = *reply.Then
		}
	}
	if reply.Log != "" {
		line := strings.Join(args, " ") + "\tUV_CACHE_DIR=" + os.Getenv("UV_CACHE_DIR") +
			"\tUV_NO_CACHE=" + os.Getenv("UV_NO_CACHE") + "\n"
		f, err := os.OpenFile(reply.Log, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
		if err != nil {
			fmt.Fprintln(stderr, "fake tool log:", err)
			return 2
		}
		_, werr := f.WriteString(line)
		if cerr := f.Close(); werr != nil || cerr != nil {
			fmt.Fprintln(stderr, "fake tool log write failed")
			return 2
		}
	}
	if reply.Touch != "" {
		if err := os.WriteFile(reply.Touch, nil, 0o600); err != nil {
			fmt.Fprintln(stderr, "fake tool touch:", err)
			return 2
		}
	}
	for _, dir := range reply.Remove {
		if err := os.RemoveAll(dir); err != nil {
			fmt.Fprintln(stderr, "fake tool remove:", err)
			return 2
		}
	}
	if reply.SleepMS > 0 {
		time.Sleep(time.Duration(reply.SleepMS) * time.Millisecond)
	}
	fmt.Fprint(stdout, reply.Stdout)
	fmt.Fprint(stderr, reply.Stderr)
	return reply.Exit
}

func executableName(name string) string {
	if runtime.GOOS == "windows" {
		return name + ".exe"
	}
	return name
}

// installFakeTool puts a fake executable called name first on PATH. It
// returns the directory holding it.
func installFakeTool(t *testing.T, name string, spec fakeToolSpec) string {
	t.Helper()
	self, err := os.Executable()
	require.NoError(t, err)
	raw, err := json.Marshal(spec)
	require.NoError(t, err)

	dir := t.TempDir()
	bin := filepath.Join(dir, executableName(name))
	if err := os.Link(self, bin); err != nil {
		copyFile(t, self, bin)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv(fakeToolEnv, string(raw))
	return dir
}

func copyFile(t *testing.T, from, to string) {
	t.Helper()
	src, err := os.Open(from)
	require.NoError(t, err)
	defer src.Close()
	dst, err := os.OpenFile(to, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	require.NoError(t, err)
	_, err = io.Copy(dst, src)
	require.NoError(t, err)
	require.NoError(t, dst.Close())
}

func TestRunFakeTool_RepliesByArguments(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "marker")
	spec := fakeToolSpec{
		Default: fakeReply{Exit: 7},
		Replies: map[string]fakeReply{
			"ps --quiet": {Stdout: "two-arg\n"},
			"ps":         {Stdout: "one-arg\n"},
			"touch":      {Touch: marker, IfExists: marker, Then: &fakeReply{Stdout: "again\n"}},
			"fail":       {Stderr: "boom", Exit: 1},
		},
	}
	raw, err := json.Marshal(spec)
	require.NoError(t, err)

	run := func(args ...string) (int, string, string) {
		var out, errOut strings.Builder
		code := runFakeTool(string(raw), args, &out, &errOut)
		return code, out.String(), errOut.String()
	}

	code, out, _ := run("ps", "--quiet")
	require.Equal(t, 0, code)
	require.Equal(t, "two-arg\n", out)

	_, out, _ = run("ps", "-a")
	require.Equal(t, "one-arg\n", out)

	code, _, _ = run("other")
	require.Equal(t, 7, code)

	code, _, errOut := run("fail")
	require.Equal(t, 1, code)
	require.Equal(t, "boom", errOut)

	_, out, _ = run("touch")
	require.Empty(t, out)
	_, out, _ = run("touch")
	require.Equal(t, "again\n", out)
}
