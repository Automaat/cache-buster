package migrate

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"
)

// reportEvery is how long a repeating migration warning stays quiet.
const reportEvery = 24 * time.Hour

var now = time.Now

type reported struct {
	Sig string    `json:"sig"`
	At  time.Time `json:"at"`
}

// markerPath sits outside the config and state dirs: the migration may be
// failing precisely because those parents cannot be written.
func markerPath(home string) string {
	return filepath.Join(home, ".cache", "bilgie", "migration-warned.json")
}

func readMarker(home string) map[string]reported {
	m := map[string]reported{}
	data, err := os.ReadFile(markerPath(home))
	if err != nil || json.Unmarshal(data, &m) != nil {
		return map[string]reported{}
	}
	return m
}

func writeMarker(home string, m map[string]reported) {
	data, err := json.Marshal(m)
	if err != nil {
		return
	}
	path := markerPath(home)
	if os.MkdirAll(filepath.Dir(path), 0o750) != nil {
		return
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "marker-*")
	if err != nil {
		return
	}
	_, werr := tmp.Write(data)
	cerr := tmp.Close()
	if werr != nil || cerr != nil || os.Rename(tmp.Name(), path) != nil {
		_ = os.Remove(tmp.Name())
	}
}

// ShouldReport says whether the failure identified by key and sig deserves a
// message now: the first time, when sig changes, and then once a day. It
// records the answer, so a scheduled tick does not repeat a persistent
// failure on every run. An unwritable marker means the failure is
// reported each time rather than hidden.
func ShouldReport(home, key, sig string) bool {
	unlock, ok := lockMarker(home)
	if !ok {
		return false
	}
	defer unlock()
	m := readMarker(home)
	if prev, ok := m[key]; ok && prev.Sig == sig && now().Sub(prev.At) < reportEvery && !prev.At.After(now()) {
		return false
	}
	m[key] = reported{Sig: sig, At: now()}
	writeMarker(home, m)
	return true
}

// lockStale is how long a lock file may sit before a crashed holder is assumed.
const lockStale = 10 * time.Second

// lockMarker serializes the read-decide-write of the marker. ok is false when
// another run holds the lock: it is deciding right now, so it reports for both.
// A marker dir that cannot be created yields a no-op lock, so the failure is
// reported each time rather than hidden.
func lockMarker(home string) (unlock func(), ok bool) {
	path := markerPath(home) + ".lock"
	if os.MkdirAll(filepath.Dir(path), 0o750) != nil {
		return func() {}, true
	}
	for range 2 {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			_ = f.Close()
			return func() { _ = os.Remove(path) }, true
		}
		if !errors.Is(err, os.ErrExist) {
			return func() {}, true
		}
		info, statErr := os.Stat(path)
		if statErr != nil || (now().Sub(info.ModTime()) >= 0 && now().Sub(info.ModTime()) < lockStale) {
			return nil, false
		}
		_ = os.Remove(path)
	}
	return nil, false
}

// Resolved forgets key once its failure is gone, so a later one is reported at once.
func Resolved(home, key string) {
	if _, ok := readMarker(home)[key]; !ok {
		return
	}
	unlock, ok := lockMarker(home)
	if !ok {
		return
	}
	defer unlock()
	m := readMarker(home)
	if _, ok := m[key]; !ok {
		return
	}
	delete(m, key)
	writeMarker(home, m)
}
