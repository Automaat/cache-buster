package auto

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/Automaat/cache-buster/internal/config"
	"github.com/Automaat/cache-buster/internal/provider"
	"github.com/Automaat/cache-buster/pkg/size"
)

// VolumesProvider is never run by auto: a volume prune cannot be bounded and
// can destroy data that exists nowhere else.
const VolumesProvider = "docker-volumes"

// ArchivesProvider is never run by auto: Xcode archives hold App Store dSYMs
// and signed builds that cannot be rebuilt.
const ArchivesProvider = "xcode-archives"

// DockerCleanTimeout bounds Docker prune commands during an unattended run.
const DockerCleanTimeout = 10 * time.Minute

// Result statuses.
const (
	StatusCleaned = "cleaned"
	StatusDryRun  = "dry-run"
	StatusSkipped = "skipped"
	StatusError   = "error"
)

// Deps holds the injectable collaborators of Run.
type Deps struct {
	Free        FreeFunc
	NewProvider func(name string, cfg config.Provider) (provider.Provider, error)
	Out         io.Writer
	Home        string
}

// Result is one provider's outcome.
type Result struct {
	Err    error
	Name   string
	Status string
	Reason string
	Output string
	Freed  int64
}

// Report is the outcome of a run.
type Report struct {
	Results   []Result
	Start     FreeSpace
	End       FreeSpace
	Tier      Tier
	DryRun    bool
	Recovered bool
	EndStale  bool
}

// Previewed reports whether the run reached a pressure tier and at least one
// provider ran without error, so a first-run dry-run showed something real.
func (r Report) Previewed() bool {
	if r.Tier == TierOK {
		return false
	}
	return slices.ContainsFunc(r.Results, func(res Result) bool {
		return res.Status == StatusDryRun || res.Status == StatusCleaned
	})
}

// Err summarizes provider failures, or returns nil when there were none.
func (r Report) Err() error {
	var errs []error
	for _, res := range r.Results {
		if res.Err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", res.Name, res.Err))
		}
	}
	return errors.Join(errs...)
}

// candidate is a provider auto may run. A sweep is a disabled
// directory-pattern provider that runs only at the critical tier.
type candidate struct {
	cfg   config.Provider
	name  string
	sweep bool
}

// Run picks the pressure tier from free space and trims providers
// accordingly. It never runs docker-volumes and never touches protected paths.
func Run(ctx context.Context, cfg *config.Config, dryRun bool, deps Deps) (Report, error) {
	start, err := deps.Free()
	if err != nil {
		return Report{}, fmt.Errorf("read free space: %w", err)
	}
	tier, err := ChooseTier(start, cfg.Auto)
	if err != nil {
		return Report{}, err
	}

	report := Report{Start: start, End: start, Tier: tier, DryRun: dryRun}
	fmt.Fprintf(deps.Out, "free %s of %s: tier %s%s\n",
		size.FormatSize(start.Free), size.FormatSize(start.Total), tier, dryRunSuffix(dryRun))

	list := candidates(cfg, tier)
	protected := ProtectedPaths(cfg, deps.Home)
	for i := range list {
		c := &list[i]
		if err := ctx.Err(); err != nil {
			return report, err
		}

		current := currentTier(cfg, tier, deps, &report)
		if tier != TierOK && current == TierOK {
			report.Recovered = true
			fmt.Fprintf(deps.Out, "free space recovered: %s free, stopping\n", size.FormatSize(report.End.Free))
			break
		}
		if c.sweep && current != TierCritical {
			continue
		}

		res := runCandidate(ctx, c, tier, dryRun, deps, protected)
		report.Results = append(report.Results, res)
		printResult(deps.Out, res)
	}

	if err := ctx.Err(); err != nil {
		return report, err
	}

	if end, freeErr := deps.Free(); freeErr == nil {
		report.End = end
	} else {
		report.EndStale = true
	}
	fmt.Fprintf(deps.Out, "done: free %s\n", size.FormatSize(report.End.Free))
	return report, nil
}

// currentTier rereads free space so a run stops once enough was freed. An
// unreadable value keeps the starting tier.
func currentTier(cfg *config.Config, start Tier, deps Deps, report *Report) Tier {
	if start == TierOK {
		return start
	}
	now, err := deps.Free()
	if err != nil {
		return start
	}
	report.End = now
	tier, err := ChooseTier(now, cfg.Auto)
	if err != nil {
		return start
	}
	return tier
}

func dryRunSuffix(dryRun bool) string {
	if dryRun {
		return " (dry-run, nothing is deleted)"
	}
	return ""
}

// candidates lists the providers for a tier, cheapest to rebuild first.
// At the critical tier the stale-directory sweeps go first because they free
// the most. docker-volumes is never listed.
func candidates(cfg *config.Config, tier Tier) []candidate {
	var sweeps, regular []string
	byName := make(map[string]candidate)

	for name := range cfg.Providers {
		pc := cfg.Providers[name]
		if !cfg.Applies(name) || neverRun(name, pc) || (name != "docker" && pruneVolumes(pc.CleanCmd)) || !config.PathsExist(pc.Paths) {
			continue
		}
		switch {
		case pc.Enabled:
			byName[name] = candidate{name: name, cfg: pc}
			regular = append(regular, name)
		case pc.Type == config.TypeDirPattern && tier == TierCritical:
			byName[name] = candidate{name: name, cfg: pc, sweep: true}
			sweeps = append(sweeps, name)
		}
	}

	sortByRebuildCost(sweeps)
	sortByRebuildCost(regular)

	out := make([]candidate, 0, len(byName))
	for _, name := range append(sweeps, regular...) {
		out = append(out, byName[name])
	}
	return out
}

// neverRun lists the providers auto must not touch, whatever they are named.
func neverRun(name string, pc config.Provider) bool {
	if name == VolumesProvider || name == ArchivesProvider {
		return true
	}
	return slices.ContainsFunc(pc.Paths, isXcodeArchives)
}

// isXcodeArchives matches an Xcode/Archives pair of path elements, so a
// renamed provider on the archives folder is still excluded.
func isXcodeArchives(path string) bool {
	parts := strings.Split(filepath.ToSlash(filepath.Clean(path)), "/")
	for i := 0; i+1 < len(parts); i++ {
		if strings.EqualFold(parts[i], "Xcode") && strings.EqualFold(parts[i+1], "Archives") {
			return true
		}
	}
	return false
}

// pruneVolumes catches renamed or custom providers whose command prunes Docker volumes.
func pruneVolumes(cmd string) bool {
	lower := strings.ToLower(cmd)
	return strings.Contains(lower, "volume")
}

func runCandidate(ctx context.Context, c *candidate, tier Tier, dryRun bool, deps Deps, protected []string) Result {
	res := Result{Name: c.name}

	p, err := deps.NewProvider(c.name, c.cfg)
	if err != nil {
		res.Status, res.Err = StatusError, fmt.Errorf("load provider: %w", err)
		return res
	}

	if reason := protectedReason(p, deps.Home, protected); reason != "" {
		return skipped(res, reason)
	}
	if !availableCtx(ctx, p) {
		if err := ctx.Err(); err != nil {
			res.Status, res.Err = StatusError, err
			return res
		}
		return skipped(res, "unavailable")
	}
	if err := ctx.Err(); err != nil {
		res.Status, res.Err = StatusError, err
		return res
	}

	if tier == TierOK {
		cur, sizeErr := p.CurrentSize(ctx)
		if sizeErr != nil {
			return skipped(res, "cannot measure size: "+sizeErr.Error())
		}
		if cur <= p.MaxSize() {
			return skipped(res, "within limit")
		}
	}

	out, err := p.Clean(ctx, provider.CleanOptions{
		DryRun:  dryRun,
		Mode:    provider.CleanModeSmart,
		Timeout: DockerCleanTimeout,
	})
	res.Output = strings.TrimSpace(out.Output)
	res.Freed = out.BytesCleaned
	switch {
	case err != nil:
		res.Status, res.Err = StatusError, err
	case out.SkipReason != "":
		res.Status, res.Reason = StatusSkipped, out.SkipReason
	case dryRun:
		res.Status = StatusDryRun
	default:
		res.Status = StatusCleaned
	}
	return res
}

func skipped(res Result, reason string) Result {
	res.Status, res.Reason = StatusSkipped, reason
	return res
}

func protectedReason(p provider.Provider, home string, protected []string) string {
	for _, path := range p.Paths() {
		if isProtectedWith(path, home, protected, true) {
			return "protected path " + path
		}
	}
	return ""
}

func printResult(out io.Writer, res Result) {
	switch res.Status {
	case StatusSkipped:
		fmt.Fprintf(out, "%s: skipped (%s)\n", res.Name, res.Reason)
	case StatusError:
		fmt.Fprintf(out, "%s: error: %v\n", res.Name, res.Err)
	case StatusDryRun:
		fmt.Fprintf(out, "%s: dry-run, would free %s\n", res.Name, size.FormatSize(res.Freed))
		printPreview(out, res.Output)
	default:
		fmt.Fprintf(out, "%s: freed %s\n", res.Name, size.FormatSize(res.Freed))
	}
}

// printPreview lists what a dry-run would remove and counts the skip lines,
// since directory-pattern providers report every non-match.
func printPreview(out io.Writer, output string) {
	skips := 0
	for line := range strings.SplitSeq(output, "\n") {
		switch {
		case line == "":
		case strings.HasPrefix(line, "skip:"):
			skips++
		default:
			fmt.Fprintf(out, "  %s\n", line)
		}
	}
	if skips > 0 {
		fmt.Fprintf(out, "  %d entries skipped\n", skips)
	}
}

// availableCtx runs the provider's availability probe but returns false as
// soon as ctx is cancelled, so an interrupt does not wait for a hung probe.
func availableCtx(ctx context.Context, p provider.Provider) bool {
	done := make(chan bool, 1)
	go func() { done <- p.Available() }()
	select {
	case ok := <-done:
		return ok
	case <-ctx.Done():
		return false
	}
}
