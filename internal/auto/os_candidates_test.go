package auto

import (
	"testing"

	"github.com/smykla-skalski/bilgie/internal/config"
	"github.com/stretchr/testify/assert"
)

func candidateNames(cfg *config.Config) []string {
	var names []string
	list := candidates(cfg, TierLow)
	for i := range list {
		names = append(names, list[i].name)
	}
	return names
}

func TestCandidates_SkipProvidersThatDoNotApplyOnThisOS(t *testing.T) {
	dir := t.TempDir()
	extra := map[string]config.Provider{
		"xcode-deriveddata": {Enabled: true, Paths: []string{dir}, MaxSize: "1G"},
		"custom":            {Enabled: true, Paths: []string{dir}, MaxSize: "1G", CleanCmd: "true"},
	}
	onLinux := config.DefaultConfigFor(config.Platform{OS: config.OSLinux})
	onMac := config.DefaultConfigFor(config.Platform{OS: config.OSDarwin})
	for _, cfg := range []*config.Config{onLinux, onMac} {
		cfg.Providers = extra
	}

	assert.Equal(t, []string{"custom"}, candidateNames(onLinux))
	assert.ElementsMatch(t, []string{"custom", "xcode-deriveddata"}, candidateNames(onMac))
}
