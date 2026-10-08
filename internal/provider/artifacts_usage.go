package provider

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// usageEntryLimit bounds how many directory entries one usage probe reads.
const usageEntryLimit = 20000

var errUsageTooLarge = errors.New("too many entries to judge whether it is in use")

// usageProbe collects the newest sign that an artifact was built or run.
type usageProbe struct {
	newest time.Time
	seen   int
}

func (u *usageProbe) note(t time.Time) {
	if t.After(u.newest) {
		u.newest = t
	}
}

// dir records the mtime of a directory itself. Its atime is ignored: listing
// it, which every scan does, would refresh it.
func (u *usageProbe) dir(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	}
	u.note(info.ModTime())
	return nil
}

// entries records the mtime of each direct entry of dir and the atime of the
// regular files among them. With follow, symlinks count as the files they
// point to, which is how a node_modules/.bin shim reports its script.
func (u *usageProbe) entries(dir string, follow bool, skip string) error {
	list, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	}
	for _, e := range list {
		u.seen++
		if u.seen > usageEntryLimit {
			return errUsageTooLarge
		}
		if e.Name() == skip {
			continue
		}
		path := filepath.Join(dir, e.Name())
		info, infoErr := e.Info()
		if infoErr != nil {
			if errors.Is(infoErr, fs.ErrNotExist) {
				continue
			}
			return infoErr
		}
		u.note(info.ModTime())
		if follow && info.Mode()&fs.ModeSymlink != 0 {
			if target, statErr := os.Stat(path); statErr == nil {
				info = target
				u.note(info.ModTime())
			}
		}
		if info.Mode().IsRegular() {
			u.note(fileAtime(info))
		}
	}
	return nil
}

// artifactUse returns the newest sign that art was built or run: the mtime of
// its top-level entries and of the executables a scheduled job would start,
// plus the atime of those files. atime is only an extra signal: noatime and
// relatime mounts make it stale, never newer than the truth.
func artifactUse(art *artifactDir) (time.Time, error) {
	u := &usageProbe{}
	err := u.dir(art.Path)
	if err == nil {
		switch art.Kind {
		case kindRust:
			err = rustUse(u, art.Path)
		case kindNode:
			err = u.entries(art.Path, false, "")
			if err == nil {
				err = u.entries(filepath.Join(art.Path, ".bin"), true, "")
			}
		case kindPython:
			err = u.entries(art.Path, false, "")
			for _, bin := range []string{"bin", "Scripts"} {
				if err == nil {
					err = u.entries(filepath.Join(art.Path, bin), true, "")
				}
			}
		}
	}
	if err != nil {
		return time.Time{}, fmt.Errorf("cannot tell whether the artifact is in use: %w", err)
	}
	return u.newest, nil
}

// rustUse reads target itself, each directory directly below it (debug,
// release and custom profile directories) and the debug or release
// directory of each target-triple directory.
func rustUse(u *usageProbe, target string) error {
	if err := u.entries(target, false, "CACHEDIR.TAG"); err != nil {
		return err
	}
	top, err := os.ReadDir(target)
	if err != nil {
		return err
	}
	var profiles []string
	for _, e := range top {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(target, e.Name())
		profiles = append(profiles, dir)
		for _, name := range []string{"debug", "release"} {
			if isRealDir(filepath.Join(dir, name)) {
				profiles = append(profiles, filepath.Join(dir, name))
			}
		}
	}
	for _, dir := range profiles {
		if err := u.dir(dir); err != nil {
			return err
		}
		if err := u.entries(dir, false, ""); err != nil {
			return err
		}
	}
	return nil
}
