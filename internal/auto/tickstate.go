package auto

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// State files next to runs.jsonl. They are separate so a tick (which only
// touches the tick file) never races the pass holder (which owns the pass
// file under the run lock). Both are replaced atomically.
const (
	tickStateName = "tick.json"
	passStateName = "pass.json"
)

var errCorruptState = errors.New("corrupt state file")

// Sample is one free-space reading kept for the trend forecast.
type Sample struct {
	Time time.Time `json:"time"`
	Free int64     `json:"free_bytes"`
}

// TickState is what the previous tick left behind.
type TickState struct {
	Time    time.Time `json:"time"`
	Tier    string    `json:"tier"`
	Action  string    `json:"action"`
	Reason  string    `json:"reason"`
	Samples []Sample  `json:"samples"`
	Free    int64     `json:"free_bytes"`
	Total   int64     `json:"total_bytes"`
}

// NotifyRecord remembers the last notification of one tier.
type NotifyRecord struct {
	Time time.Time `json:"time"`
	Free int64     `json:"free_bytes"`
}

// PassState records the last full pass, whoever ran it (tick or auto), and
// the notifications sent, for the cooldowns.
type PassState struct {
	Time     time.Time               `json:"time"`
	Tier     string                  `json:"tier"`
	Notified map[string]NotifyRecord `json:"notified,omitempty"`
	DryRun   bool                    `json:"dry_run"`
}

func readJSON(stateDir, name string, v any) error {
	data, err := os.ReadFile(filepath.Join(stateDir, name))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read %s: %w", name, err)
	}
	if err := json.Unmarshal(data, v); err != nil {
		return fmt.Errorf("%w: %s: %w", errCorruptState, name, err)
	}
	return nil
}

func writeJSON(stateDir, name string, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("encode %s: %w", name, err)
	}
	return writeFileAtomic(filepath.Join(stateDir, name), data)
}

// ReadTickState loads the tick state. A missing or torn file is the empty
// state, so a bad write never wedges the agent.
func ReadTickState(stateDir string) (TickState, error) {
	var s TickState
	if err := readJSON(stateDir, tickStateName, &s); err != nil {
		if errors.Is(err, errCorruptState) {
			return TickState{}, nil
		}
		return TickState{}, err
	}
	return s, nil
}

// WriteTickState stores the tick state.
func WriteTickState(stateDir string, s TickState) error {
	return writeJSON(stateDir, tickStateName, s)
}

// ReadPassState loads the pass state, with the same tolerance as ReadTickState.
func ReadPassState(stateDir string) (PassState, error) {
	var s PassState
	if err := readJSON(stateDir, passStateName, &s); err != nil {
		if errors.Is(err, errCorruptState) {
			return PassState{}, nil
		}
		return PassState{}, err
	}
	return s, nil
}

// WritePassState stores the pass state.
func WritePassState(stateDir string, s PassState) error {
	return writeJSON(stateDir, passStateName, s)
}
