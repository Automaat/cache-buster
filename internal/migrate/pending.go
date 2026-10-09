package migrate

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

const configFile = "config.yaml"

// Pending describes a legacy config that holds the user's settings while the
// current location has none, so a command would silently run on defaults.
type Pending struct {
	Old, New string
}

// PendingConfig reports a legacy config file that has not reached the current
// config dir. It fails closed: a legacy file that cannot even be stat'ed
// counts as present, a current one that cannot be stat'ed as missing.
func PendingConfig(home string) *Pending {
	pair := Dirs(home)[0]
	if !isDir(pair[0]) {
		return nil
	}
	if _, err := os.Stat(filepath.Join(pair[0], configFile)); errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if _, err := os.Lstat(filepath.Join(pair[1], configFile)); err == nil {
		return nil
	}
	return &Pending{Old: pair[0], New: pair[1]}
}

// Step is the manual command that finishes the migration of the config. A
// file sitting at the new path is set aside first.
func (p Pending) Step() string {
	move := func(src, dst string) string {
		if runtime.GOOS == "windows" {
			return fmt.Sprintf("Move-Item -LiteralPath %q -Destination %q", src, dst)
		}
		return fmt.Sprintf("mv %q %q", src, dst)
	}
	info, err := os.Lstat(p.New)
	switch {
	case err != nil:
		return move(p.Old, p.New)
	case info.IsDir():
		return move(filepath.Join(p.Old, configFile), filepath.Join(p.New, configFile))
	default:
		return move(p.New, p.New+".bak") + sep() + move(p.Old, p.New)
	}
}

func sep() string {
	if runtime.GOOS == "windows" {
		return "; "
	}
	return " && "
}
