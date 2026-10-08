package auto

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"
	"unicode/utf16"

	"github.com/Automaat/cache-buster/internal/config"
	"github.com/Automaat/cache-buster/internal/provider"
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

	if runtime.GOOS == "darwin" || runtime.GOOS == "linux" || runtime.GOOS == "windows" {
		require.NotNil(t, notify)
		return
	}
	assert.Nil(t, notify)
}

func TestNotifierFor_RunsTheNativeToolPerOS(t *testing.T) {
	hostile := `x" & (do shell script "touch evil") & "; $(touch evil) '`
	tests := []struct {
		goos     string
		wantName string
		check    func(t *testing.T, args []string)
	}{
		{goosDarwin, "osascript", func(t *testing.T, args []string) {
			t.Helper()
			assert.Equal(t, []string{hostile, "title"}, args[len(args)-2:])
		}},
		{goosLinux, "notify-send", func(t *testing.T, args []string) {
			t.Helper()
			assert.Equal(t, []string{"--app-name=cache-buster", "--", "title", hostile}, args)
		}},
		{goosWindows, "powershell.exe", func(t *testing.T, args []string) {
			t.Helper()
			require.Equal(t, "-EncodedCommand", args[len(args)-2])
			assert.NotContains(t, strings.Join(args, " "), "evil", "text must not appear as script source")
			script := decodePowerShell(t, args[len(args)-1])
			assert.NotContains(t, script, "evil")
			assert.Contains(t, script, base64.StdEncoding.EncodeToString([]byte(hostile)))
		}},
	}
	for _, tt := range tests {
		t.Run(tt.goos, func(t *testing.T) {
			var gotName string
			var gotArgs []string
			notify := NotifierFor(tt.goos, func(_ context.Context, name string, args ...string) ([]byte, error) {
				gotName, gotArgs = name, args
				return nil, nil
			})

			require.NoError(t, notify(t.Context(), "title", hostile))

			assert.Equal(t, tt.wantName, gotName)
			tt.check(t, gotArgs)
		})
	}
	assert.Nil(t, NotifierFor("plan9", nil))
}

func decodePowerShell(t *testing.T, encoded string) string {
	t.Helper()
	raw, err := base64.StdEncoding.DecodeString(encoded)
	require.NoError(t, err)
	require.Zero(t, len(raw)%2)
	units := make([]uint16, len(raw)/2)
	for i := range units {
		units[i] = uint16(raw[2*i]) | uint16(raw[2*i+1])<<8
	}
	return string(utf16.Decode(units))
}

func TestToastScript_Golden(t *testing.T) {
	script := toastScriptFor("Disk space is low", "12 GiB free of 500 GiB after cleanup.")

	assertGolden(t, "toast.ps1", script)
}

func TestNotifier_MissingProgramIsASkipNotAFailure(t *testing.T) {
	for _, goos := range []string{goosDarwin, goosLinux, goosWindows} {
		notify := NotifierFor(goos, func(_ context.Context, name string, _ ...string) ([]byte, error) {
			return nil, &exec.Error{Name: name, Err: exec.ErrNotFound}
		})

		err := notify(t.Context(), "t", "m")

		require.ErrorIs(t, err, ErrNotifierUnavailable, goos)
	}

	report := Report{End: FreeSpace{Free: gib, Total: 1000 * gib}}
	notify := NotifierFor(goosLinux, func(_ context.Context, name string, _ ...string) ([]byte, error) {
		return nil, &exec.Error{Name: name, Err: exec.ErrNotFound}
	})
	sent, err := NotifyIfStillLow(t.Context(), notify, report, autoCfg())
	require.ErrorIs(t, err, ErrNotifierUnavailable)
	assert.False(t, sent)
}

func TestNotifier_OtherFailuresAreNotSkips(t *testing.T) {
	notify := NotifierFor(goosLinux, func(context.Context, string, ...string) ([]byte, error) {
		return nil, errors.New("exit status 1")
	})

	err := notify(t.Context(), "t", "m")

	require.Error(t, err)
	assert.NotErrorIs(t, err, ErrNotifierUnavailable)
}

func TestNotifier_IsBoundedEvenWhenTheToolIgnoresItsContext(t *testing.T) {
	release := make(chan struct{})
	defer close(release)
	notify := notifierFor(goosLinux, func(context.Context, string, ...string) ([]byte, error) {
		<-release
		return nil, nil
	}, 20*time.Millisecond)

	start := time.Now()
	err := notify(t.Context(), "t", "m")

	require.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Less(t, time.Since(start), 5*time.Second)
}

func TestNotifyTimeoutIsAboutTenSeconds(t *testing.T) {
	assert.Equal(t, 10*time.Second, NotifyTimeout)
}

func TestRun_FailedFinalReadMarksEndStaleAndSuppressesNotification(t *testing.T) {
	reads := 0
	deps := Deps{
		Free: func() (FreeSpace, error) {
			reads++
			if reads > 1 {
				return FreeSpace{}, errors.New("statfs failed")
			}
			return FreeSpace{Free: gib, Total: 1000 * gib}, nil
		},
		NewProvider: func(string, config.Provider) (provider.Provider, error) { return nil, errors.New("none") },
		Out:         &bytes.Buffer{},
	}

	report, err := Run(t.Context(), &config.Config{Auto: autoCfg()}, false, deps)

	require.NoError(t, err)
	assert.True(t, report.EndStale)
	var sent []sentNote
	got, err := NotifyIfStillLow(t.Context(), noteRecorder(&sent, nil), report, autoCfg())
	require.NoError(t, err)
	assert.False(t, got)
	assert.Empty(t, sent)
}
