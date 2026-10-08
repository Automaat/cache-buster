package auto

import (
	"context"
	"errors"
	"testing"

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
	assert.Equal(t, []string{hostile, "title"}, gotArgs[len(gotArgs)-2:])
	for _, a := range gotArgs[:len(gotArgs)-2] {
		assert.NotContains(t, a, "evil", "script text must not embed the message")
	}
}
