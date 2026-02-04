package cli

import (
	"fmt"
	"testing"

	"github.com/Automaat/cache-buster/internal/config"
	"github.com/Automaat/cache-buster/internal/provider"
	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewModel(t *testing.T) {
	cfg := &config.Config{
		Providers: map[string]config.Provider{
			"test": {Enabled: true, Paths: []string{"/tmp/test"}, MaxSize: "1G"},
		},
	}

	m := newModel(cfg, []string{"test"}, false, false, nil)

	assert.Equal(t, stateSelection, m.state)
	assert.Len(t, m.providers, 1)
	assert.Equal(t, "test", m.providers[0].name)
	assert.Empty(t, m.selected)
	assert.False(t, m.dryRun)
	assert.False(t, m.smartMode)
}

func TestModelSelectionKeyBindings(t *testing.T) {
	cfg := &config.Config{
		Providers: map[string]config.Provider{
			"p1": {Enabled: true, Paths: []string{"/tmp/p1"}, MaxSize: "1G"},
			"p2": {Enabled: true, Paths: []string{"/tmp/p2"}, MaxSize: "1G"},
			"p3": {Enabled: true, Paths: []string{"/tmp/p3"}, MaxSize: "1G"},
		},
	}

	t.Run("cursor movement", func(t *testing.T) {
		m := newModel(cfg, []string{"p1", "p2", "p3"}, false, false, nil)
		m.providers[0].available = true
		m.providers[1].available = true
		m.providers[2].available = true

		assert.Equal(t, 0, m.cursor)

		m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
		m = m2.(model)
		assert.Equal(t, 1, m.cursor)

		m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
		m = m2.(model)
		assert.Equal(t, 2, m.cursor)

		m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
		m = m2.(model)
		assert.Equal(t, 2, m.cursor)

		m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
		m = m2.(model)
		assert.Equal(t, 1, m.cursor)

		m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyUp})
		m = m2.(model)
		assert.Equal(t, 0, m.cursor)

		m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyUp})
		m = m2.(model)
		assert.Equal(t, 0, m.cursor)
	})

	t.Run("toggle selection", func(t *testing.T) {
		m := newModel(cfg, []string{"p1", "p2"}, false, false, nil)
		m.providers[0].available = true
		m.providers[1].available = true

		m2, _ := m.Update(tea.KeyMsg{Type: tea.KeySpace})
		m = m2.(model)
		assert.Contains(t, m.selected, 0)

		m2, _ = m.Update(tea.KeyMsg{Type: tea.KeySpace})
		m = m2.(model)
		assert.NotContains(t, m.selected, 0)
	})

	t.Run("select all", func(t *testing.T) {
		m := newModel(cfg, []string{"p1", "p2", "p3"}, false, false, nil)
		m.providers[0].available = true
		m.providers[1].available = false
		m.providers[2].available = true

		m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
		m = m2.(model)

		assert.Contains(t, m.selected, 0)
		assert.NotContains(t, m.selected, 1)
		assert.Contains(t, m.selected, 2)
	})

	t.Run("deselect all", func(t *testing.T) {
		m := newModel(cfg, []string{"p1", "p2"}, false, false, nil)
		m.providers[0].available = true
		m.providers[1].available = true
		m.selected[0] = struct{}{}
		m.selected[1] = struct{}{}

		m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
		m = m2.(model)

		assert.Empty(t, m.selected)
	})

	t.Run("select over-limit only", func(t *testing.T) {
		m := newModel(cfg, []string{"p1", "p2", "p3"}, false, false, nil)
		m.providers[0].available = true
		m.providers[0].overLimit = true
		m.providers[1].available = true
		m.providers[1].overLimit = false
		m.providers[2].available = true
		m.providers[2].overLimit = true

		m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'o'}})
		m = m2.(model)

		assert.Contains(t, m.selected, 0)
		assert.NotContains(t, m.selected, 1)
		assert.Contains(t, m.selected, 2)
	})

	t.Run("cannot toggle unavailable", func(t *testing.T) {
		m := newModel(cfg, []string{"p1"}, false, false, nil)
		m.providers[0].available = false

		m2, _ := m.Update(tea.KeyMsg{Type: tea.KeySpace})
		m = m2.(model)

		assert.Empty(t, m.selected)
	})
}

func TestModelStateTransitions(t *testing.T) {
	cfg := &config.Config{
		Providers: map[string]config.Provider{
			"p1": {Enabled: true, Paths: []string{"/tmp/p1"}, MaxSize: "1G"},
		},
	}

	t.Run("selection to confirmation", func(t *testing.T) {
		m := newModel(cfg, []string{"p1"}, false, false, nil)
		m.providers[0].available = true
		m.selected[0] = struct{}{}

		m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
		m = m2.(model)

		assert.Equal(t, stateConfirmation, m.state)
	})

	t.Run("no selection prevents confirmation", func(t *testing.T) {
		m := newModel(cfg, []string{"p1"}, false, false, nil)
		m.providers[0].available = true

		m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
		m = m2.(model)

		assert.Equal(t, stateSelection, m.state)
	})

	t.Run("confirmation back to selection", func(t *testing.T) {
		m := newModel(cfg, []string{"p1"}, false, false, nil)
		m.state = stateConfirmation

		m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
		m = m2.(model)

		assert.Equal(t, stateSelection, m.state)
	})

	t.Run("confirmation with esc", func(t *testing.T) {
		m := newModel(cfg, []string{"p1"}, false, false, nil)
		m.state = stateConfirmation

		m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
		m = m2.(model)

		assert.Equal(t, stateSelection, m.state)
	})
}

func TestModelQuit(t *testing.T) {
	cfg := &config.Config{
		Providers: map[string]config.Provider{
			"p1": {Enabled: true, Paths: []string{"/tmp/p1"}, MaxSize: "1G"},
		},
	}

	t.Run("quit from selection", func(t *testing.T) {
		m := newModel(cfg, []string{"p1"}, false, false, nil)

		m2, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
		m = m2.(model)

		assert.True(t, m.quitting)
		require.NotNil(t, cmd)
	})

	t.Run("esc from selection", func(t *testing.T) {
		m := newModel(cfg, []string{"p1"}, false, false, nil)

		m2, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
		m = m2.(model)

		assert.True(t, m.quitting)
		require.NotNil(t, cmd)
	})

	t.Run("quit from done", func(t *testing.T) {
		m := newModel(cfg, []string{"p1"}, false, false, nil)
		m.state = stateDone

		m2, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
		m = m2.(model)

		assert.True(t, m.quitting)
		require.NotNil(t, cmd)
	})
}

func TestModelView(t *testing.T) {
	cfg := &config.Config{
		Providers: map[string]config.Provider{
			"p1": {Enabled: true, Paths: []string{"/tmp/p1"}, MaxSize: "1G"},
		},
	}

	t.Run("selection view contains title", func(t *testing.T) {
		m := newModel(cfg, []string{"p1"}, false, false, nil)
		view := m.View()

		assert.Contains(t, view, "Cache Buster")
		assert.Contains(t, view, "Select providers to clean")
	})

	t.Run("confirmation view shows provider count", func(t *testing.T) {
		m := newModel(cfg, []string{"p1"}, false, false, nil)
		m.state = stateConfirmation
		m.selected[0] = struct{}{}
		view := m.View()

		assert.Contains(t, view, "Clean 1 provider")
		assert.Contains(t, view, "y=confirm")
	})

	t.Run("done view shows results", func(t *testing.T) {
		m := newModel(cfg, []string{"p1"}, false, false, nil)
		m.state = stateDone
		m.selected[0] = struct{}{}
		m.totalFreed = 1024 * 1024 * 100
		view := m.View()

		assert.Contains(t, view, "Cleaned 1 provider")
		assert.Contains(t, view, "Press enter to exit")
	})

	t.Run("quitting returns empty", func(t *testing.T) {
		m := newModel(cfg, []string{"p1"}, false, false, nil)
		m.quitting = true
		view := m.View()

		assert.Empty(t, view)
	})
}

func TestSelectedNames(t *testing.T) {
	cfg := &config.Config{
		Providers: map[string]config.Provider{
			"p1": {Enabled: true, Paths: []string{"/tmp/p1"}, MaxSize: "1G"},
			"p2": {Enabled: true, Paths: []string{"/tmp/p2"}, MaxSize: "1G"},
			"p3": {Enabled: true, Paths: []string{"/tmp/p3"}, MaxSize: "1G"},
		},
	}

	m := newModel(cfg, []string{"p1", "p2", "p3"}, false, false, nil)
	m.selected[0] = struct{}{}
	m.selected[2] = struct{}{}

	names := m.selectedNames()

	assert.Len(t, names, 2)
	assert.Contains(t, names, "p1")
	assert.Contains(t, names, "p3")
	assert.NotContains(t, names, "p2")
}

func TestModelDryRunAndSmart(t *testing.T) {
	cfg := &config.Config{
		Providers: map[string]config.Provider{
			"p1": {Enabled: true, Paths: []string{"/tmp/p1"}, MaxSize: "1G"},
		},
	}

	t.Run("dry-run mode", func(t *testing.T) {
		m := newModel(cfg, []string{"p1"}, true, false, nil)
		assert.True(t, m.dryRun)
		assert.False(t, m.smartMode)
	})

	t.Run("smart mode", func(t *testing.T) {
		m := newModel(cfg, []string{"p1"}, false, true, nil)
		assert.False(t, m.dryRun)
		assert.True(t, m.smartMode)
	})

	t.Run("confirmation view shows mode", func(t *testing.T) {
		m := newModel(cfg, []string{"p1"}, true, true, nil)
		m.state = stateConfirmation
		m.selected[0] = struct{}{}
		view := m.View()

		assert.Contains(t, view, "smart")
		assert.Contains(t, view, "dry-run")
	})
}

func TestModelInit(t *testing.T) {
	cfg := &config.Config{
		Providers: map[string]config.Provider{
			"p1": {Enabled: true, Paths: []string{"/tmp/p1"}, MaxSize: "1G"},
		},
	}

	m := newModel(cfg, []string{"p1"}, false, false, nil)
	cmd := m.Init()

	assert.NotNil(t, cmd)
}

func TestModelUpdateWindowSize(t *testing.T) {
	cfg := &config.Config{
		Providers: map[string]config.Provider{
			"p1": {Enabled: true, Paths: []string{"/tmp/p1"}, MaxSize: "1G"},
		},
	}

	t.Run("normal window size", func(t *testing.T) {
		m := newModel(cfg, []string{"p1"}, false, false, nil)
		m2, cmd := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
		m = m2.(model)

		assert.Equal(t, 120, m.width)
		assert.Equal(t, 40, m.height)
		assert.Equal(t, 110, m.progress.Width)
		assert.Nil(t, cmd)
	})

	t.Run("small window clamps progress width", func(t *testing.T) {
		m := newModel(cfg, []string{"p1"}, false, false, nil)
		m2, cmd := m.Update(tea.WindowSizeMsg{Width: 5, Height: 10})
		m = m2.(model)

		assert.Equal(t, 5, m.width)
		assert.Equal(t, 10, m.height)
		assert.Equal(t, 1, m.progress.Width) // clamped to 1
		assert.Nil(t, cmd)
	})

	t.Run("zero width clamps progress width", func(t *testing.T) {
		m := newModel(cfg, []string{"p1"}, false, false, nil)
		m2, _ := m.Update(tea.WindowSizeMsg{Width: 0, Height: 10})
		m = m2.(model)

		assert.Equal(t, 1, m.progress.Width) // clamped to 1
	})
}

func TestModelUpdateScanResult(t *testing.T) {
	cfg := &config.Config{
		Providers: map[string]config.Provider{
			"p1": {Enabled: true, Paths: []string{"/tmp/p1"}, MaxSize: "1G"},
		},
	}

	m := newModel(cfg, []string{"p1"}, false, false, nil)
	item := providerItem{
		name:       "p1",
		current:    1024,
		currentFmt: "1 KiB",
		max:        1024 * 1024,
		maxFmt:     "1 MiB",
		available:  true,
	}

	m2, cmd := m.Update(scanResultMsg{idx: 0, item: item})
	m = m2.(model)

	assert.Equal(t, "p1", m.providers[0].name)
	assert.Equal(t, int64(1024), m.providers[0].current)
	assert.True(t, m.providers[0].available)
	assert.Nil(t, cmd)
}

func TestModelUpdateCleanResult(t *testing.T) {
	cfg := &config.Config{
		Providers: map[string]config.Provider{
			"p1": {Enabled: true, Paths: []string{"/tmp/p1"}, MaxSize: "1G"},
		},
	}

	t.Run("successful clean", func(t *testing.T) {
		m := newModel(cfg, []string{"p1"}, false, false, nil)
		m.state = stateCleaning
		m.selected[0] = struct{}{}
		m.cleanIdx = 0

		result := provider.CleanResult{BytesCleaned: 1024}
		m2, _ := m.Update(cleanResultMsg{idx: 0, result: result, err: nil})
		m = m2.(model)

		assert.Equal(t, int64(1024), m.totalFreed)
		assert.NotNil(t, m.providers[0].cleanResult)
		assert.Nil(t, m.providers[0].cleanErr)
		assert.Equal(t, stateDone, m.state)
	})

	t.Run("clean with error", func(t *testing.T) {
		m := newModel(cfg, []string{"p1"}, false, false, nil)
		m.state = stateCleaning
		m.selected[0] = struct{}{}
		m.cleanIdx = 0

		result := provider.CleanResult{}
		err := fmt.Errorf("clean failed")
		m2, _ := m.Update(cleanResultMsg{idx: 0, result: result, err: err})
		m = m2.(model)

		assert.Equal(t, int64(0), m.totalFreed)
		assert.NotNil(t, m.providers[0].cleanErr)
	})
}

func TestModelCleanResultPartialFailures(t *testing.T) {
	cfg := &config.Config{
		Providers: map[string]config.Provider{
			"p1": {Enabled: true, Paths: []string{"/tmp/p1"}, MaxSize: "1G"},
			"p2": {Enabled: true, Paths: []string{"/tmp/p2"}, MaxSize: "1G"},
			"p3": {Enabled: true, Paths: []string{"/tmp/p3"}, MaxSize: "1G"},
		},
	}

	m := newModel(cfg, []string{"p1", "p2", "p3"}, false, false, nil)
	m.state = stateCleaning
	m.selected[0] = struct{}{}
	m.selected[1] = struct{}{}
	m.selected[2] = struct{}{}
	m.cleanIdx = -1

	// Start cleaning - advances to p1
	m2, _ := m.cleanNext()
	m = m2.(model)
	assert.Equal(t, 0, m.cleanIdx)

	// p1 succeeds with 1024 bytes
	m2, _ = m.Update(cleanResultMsg{idx: 0, result: provider.CleanResult{BytesCleaned: 1024}, err: nil})
	m = m2.(model)
	assert.Equal(t, int64(1024), m.totalFreed)
	assert.Equal(t, 1, m.cleanIdx) // advanced to p2

	// p2 fails
	m2, _ = m.Update(cleanResultMsg{idx: 1, result: provider.CleanResult{}, err: fmt.Errorf("disk error")})
	m = m2.(model)
	assert.Equal(t, int64(1024), m.totalFreed) // unchanged
	assert.NotNil(t, m.providers[1].cleanErr)
	assert.Equal(t, 2, m.cleanIdx) // advanced to p3

	// p3 succeeds with 2048 bytes
	m2, _ = m.Update(cleanResultMsg{idx: 2, result: provider.CleanResult{BytesCleaned: 2048}, err: nil})
	m = m2.(model)
	assert.Equal(t, int64(3072), m.totalFreed) // 1024 + 2048
	assert.Equal(t, stateDone, m.state)

	// Verify final state
	assert.Nil(t, m.providers[0].cleanErr)
	assert.NotNil(t, m.providers[1].cleanErr)
	assert.Nil(t, m.providers[2].cleanErr)
}

func TestModelUpdateSpinnerTick(t *testing.T) {
	cfg := &config.Config{
		Providers: map[string]config.Provider{
			"p1": {Enabled: true, Paths: []string{"/tmp/p1"}, MaxSize: "1G"},
		},
	}

	m := newModel(cfg, []string{"p1"}, false, false, nil)
	_, cmd := m.Update(spinner.TickMsg{})

	assert.NotNil(t, cmd)
}

func TestModelUpdateProgressFrame(t *testing.T) {
	cfg := &config.Config{
		Providers: map[string]config.Provider{
			"p1": {Enabled: true, Paths: []string{"/tmp/p1"}, MaxSize: "1G"},
		},
	}

	m := newModel(cfg, []string{"p1"}, false, false, nil)
	m2, _ := m.Update(progress.FrameMsg{})
	_ = m2.(model)
}

func TestModelHandleKeyInCleaningState(t *testing.T) {
	cfg := &config.Config{
		Providers: map[string]config.Provider{
			"p1": {Enabled: true, Paths: []string{"/tmp/p1"}, MaxSize: "1G"},
		},
	}

	m := newModel(cfg, []string{"p1"}, false, false, nil)
	m.state = stateCleaning

	m2, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	m = m2.(model)

	assert.Equal(t, stateCleaning, m.state)
	assert.Nil(t, cmd)
}

func TestModelConfirmationModeToggle(t *testing.T) {
	cfg := &config.Config{
		Providers: map[string]config.Provider{
			"p1": {Enabled: true, Paths: []string{"/tmp/p1"}, MaxSize: "1G"},
		},
	}

	t.Run("switch to full mode", func(t *testing.T) {
		m := newModel(cfg, []string{"p1"}, false, true, nil)
		m.state = stateConfirmation

		m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'f'}})
		m = m2.(model)

		assert.False(t, m.smartMode)
	})

	t.Run("switch to smart mode", func(t *testing.T) {
		m := newModel(cfg, []string{"p1"}, false, false, nil)
		m.state = stateConfirmation

		m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
		m = m2.(model)

		assert.True(t, m.smartMode)
	})

	t.Run("uppercase F works", func(t *testing.T) {
		m := newModel(cfg, []string{"p1"}, false, true, nil)
		m.state = stateConfirmation

		m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'F'}})
		m = m2.(model)

		assert.False(t, m.smartMode)
	})

	t.Run("uppercase S works", func(t *testing.T) {
		m := newModel(cfg, []string{"p1"}, false, false, nil)
		m.state = stateConfirmation

		m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'S'}})
		m = m2.(model)

		assert.True(t, m.smartMode)
	})
}

func TestModelConfirmationStartsCleaning(t *testing.T) {
	cfg := &config.Config{
		Providers: map[string]config.Provider{
			"p1": {Enabled: true, Paths: []string{"/tmp/p1"}, MaxSize: "1G"},
		},
	}

	m := newModel(cfg, []string{"p1"}, false, false, nil)
	m.state = stateConfirmation
	m.selected[0] = struct{}{}
	m.providers[0].available = true

	m2, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	m = m2.(model)

	assert.Equal(t, stateCleaning, m.state)
	assert.NotNil(t, cmd)
}

func TestModelCleanNext(t *testing.T) {
	cfg := &config.Config{
		Providers: map[string]config.Provider{
			"p1": {Enabled: true, Paths: []string{"/tmp/p1"}, MaxSize: "1G"},
			"p2": {Enabled: true, Paths: []string{"/tmp/p2"}, MaxSize: "1G"},
			"p3": {Enabled: true, Paths: []string{"/tmp/p3"}, MaxSize: "1G"},
		},
	}

	t.Run("skips unselected providers", func(t *testing.T) {
		m := newModel(cfg, []string{"p1", "p2", "p3"}, false, false, nil)
		m.state = stateCleaning
		m.selected[0] = struct{}{}
		m.selected[2] = struct{}{}
		m.cleanIdx = -1

		m2, _ := m.cleanNext()
		m = m2.(model)

		assert.Equal(t, 0, m.cleanIdx)

		m.providers[0].cleanResult = &provider.CleanResult{}
		m2, _ = m.cleanNext()
		m = m2.(model)

		assert.Equal(t, 2, m.cleanIdx)
	})

	t.Run("transitions to done when complete", func(t *testing.T) {
		m := newModel(cfg, []string{"p1"}, false, false, nil)
		m.state = stateCleaning
		m.selected[0] = struct{}{}
		m.cleanIdx = 0
		m.providers[0].cleanResult = &provider.CleanResult{}

		m2, cmd := m.cleanNext()
		m = m2.(model)

		assert.Equal(t, stateDone, m.state)
		assert.Nil(t, cmd)
	})
}

func TestViewSelection(t *testing.T) {
	cfg := &config.Config{
		Providers: map[string]config.Provider{
			"p1": {Enabled: true, Paths: []string{"/tmp/p1"}, MaxSize: "1G"},
			"p2": {Enabled: true, Paths: []string{"/tmp/p2"}, MaxSize: "1G"},
			"p3": {Enabled: true, Paths: []string{"/tmp/p3"}, MaxSize: "1G"},
		},
	}

	t.Run("shows unavailable provider with error", func(t *testing.T) {
		m := newModel(cfg, []string{"p1"}, false, false, nil)
		m.providers[0].available = false
		m.providers[0].errMsg = "unavailable" // as set by scanProviderCmd
		view := m.viewSelection()

		assert.Contains(t, view, "unavailable")
	})

	t.Run("shows error message", func(t *testing.T) {
		m := newModel(cfg, []string{"p1"}, false, false, nil)
		m.providers[0].available = true
		m.providers[0].errMsg = "load error"
		view := m.viewSelection()

		assert.Contains(t, view, "load error")
	})

	t.Run("shows loading spinner when no size yet", func(t *testing.T) {
		m := newModel(cfg, []string{"p1"}, false, false, nil)
		m.providers[0].available = true
		m.providers[0].currentFmt = ""
		view := m.viewSelection()

		assert.Contains(t, view, "p1")
	})

	t.Run("shows over limit status", func(t *testing.T) {
		m := newModel(cfg, []string{"p1"}, false, false, nil)
		m.providers[0].available = true
		m.providers[0].currentFmt = "2 GiB"
		m.providers[0].maxFmt = "1 GiB"
		m.providers[0].overLimit = true
		view := m.viewSelection()

		assert.Contains(t, view, "OVER")
	})

	t.Run("shows ok status", func(t *testing.T) {
		m := newModel(cfg, []string{"p1"}, false, false, nil)
		m.providers[0].available = true
		m.providers[0].currentFmt = "500 MiB"
		m.providers[0].maxFmt = "1 GiB"
		m.providers[0].overLimit = false
		view := m.viewSelection()

		assert.Contains(t, view, "ok")
	})

	t.Run("shows selected count", func(t *testing.T) {
		m := newModel(cfg, []string{"p1", "p2", "p3"}, false, false, nil)
		m.selected[0] = struct{}{}
		m.selected[1] = struct{}{}
		view := m.viewSelection()

		assert.Contains(t, view, "Selected: 2")
	})

	t.Run("shows cursor position", func(t *testing.T) {
		m := newModel(cfg, []string{"p1", "p2"}, false, false, nil)
		m.cursor = 1
		view := m.viewSelection()

		assert.Contains(t, view, ">")
	})

	t.Run("shows checkbox state", func(t *testing.T) {
		m := newModel(cfg, []string{"p1"}, false, false, nil)
		m.selected[0] = struct{}{}
		view := m.viewSelection()

		assert.Contains(t, view, "[x]")
	})
}

func TestViewCleaning(t *testing.T) {
	cfg := &config.Config{
		Providers: map[string]config.Provider{
			"p1": {Enabled: true, Paths: []string{"/tmp/p1"}, MaxSize: "1G"},
			"p2": {Enabled: true, Paths: []string{"/tmp/p2"}, MaxSize: "1G"},
		},
	}

	t.Run("shows current provider being cleaned", func(t *testing.T) {
		m := newModel(cfg, []string{"p1", "p2"}, false, false, nil)
		m.state = stateCleaning
		m.cleanIdx = 0
		m.providers[0].name = "p1"
		view := m.viewCleaning()

		assert.Contains(t, view, "Cleaning p1")
	})

	t.Run("shows completed providers", func(t *testing.T) {
		m := newModel(cfg, []string{"p1", "p2"}, false, false, nil)
		m.state = stateCleaning
		m.selected[0] = struct{}{}
		m.selected[1] = struct{}{}
		m.cleanIdx = 1
		m.providers[0].cleanResult = &provider.CleanResult{BytesCleaned: 1024 * 1024}
		view := m.viewCleaning()

		assert.Contains(t, view, "Completed")
		assert.Contains(t, view, "freed")
	})

	t.Run("shows error for failed clean", func(t *testing.T) {
		m := newModel(cfg, []string{"p1"}, false, false, nil)
		m.state = stateCleaning
		m.selected[0] = struct{}{}
		m.cleanIdx = 0
		m.providers[0].cleanResult = &provider.CleanResult{}
		m.providers[0].cleanErr = fmt.Errorf("failed")
		view := m.viewCleaning()

		assert.Contains(t, view, "error")
	})
}

func TestViewDone(t *testing.T) {
	cfg := &config.Config{
		Providers: map[string]config.Provider{
			"p1": {Enabled: true, Paths: []string{"/tmp/p1"}, MaxSize: "1G"},
			"p2": {Enabled: true, Paths: []string{"/tmp/p2"}, MaxSize: "1G"},
		},
	}

	t.Run("shows total freed", func(t *testing.T) {
		m := newModel(cfg, []string{"p1"}, false, false, nil)
		m.state = stateDone
		m.selected[0] = struct{}{}
		m.totalFreed = 1024 * 1024 * 100
		m.providers[0].cleanResult = &provider.CleanResult{BytesCleaned: 1024 * 1024 * 100}
		view := m.viewDone()

		assert.Contains(t, view, "Cleaned 1 provider")
		assert.Contains(t, view, "freed")
	})

	t.Run("shows dry-run message", func(t *testing.T) {
		m := newModel(cfg, []string{"p1"}, true, false, nil)
		m.state = stateDone
		m.selected[0] = struct{}{}
		view := m.viewDone()

		assert.Contains(t, view, "dry-run")
		assert.Contains(t, view, "Would clean")
	})

	t.Run("shows errors section", func(t *testing.T) {
		m := newModel(cfg, []string{"p1"}, false, false, nil)
		m.state = stateDone
		m.selected[0] = struct{}{}
		m.providers[0].cleanResult = &provider.CleanResult{}
		m.providers[0].cleanErr = fmt.Errorf("clean failed")
		view := m.viewDone()

		assert.Contains(t, view, "Errors")
		assert.Contains(t, view, "clean failed")
	})

	t.Run("shows success checkmark", func(t *testing.T) {
		m := newModel(cfg, []string{"p1"}, false, false, nil)
		m.state = stateDone
		m.selected[0] = struct{}{}
		m.providers[0].cleanResult = &provider.CleanResult{BytesCleaned: 1024}
		view := m.viewDone()

		assert.Contains(t, view, "✓")
	})

	t.Run("shows failure mark", func(t *testing.T) {
		m := newModel(cfg, []string{"p1"}, false, false, nil)
		m.state = stateDone
		m.selected[0] = struct{}{}
		m.providers[0].cleanResult = &provider.CleanResult{}
		m.providers[0].cleanErr = fmt.Errorf("failed")
		view := m.viewDone()

		assert.Contains(t, view, "✗")
	})
}

func TestViewConfirmation(t *testing.T) {
	cfg := &config.Config{
		Providers: map[string]config.Provider{
			"p1": {Enabled: true, Paths: []string{"/tmp/p1"}, MaxSize: "1G"},
			"p2": {Enabled: true, Paths: []string{"/tmp/p2"}, MaxSize: "1G"},
		},
	}

	t.Run("shows full mode", func(t *testing.T) {
		m := newModel(cfg, []string{"p1"}, false, false, nil)
		m.state = stateConfirmation
		m.selected[0] = struct{}{}
		view := m.viewConfirmationPopup()

		assert.Contains(t, view, "[full]")
	})

	t.Run("shows smart mode", func(t *testing.T) {
		m := newModel(cfg, []string{"p1"}, false, true, nil)
		m.state = stateConfirmation
		m.selected[0] = struct{}{}
		view := m.viewConfirmationPopup()

		assert.Contains(t, view, "[smart]")
	})

	t.Run("lists selected providers", func(t *testing.T) {
		m := newModel(cfg, []string{"p1", "p2"}, false, false, nil)
		m.state = stateConfirmation
		m.selected[0] = struct{}{}
		m.selected[1] = struct{}{}
		view := m.viewConfirmationPopup()

		assert.Contains(t, view, "• p1")
		assert.Contains(t, view, "• p2")
	})

	t.Run("shows keybinding hints", func(t *testing.T) {
		m := newModel(cfg, []string{"p1"}, false, false, nil)
		m.state = stateConfirmation
		m.selected[0] = struct{}{}
		view := m.viewConfirmationPopup()

		assert.Contains(t, view, "s=smart")
		assert.Contains(t, view, "f=full")
	})
}

func TestCtrlCHandling(t *testing.T) {
	cfg := &config.Config{
		Providers: map[string]config.Provider{
			"p1": {Enabled: true, Paths: []string{"/tmp/p1"}, MaxSize: "1G"},
		},
	}

	t.Run("ctrl+c quits from selection", func(t *testing.T) {
		m := newModel(cfg, []string{"p1"}, false, false, nil)
		m.state = stateSelection

		m2, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
		m = m2.(model)

		assert.True(t, m.quitting)
		require.NotNil(t, cmd)
	})

	t.Run("ctrl+c goes back from confirmation", func(t *testing.T) {
		m := newModel(cfg, []string{"p1"}, false, false, nil)
		m.state = stateConfirmation

		m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
		m = m2.(model)

		assert.Equal(t, stateSelection, m.state)
	})

	t.Run("ctrl+c quits from done", func(t *testing.T) {
		m := newModel(cfg, []string{"p1"}, false, false, nil)
		m.state = stateDone

		m2, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
		m = m2.(model)

		assert.True(t, m.quitting)
		require.NotNil(t, cmd)
	})
}
