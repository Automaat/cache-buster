package osshim

import (
	"bufio"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Memory is one reading of memory and swap, in bytes. A field the OS does
// not report stays zero. FreePercent is the macOS "system-wide memory free
// percentage", or -1 where the OS does not report it.
type Memory struct {
	SwapUsed     uint64
	SwapTotal    uint64
	MemAvailable uint64
	MemTotal     uint64
	FreePercent  int
}

// ReadMemory reads memory and swap pressure from the running OS.
func ReadMemory(ctx context.Context) (Memory, error) {
	return readMemory(ctx)
}

// ParseMeminfo reads /proc/meminfo text. Swap used is SwapTotal - SwapFree;
// MemAvailable falls back to MemFree when the kernel predates it.
func ParseMeminfo(r io.Reader) (Memory, error) {
	vals := make(map[string]uint64)
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		name, rest, ok := strings.Cut(sc.Text(), ":")
		if !ok {
			continue
		}
		fields := strings.Fields(rest)
		if len(fields) == 0 {
			continue
		}
		n, err := strconv.ParseUint(fields[0], 10, 64)
		if err != nil {
			continue
		}
		if len(fields) > 1 && strings.EqualFold(fields[1], "kB") {
			n *= 1024
		}
		vals[name] = n
	}
	if err := sc.Err(); err != nil {
		return Memory{}, err
	}
	total, ok := vals["MemTotal"]
	if !ok {
		return Memory{}, errors.New("meminfo has no MemTotal")
	}
	avail, ok := vals["MemAvailable"]
	if !ok {
		avail = vals["MemFree"]
	}
	m := Memory{MemTotal: total, MemAvailable: avail, SwapTotal: vals["SwapTotal"], FreePercent: -1}
	if free := vals["SwapFree"]; m.SwapTotal > free {
		m.SwapUsed = m.SwapTotal - free
	}
	return m, nil
}

var swapUsageField = regexp.MustCompile(`(total|used|free)\s*=\s*(\d+(?:\.\d+)?)\s*([KMGT]?)`)

// ParseSwapUsage reads `sysctl vm.swapusage` text, for example
// "vm.swapusage: total = 4096.00M  used = 2048.50M  free = 2047.50M  (encrypted)".
func ParseSwapUsage(out string) (used, total uint64, err error) {
	var gotUsed, gotTotal bool
	for _, m := range swapUsageField.FindAllStringSubmatch(out, -1) {
		v, perr := strconv.ParseFloat(m[2], 64)
		if perr != nil || v < 0 || math.IsInf(v, 0) {
			return 0, 0, fmt.Errorf("swap usage %q: bad number", m[0])
		}
		bytes := uint64(v * float64(unitBytes(m[3])))
		switch m[1] {
		case "used":
			used, gotUsed = bytes, true
		case "total":
			total, gotTotal = bytes, true
		}
	}
	if !gotUsed || !gotTotal {
		return 0, 0, fmt.Errorf("unrecognized swap usage output %q", strings.TrimSpace(out))
	}
	return used, total, nil
}

func unitBytes(unit string) uint64 {
	switch unit {
	case "K":
		return 1 << 10
	case "M":
		return 1 << 20
	case "G":
		return 1 << 30
	case "T":
		return 1 << 40
	default:
		return 1
	}
}

var freePercentLine = regexp.MustCompile(`memory free percentage:\s*(\d+)%`)

// ParseMemoryPressure reads the free percentage out of `memory_pressure`
// text.
func ParseMemoryPressure(out string) (int, error) {
	m := freePercentLine.FindStringSubmatch(out)
	if m == nil {
		return 0, errors.New("memory_pressure output has no free percentage")
	}
	pct, err := strconv.Atoi(m[1])
	if err != nil || pct > 100 {
		return 0, fmt.Errorf("memory_pressure free percentage %q out of range", m[1])
	}
	return pct, nil
}

// ParsePageFiles sums a Windows SystemPageFileInformation buffer: a chain of
// entries that each start with NextEntryOffset, TotalSize, TotalInUse and
// PeakUsage as little-endian uint32 page counts. Sizes come back in bytes.
func ParsePageFiles(buf []byte, pageSize uint64) (used, total uint64, err error) {
	const header = 16
	off := uint32(0)
	for {
		if uint64(off)+header > uint64(len(buf)) {
			return 0, 0, errors.New("page file information is truncated")
		}
		e := buf[off:]
		next := binary.LittleEndian.Uint32(e[0:4])
		total += uint64(binary.LittleEndian.Uint32(e[4:8])) * pageSize
		used += uint64(binary.LittleEndian.Uint32(e[8:12])) * pageSize
		if next == 0 {
			return used, total, nil
		}
		off += next
	}
}

// commandRunner runs a program and returns its standard output.
type commandRunner func(ctx context.Context, name string, args ...string) (string, error)

const memoryProbeTimeout = 5 * time.Second

// readMemoryWith reads swap with sysctl and the free percentage with
// memory_pressure. A missing free percentage is not an error: swap alone is
// enough to act on.
func readMemoryWith(ctx context.Context, run commandRunner) (Memory, error) {
	ctx, cancel := context.WithTimeout(ctx, memoryProbeTimeout)
	defer cancel()

	out, err := run(ctx, "sysctl", "vm.swapusage")
	if err != nil {
		return Memory{}, fmt.Errorf("sysctl vm.swapusage: %w", err)
	}
	used, total, err := ParseSwapUsage(out)
	if err != nil {
		return Memory{}, err
	}
	m := Memory{SwapUsed: used, SwapTotal: total, FreePercent: -1}
	if out, err := run(ctx, "memory_pressure"); err == nil {
		if pct, perr := ParseMemoryPressure(out); perr == nil {
			m.FreePercent = pct
		}
	}
	return m, nil
}
