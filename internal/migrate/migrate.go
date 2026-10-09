// Package migrate moves the config and state directories of the project's
// former name to the current one.
package migrate

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

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

// Issue is a legacy dir that could not be migrated, or only in part.
type Issue struct {
	Old, New string
	Err      error
}

// Legacy moves each legacy dir under home to its current location and writes a
// note per move to out. A dir that is skipped or fails is reported as a
// warning, at most once per kind of failure and day (see Throttle), and never
// aborts the command. The returned issues list every dir left behind, whether
// or not it was warned about.
func Legacy(home string, out io.Writer) []Issue {
	var issues []Issue
	for _, pair := range Dirs(home) {
		moved, err := Dir(pair[0], pair[1])
		if moved {
			fmt.Fprintf(out, "migrated %s to %s\n", pair[0], pair[1])
		}
		if err == nil {
			Resolved(home, pair[0])
			continue
		}
		issues = append(issues, Issue{Old: pair[0], New: pair[1], Err: err})
		if ShouldReport(home, pair[0], err.Error()) {
			fmt.Fprintf(out, "warning: not migrating %s: %v\n", pair[0], err)
		}
	}
	return issues
}

// ErrNewDirExists reports that entries of the legacy dir were left in place
// because the current dir already holds an entry of the same name; the user
// has to merge them by hand.
var ErrNewDirExists = errors.New("the new dir already exists")

// Dir moves oldDir to newDir when oldDir exists and newDir does not. When
// both are real directories, every entry of oldDir that newDir lacks moves
// over and the rest stays: nothing is ever overwritten, and the error wraps
// ErrNewDirExists so the caller can tell the user to move the rest by hand.
// A symlink to a directory (home-manager, stow) or a Windows junction is
// renamed as a link, so its target is never touched. An oldDir that is not a
// directory is skipped with an error. Moving needs no read access to the
// contents: a rename inside one parent only edits directory entries. A relative
// link only keeps its meaning when oldDir and newDir share a parent, as the
// pairs from Dirs do.
func Dir(oldDir, newDir string) (bool, error) {
	info, err := os.Lstat(oldDir)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	linkLike := info.Mode()&(os.ModeSymlink|os.ModeIrregular) != 0
	var statErr error
	if linkLike {
		info, statErr = os.Stat(oldDir)
	}
	newInfo, err := os.Lstat(newDir)
	if err == nil {
		if !linkLike && statErr == nil && info.IsDir() && newInfo.IsDir() {
			return merge(oldDir, newDir)
		}
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
	if err := os.MkdirAll(filepath.Dir(newDir), 0o750); err != nil {
		return false, err
	}
	if err := rename(oldDir, newDir); err != nil {
		return false, tolerateLostRace(err, oldDir)
	}
	return true, nil
}

// merge moves the entries of oldDir that newDir lacks, then drops oldDir if it
// ended up empty. Each rename is atomic, so concurrent runs split the entries
// between them and only the winner of an entry counts it as moved.
func merge(oldDir, newDir string) (bool, error) {
	entries, err := os.ReadDir(oldDir)
	if err != nil {
		return false, tolerateLostRace(err, oldDir)
	}
	moved := false
	var kept []string
	for _, e := range entries {
		src, dst := filepath.Join(oldDir, e.Name()), filepath.Join(newDir, e.Name())
		if pathExists(dst) {
			if emptyFile(src) {
				_ = os.Remove(src)
				continue
			}
			kept = append(kept, e.Name())
			continue
		}
		if err := rename(src, dst); err != nil {
			if tolerateLostRace(err, src) == nil {
				continue
			}
			return moved, err
		}
		moved = true
	}
	if len(kept) == 0 {
		_ = os.Remove(oldDir)
		return moved, nil
	}
	return moved, fmt.Errorf("%w (%s): %s in %s also exist there; move what you still need by hand, then remove them",
		ErrNewDirExists, newDir, strings.Join(kept, ", "), oldDir)
}

func emptyFile(path string) bool {
	info, err := os.Lstat(path)
	return err == nil && info.Mode().IsRegular() && info.Size() == 0
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
