// Package migrate moves the config and state directories of the project's
// former name to the current one.
package migrate

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/smykla-skalski/bilgie/internal/appname"
)

// Dirs returns the legacy and current location of the config and state dirs
// under home, matching the roots the config and auto packages use.
func Dirs(home string) [][2]string {
	return [][2]string{
		{
			filepath.Join(home, ".config", appname.Legacy),
			filepath.Join(home, ".config", appname.Name),
		},
		{
			filepath.Join(home, ".local", "state", appname.Legacy),
			filepath.Join(home, ".local", "state", appname.Name),
		},
	}
}

// Legacy moves each legacy dir under home to its current location and writes a
// one-line note per move to out. A dir that is skipped or fails is reported as
// a warning and never aborts the command.
func Legacy(home string, out io.Writer) {
	for _, pair := range Dirs(home) {
		moved, err := Dir(pair[0], pair[1])
		switch {
		case err != nil:
			fmt.Fprintf(out, "warning: not migrating %s: %v\n", pair[0], err)
		case moved:
			fmt.Fprintf(out, "migrated %s to %s\n", pair[0], pair[1])
		}
	}
}

// Dir moves oldDir to newDir when oldDir exists and newDir does not. It never
// overwrites: an existing newDir leaves both untouched. An oldDir that is not
// a readable directory is skipped with an error.
func Dir(oldDir, newDir string) (bool, error) {
	info, err := os.Lstat(oldDir)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	_, err = os.Lstat(newDir)
	if err == nil {
		return false, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	if !info.IsDir() {
		return false, errors.New("not a directory")
	}
	if err := checkReadable(oldDir); err != nil {
		return false, tolerateLostRace(err, oldDir, newDir)
	}
	if err := os.MkdirAll(filepath.Dir(newDir), 0o750); err != nil {
		return false, err
	}
	if err := rename(oldDir, newDir); err != nil {
		return false, tolerateLostRace(err, oldDir, newDir)
	}
	return true, nil
}

var rename = os.Rename

// tolerateLostRace drops a not-exist error when another process has already
// moved oldDir to newDir; any other failure is returned as is.
func tolerateLostRace(err error, oldDir, newDir string) error {
	if errors.Is(err, os.ErrNotExist) && !pathExists(oldDir) && pathExists(newDir) {
		return nil
	}
	return err
}

func pathExists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

// checkReadable lists the dir and opens its files, so a dir with unreadable
// content stays where it is instead of being moved half-usable.
func checkReadable(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if !e.Type().IsRegular() {
			continue
		}
		f, err := os.Open(filepath.Join(dir, e.Name()))
		if err != nil {
			return err
		}
		_ = f.Close()
	}
	return nil
}
