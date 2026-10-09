package migrate

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/smykla-skalski/bilgie/internal/fsx"
)

const maxMarker = 1 << 20

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
	var m map[string]reported
	data, err := fsx.ReadRegular(markerPath(home), maxMarker)
	if err != nil || json.Unmarshal(data, &m) != nil || m == nil {
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

func noop() {}

// lockFile takes an advisory lock by creating path and returns its unlock. It
// never fails or blocks past wait: when the lock stays taken, or cannot be
// created at all, the caller proceeds unlocked, because the lock only keeps
// concurrent runs from announcing the same move twice. A lock older than
// lockStale, or dated in the future, belongs to a crashed holder and is
// broken. Unlock removes the file only while it still holds this run's token.
func lockFile(path string, wait time.Duration) (unlock func()) {
	if os.MkdirAll(filepath.Dir(path), 0o750) != nil {
		return noop
	}
	token := fmt.Sprintf("%d-%d", os.Getpid(), time.Now().UnixNano())
	deadline := time.Now().Add(wait)
	for {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			_, _ = f.WriteString(token)
			_ = f.Close()
			return func() { releaseLock(path, token) }
		}
		if !errors.Is(err, os.ErrExist) {
			return noop
		}
		if info, statErr := os.Lstat(path); statErr != nil || staleLock(now().Sub(info.ModTime())) {
			_ = os.Remove(path)
		}
		if !time.Now().Before(deadline) {
			return noop
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func releaseLock(path, token string) {
	if data, err := fsx.ReadRegular(path, maxMarker); err == nil && string(data) == token {
		_ = os.Remove(path)
	}
}

func staleLock(age time.Duration) bool { return age < 0 || age >= lockStale }

// Resolved forgets key once its failure is gone, so a later one is reported at once.
func Resolved(home, key string) {
	if _, ok := readMarker(home)[key]; !ok {
		return
	}
	m := readMarker(home)
	if _, ok := m[key]; !ok {
		return
	}
	delete(m, key)
	writeMarker(home, m)
}
