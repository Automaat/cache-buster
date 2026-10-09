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

// ErrNewDirExists reports that the legacy dir was left in place because the
// current dir already exists; the user has to merge them by hand.
var ErrNewDirExists = errors.New("the new dir already exists")

// Dir moves oldDir to newDir when oldDir exists and newDir does not. It never
// overwrites: when both exist and oldDir holds anything, both stay untouched
// and the error wraps ErrNewDirExists so the caller can tell the user to move
// the rest by hand. A symlink to a directory (home-manager, stow) is
// recreated at newDir, pointing at the same target, and the old link is
// removed. An oldDir that is not a readable directory is skipped with an error.
func Dir(oldDir, newDir string) (bool, error) {
	info, err := os.Lstat(oldDir)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	isLink := info.Mode()&os.ModeSymlink != 0
	var statErr error
	if isLink {
		info, statErr = os.Stat(oldDir)
	}
	_, err = os.Lstat(newDir)
	if err == nil {
		return false, skipOrWarn(oldDir, newDir)
	}
	if !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	if statErr != nil {
		return false, fmt.Errorf("symlink does not lead to a directory: %w", statErr)
	}
	if !info.IsDir() {
		return false, errors.New("not a directory")
	}
	if isLink {
		return moveLink(oldDir, newDir)
	}
	if err := checkReadable(oldDir); err != nil {
		return false, tolerateLostRace(err, oldDir)
	}
	if err := os.MkdirAll(filepath.Dir(newDir), 0o750); err != nil {
		return false, err
	}
	if err := rename(oldDir, newDir); err != nil {
		return false, tolerateLostRace(err, oldDir)
	}
	return true, nil
}

// skipOrWarn settles a legacy path next to an existing newDir: silent unless
// it is a directory (or a link to one) that still holds data.
func skipOrWarn(oldDir, newDir string) error {
	if !isDir(oldDir) {
		return nil
	}
	return newDirExists(oldDir, newDir)
}

func isDir(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.IsDir()
}

// newDirExists is nil for an empty oldDir, which holds nothing to lose, and
// an actionable error otherwise.
func newDirExists(oldDir, newDir string) error {
	oldInfo, oldErr := os.Stat(oldDir)
	newInfo, newErr := os.Stat(newDir)
	if oldErr == nil && newErr == nil && os.SameFile(oldInfo, newInfo) {
		return nil
	}
	entries, err := os.ReadDir(oldDir)
	if err == nil && len(entries) == 0 {
		return nil
	}
	return fmt.Errorf("%w (%s): move what you still need from %s by hand, then remove it",
		ErrNewDirExists, newDir, oldDir)
}

// moveLink recreates the symlink oldDir at newDir with the same target, made absolute
// when the two dirs differ in parent, then drops the old one.
func moveLink(oldDir, newDir string) (bool, error) {
	target, err := os.Readlink(oldDir)
	if err != nil {
		return false, err
	}
	if !filepath.IsAbs(target) && filepath.Dir(oldDir) != filepath.Dir(newDir) {
		parent, err := filepath.EvalSymlinks(filepath.Dir(oldDir))
		if err != nil {
			return false, err
		}
		target = filepath.Join(parent, target)
	}
	if err := os.MkdirAll(filepath.Dir(newDir), 0o750); err != nil {
		return false, err
	}
	if err := os.Symlink(target, newDir); err != nil {
		if errors.Is(err, os.ErrExist) {
			return false, nil
		}
		return false, tolerateLostRace(err, oldDir)
	}
	if err := os.Remove(oldDir); err != nil {
		if !pathExists(oldDir) {
			return true, nil
		}
		_ = os.Remove(newDir)
		return false, err
	}
	return true, nil
}

var rename = os.Rename

// tolerateLostRace drops a not-exist error once oldDir is gone, whether a
// concurrent run moved it or the user deleted it; any other failure is
// returned as is.
func tolerateLostRace(err error, oldDir string) error {
	if errors.Is(err, os.ErrNotExist) && !pathExists(oldDir) {
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
