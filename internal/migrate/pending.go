package migrate

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

const configFile = "config.yaml"

// Pending describes a legacy config that holds the user's settings while the
// current location has none, so a command would silently run on defaults.
type Pending struct {
	Old, New string
}

// PendingConfig reports a legacy config file that has not reached the current
// config dir. It fails closed: a legacy file that cannot even be stat'ed
// counts as present, while a current one counts as absent unless it is a
// regular file (after following links) with actual YAML content: a dangling
// link, a directory, an empty or comment-only file all leave defaults in force.
func PendingConfig(home string) *Pending {
	pair := Dirs(home)[0]
	if !isDir(pair[0]) {
		return nil
	}
	if _, err := os.Stat(filepath.Join(pair[0], configFile)); errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if hasContent(filepath.Join(pair[1], configFile)) {
		return nil
	}
	return &Pending{Old: pair[0], New: pair[1]}
}

// maxProbe bounds how much of a config is read to look for content.
const maxProbe = 1 << 20

// hasContent is true for a regular file that holds a line other than blank,
// a comment or a YAML document marker. A file it cannot read counts as
// content: the loader reports that error itself.
func hasContent(path string) bool {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return false
	}
	f, err := os.Open(path)
	if err != nil {
		return true
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxProbe))
	if err != nil {
		return true
	}
	for line := range strings.SplitSeq(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "#") && line != "---" && line != "..." {
			return true
		}
	}
	return false
}

// Step is the manual command that finishes the migration of the config. A
// file, link or directory sitting where the new config belongs is set aside
// first, never overwritten.
func (p Pending) Step() string {
	move := func(src, dst string) string {
		if runtime.GOOS == "windows" {
			return fmt.Sprintf("Move-Item -LiteralPath %s -Destination %s", shellQuote(src), shellQuote(dst))
		}
		return fmt.Sprintf("mv %s %s", shellQuote(src), shellQuote(dst))
	}
	info, err := os.Lstat(p.New)
	switch {
	case err != nil:
		return move(p.Old, p.New)
	case !info.IsDir():
		return move(p.New, freeName(p.New+".bak")) + sep() + move(p.Old, p.New)
	}
	oldCfg, newCfg := filepath.Join(p.Old, configFile), filepath.Join(p.New, configFile)
	if pathExists(newCfg) {
		return move(newCfg, freeName(newCfg+".bak")) + sep() + move(oldCfg, newCfg)
	}
	return move(oldCfg, newCfg)
}

func sep() string {
	if runtime.GOOS == "windows" {
		return "; "
	}
	return " && "
}

// freeName appends a counter until nothing exists at the path, so setting a
// file aside never replaces an earlier backup.
func freeName(path string) string {
	candidate := path
	for i := 2; pathExists(candidate); i++ {
		candidate = fmt.Sprintf("%s.%d", path, i)
	}
	return candidate
}

// shellQuote single-quotes s for POSIX shells and PowerShell, which both treat
// the contents literally, so $ and backticks in a path survive a paste.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", func() string {
		if runtime.GOOS == "windows" {
			return "''"
		}
		return `'\''`
	}()) + "'"
}
