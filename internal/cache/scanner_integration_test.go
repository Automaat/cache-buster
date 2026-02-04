package cache

import (
	"os/exec"
	"strconv"
	"strings"
	"testing"

	"github.com/Automaat/cache-buster/internal/config"
	"github.com/Automaat/cache-buster/pkg/size"
)

func TestCalculateSizeAgainstDu(t *testing.T) {
	paths, err := config.ExpandPaths([]string{"~/.npm"})
	if err != nil {
		t.Skipf("expand paths: %v", err)
	}
	if len(paths) == 0 {
		t.Skip("~/.npm not found")
	}

	result, err := CalculateSize(paths)
	if err != nil {
		t.Fatalf("CalculateSize: %v", err)
	}
	ourSize := result.Size

	out, err := exec.Command("du", "-sb", paths[0]).Output()
	if err != nil {
		t.Skipf("du command failed: %v", err)
	}

	fields := strings.Fields(string(out))
	if len(fields) < 1 {
		t.Fatal("unexpected du output")
	}

	duSize, err := strconv.ParseInt(fields[0], 10, 64)
	if err != nil {
		t.Fatalf("parse du output: %v", err)
	}

	t.Logf("CalculateSize: %d (%s)", ourSize, size.FormatSize(ourSize))
	t.Logf("du -sb:        %d (%s)", duSize, size.FormatSize(duSize))

	diff := ourSize - duSize
	if diff < 0 {
		diff = -diff
	}

	if duSize == 0 {
		if ourSize == 0 {
			t.Log("Both report 0 bytes")
			return
		}
		t.Errorf("du reports 0 bytes but CalculateSize reports %d", ourSize)
		return
	}

	pct := float64(diff) / float64(duSize) * 100
	t.Logf("difference:    %d bytes (%.2f%%)", diff, pct)

	if pct > 5 {
		t.Errorf("size difference > 5%%: ours=%d, du=%d", ourSize, duSize)
	}
}
