package auto

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/smykla-skalski/bilgie/internal/config"
	"github.com/smykla-skalski/bilgie/internal/provider"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type sizedProvider struct {
	provider.Provider
	size int64
}

func (s *sizedProvider) SetProtected(paths []string) {
	s.Provider.(provider.ProtectionAware).SetProtected(paths)
}

func (s *sizedProvider) CurrentSize(context.Context) (int64, error) { return s.size, nil }

func TestRun_MiseWithPluginClonesIsNotSkippedByCheckoutScan(t *testing.T) {
	h := newHarness(t)
	root := h.dir(".local", "share", "mise")
	for _, plugin := range []string{"lua", "make", "teleport-ent"} {
		require.NoError(t, os.MkdirAll(filepath.Join(root, "plugins", plugin, ".git"), 0o750))
	}
	pc := config.Provider{Enabled: true, Paths: []string{root}, MaxSize: "1G", CleanCmd: "mise prune"}
	p, err := provider.NewProvider("mise", pc)
	require.NoError(t, err)
	_, aware := p.(provider.ProtectionAware)
	require.True(t, aware, "mise enforces protection itself")

	t.Setenv("PATH", t.TempDir())
	h.cfg.Providers["mise"] = pc
	h.fakes["mise"] = &fakeProvider{calls: &h.calls, opts: &h.opts, name: "mise", paths: []string{root}, max: gib}
	h.wrap = map[string]provider.Provider{"mise": &sizedProvider{Provider: p, size: 2 * gib}}

	report, err := h.run(true, 400*gib)

	require.NoError(t, err)
	require.Len(t, report.Results, 1)
	assert.Equal(t, "unavailable", report.Results[0].Reason)
	assert.NotContains(t, h.out.String(), "checkout")
}
