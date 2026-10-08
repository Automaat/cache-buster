package auto

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func sampleReport() Report {
	return Report{
		Tier:  TierLow,
		Start: FreeSpace{Free: 10 * gib, Total: 500 * gib},
		End:   FreeSpace{Free: 14 * gib, Total: 500 * gib},
		Results: []Result{
			{Name: "go-build", Status: StatusCleaned, Freed: 3 * gib},
			{Name: "npm", Status: StatusCleaned, Freed: gib},
			{Name: "docker", Status: StatusSkipped, Reason: "unavailable"},
			{Name: "uv", Status: StatusError, Err: errors.New("boom")},
		},
	}
}

func TestNewRunRecord_SummarizesReport(t *testing.T) {
	now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.FixedZone("x", 7200))

	rec := NewRunRecord(sampleReport(), now, nil, true)

	assert.Equal(t, now.UTC(), rec.Time)
	assert.Equal(t, "low", rec.Tier)
	assert.Equal(t, 10*gib, rec.FreeBefore)
	assert.Equal(t, 14*gib, rec.FreeAfter)
	assert.Equal(t, 4*gib, rec.FreedBytes)
	assert.True(t, rec.Notified)
	require.Len(t, rec.Providers, 4)
	assert.Equal(t, ProviderRecord{Name: "docker", Status: StatusSkipped, Reason: "unavailable"}, rec.Providers[2])
	assert.Equal(t, "boom", rec.Providers[3].Error)
}

func TestNewRunRecord_EmptyResultsEncodeAsArray(t *testing.T) {
	rec := NewRunRecord(Report{Tier: TierOK}, time.Now(), errors.New("stopped"), false)

	data, err := json.Marshal(rec)
	require.NoError(t, err)
	assert.Contains(t, string(data), `"providers":[]`)
	assert.Equal(t, "stopped", rec.Error)
}

func TestAppendRun_WritesExactlyOneValidLinePerRun(t *testing.T) {
	dir := t.TempDir()
	rec := NewRunRecord(sampleReport(), time.Now(), nil, false)

	require.NoError(t, AppendRun(dir, rec))
	require.NoError(t, AppendRun(dir, rec))

	raw, err := os.ReadFile(filepath.Join(dir, RunLogName))
	require.NoError(t, err)
	lines := strings.Split(strings.TrimSuffix(string(raw), "\n"), "\n")
	require.Len(t, lines, 2)
	for _, line := range lines {
		var got RunRecord
		require.NoError(t, json.Unmarshal([]byte(line), &got))
		assert.Equal(t, rec.FreedBytes, got.FreedBytes)
	}
	info, err := os.Stat(filepath.Join(dir, RunLogName))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}

func TestAppendRun_ConcurrentWritersNeverInterleave(t *testing.T) {
	dir := t.TempDir()
	var wg sync.WaitGroup
	for range 30 {
		wg.Go(func() {
			assert.NoError(t, AppendRun(dir, NewRunRecord(sampleReport(), time.Now(), nil, false)))
		})
	}
	wg.Wait()

	runs, corrupt, err := ReadRuns(dir, 0)
	require.NoError(t, err)
	assert.Len(t, runs, 30)
	assert.Zero(t, corrupt)
}

func TestAppendRun_ClosesUnterminatedLastLine(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, RunLogName)
	require.NoError(t, os.WriteFile(path, []byte(`{"time":"2026-10-08T10:00:00Z","tier":"ok"`), 0o600))

	require.NoError(t, AppendRun(dir, NewRunRecord(sampleReport(), time.Now(), nil, false)))

	runs, corrupt, err := ReadRuns(dir, 0)
	require.NoError(t, err)
	assert.Len(t, runs, 1, "the new record must survive the torn line before it")
	assert.Equal(t, 1, corrupt)
}

func TestAppendRun_RotatesLargeLog(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, RunLogName)
	require.NoError(t, os.WriteFile(path, make([]byte, rotateAt), 0o600))

	require.NoError(t, AppendRun(dir, NewRunRecord(sampleReport(), time.Now(), nil, false)))

	_, err := os.Stat(path + ".1")
	require.NoError(t, err)
	runs, _, err := ReadRuns(dir, 0)
	require.NoError(t, err)
	assert.Len(t, runs, 1)
}

func TestReadRuns(t *testing.T) {
	dir := t.TempDir()
	t0 := time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC)
	for i := range 5 {
		require.NoError(t, AppendRun(dir, NewRunRecord(sampleReport(), t0.Add(time.Duration(i)*time.Hour), nil, false)))
	}
	f, err := os.OpenFile(filepath.Join(dir, RunLogName), os.O_APPEND|os.O_WRONLY, 0o600)
	require.NoError(t, err)
	_, err = f.WriteString("not json\n{}\n\n")
	require.NoError(t, err)
	require.NoError(t, f.Close())

	runs, corrupt, err := ReadRuns(dir, 3)
	require.NoError(t, err)
	assert.Equal(t, 2, corrupt, "garbage and a record without a time are both unreadable")
	require.Len(t, runs, 5-2)
	assert.Equal(t, t0.Add(4*time.Hour), runs[2].Time, "newest last")

	all, _, err := ReadRuns(dir, 0)
	require.NoError(t, err)
	assert.Len(t, all, 5)
}

func TestReadRuns_MissingLogIsEmpty(t *testing.T) {
	runs, corrupt, err := ReadRuns(filepath.Join(t.TempDir(), "nope"), 5)

	require.NoError(t, err)
	assert.Empty(t, runs)
	assert.Zero(t, corrupt)
}
