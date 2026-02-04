package cli

import (
	"testing"

	"github.com/Automaat/cache-buster/internal/config"
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

	m := newModel(cfg, []string{"test"}, false, false)

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
		m := newModel(cfg, []string{"p1", "p2", "p3"}, false, false)
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
		m := newModel(cfg, []string{"p1", "p2"}, false, false)
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
		m := newModel(cfg, []string{"p1", "p2", "p3"}, false, false)
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
		m := newModel(cfg, []string{"p1", "p2"}, false, false)
		m.providers[0].available = true
		m.providers[1].available = true
		m.selected[0] = struct{}{}
		m.selected[1] = struct{}{}

		m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
		m = m2.(model)

		assert.Empty(t, m.selected)
	})

	t.Run("select over-limit only", func(t *testing.T) {
		m := newModel(cfg, []string{"p1", "p2", "p3"}, false, false)
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
		m := newModel(cfg, []string{"p1"}, false, false)
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
		m := newModel(cfg, []string{"p1"}, false, false)
		m.providers[0].available = true
		m.selected[0] = struct{}{}

		m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
		m = m2.(model)

		assert.Equal(t, stateConfirmation, m.state)
	})

	t.Run("no selection prevents confirmation", func(t *testing.T) {
		m := newModel(cfg, []string{"p1"}, false, false)
		m.providers[0].available = true

		m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
		m = m2.(model)

		assert.Equal(t, stateSelection, m.state)
	})

	t.Run("confirmation back to selection", func(t *testing.T) {
		m := newModel(cfg, []string{"p1"}, false, false)
		m.state = stateConfirmation

		m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
		m = m2.(model)

		assert.Equal(t, stateSelection, m.state)
	})

	t.Run("confirmation with esc", func(t *testing.T) {
		m := newModel(cfg, []string{"p1"}, false, false)
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
		m := newModel(cfg, []string{"p1"}, false, false)

		m2, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
		m = m2.(model)

		assert.True(t, m.quitting)
		require.NotNil(t, cmd)
	})

	t.Run("esc from selection", func(t *testing.T) {
		m := newModel(cfg, []string{"p1"}, false, false)

		m2, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
		m = m2.(model)

		assert.True(t, m.quitting)
		require.NotNil(t, cmd)
	})

	t.Run("quit from done", func(t *testing.T) {
		m := newModel(cfg, []string{"p1"}, false, false)
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
		m := newModel(cfg, []string{"p1"}, false, false)
		view := m.View()

		assert.Contains(t, view, "Cache Buster")
		assert.Contains(t, view, "Select providers to clean")
	})

	t.Run("confirmation view shows provider count", func(t *testing.T) {
		m := newModel(cfg, []string{"p1"}, false, false)
		m.state = stateConfirmation
		m.selected[0] = struct{}{}
		view := m.View()

		assert.Contains(t, view, "Clean 1 provider")
		assert.Contains(t, view, "Press y to confirm")
	})

	t.Run("done view shows results", func(t *testing.T) {
		m := newModel(cfg, []string{"p1"}, false, false)
		m.state = stateDone
		m.selected[0] = struct{}{}
		m.totalFreed = 1024 * 1024 * 100
		view := m.View()

		assert.Contains(t, view, "Cleaned 1 provider")
		assert.Contains(t, view, "Press enter to exit")
	})

	t.Run("quitting returns empty", func(t *testing.T) {
		m := newModel(cfg, []string{"p1"}, false, false)
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

	m := newModel(cfg, []string{"p1", "p2", "p3"}, false, false)
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
		m := newModel(cfg, []string{"p1"}, true, false)
		assert.True(t, m.dryRun)
		assert.False(t, m.smartMode)
	})

	t.Run("smart mode", func(t *testing.T) {
		m := newModel(cfg, []string{"p1"}, false, true)
		assert.False(t, m.dryRun)
		assert.True(t, m.smartMode)
	})

	t.Run("confirmation view shows mode", func(t *testing.T) {
		m := newModel(cfg, []string{"p1"}, true, true)
		m.state = stateConfirmation
		m.selected[0] = struct{}{}
		view := m.View()

		assert.Contains(t, view, "smart")
		assert.Contains(t, view, "dry-run")
	})
}
