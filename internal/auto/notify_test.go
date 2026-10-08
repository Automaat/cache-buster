package auto

import (
	"context"
	"errors"
	"runtime"
	"testing"

	"github.com/Automaat/cache-buster/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type sentNote struct{ title, message string }

func noteRecorder(sent *[]sentNote, err error) Notifier {
	return func(_ context.Context, title, message string) error {
		*sent = append(*sent, sentNote{title, message})
		return err
	}
}

func TestNotifyIfStillLow(t *testing.T) {
	total := 1000 * gib
	tests := []struct {
		name     string
		end      int64
		dryRun   bool
		wantSent bool
	}{
		{"still under the pct floor", 100 * gib, false, true},
		{"critical", 2 * gib, false, true},
		{"recovered above both floors", 400 * gib, false, false},
		{"dry-run frees nothing so it stays quiet", 2 * gib, true, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var sent []sentNote
			report := Report{DryRun: tt.dryRun, End: FreeSpace{Free: tt.end, Total: total}}

			got, err := NotifyIfStillLow(t.Context(), noteRecorder(&sent, nil), report, autoCfg())

			require.NoError(t, err)
			assert.Equal(t, tt.wantSent, got)
			assert.Len(t, sent, map[bool]int{true: 1, false: 0}[tt.wantSent])
		})
	}
}

func TestNotifyIfStillLow_NilNotifierAndFailure(t *testing.T) {
	report := Report{End: FreeSpace{Free: gib, Total: 1000 * gib}}

	got, err := NotifyIfStillLow(t.Context(), nil, report, autoCfg())
	require.NoError(t, err)
	assert.False(t, got)

	var sent []sentNote
	got, err = NotifyIfStillLow(t.Context(), noteRecorder(&sent, errors.New("denied")), report, autoCfg())
	require.ErrorContains(t, err, "denied")
	assert.False(t, got)
}

func TestOsascriptNotifier_PassesTextAsArguments(t *testing.T) {
	var gotName string
	var gotArgs []string
	notify := OsascriptNotifier(func(_ context.Context, name string, args ...string) ([]byte, error) {
		gotName, gotArgs = name, args
		return nil, nil
	})
	hostile := `x" & (do shell script "touch evil") & "`

	require.NoError(t, notify(t.Context(), "title", hostile))

	assert.Equal(t, "osascript", gotName)
	require.Len(t, gotArgs, 8)
	assert.Equal(t, []string{hostile, "title"}, gotArgs[6:])
	for _, a := range gotArgs[:6] {
		assert.NotContains(t, a, "evil", "script text must not embed the message")
	}
}

func TestNotifyIfStillLow_UsesConfiguredFloorNotCriticalLevel(t *testing.T) {
	var sent []sentNote
	cfg := config.Auto{MinFree: "2G", MinFreePct: 0}
	report := Report{End: FreeSpace{Free: 4 * gib, Total: 1000 * gib}}

	got, err := NotifyIfStillLow(t.Context(), noteRecorder(&sent, nil), report, cfg)

	require.NoError(t, err)
	assert.False(t, got, "4 GiB is above min_free, so the user asked for no warning")
	assert.Empty(t, sent)
}

func TestDefaultNotifier_OnlyWhereSupported(t *testing.T) {
	notify := DefaultNotifier(func(context.Context, string, ...string) ([]byte, error) { return nil, nil })

	if runtime.GOOS == "darwin" {
		require.NotNil(t, notify)
		return
	}
	assert.Nil(t, notify)
}
