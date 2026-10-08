package cli

import (
	"context"
	"fmt"

	"github.com/smykla-skalski/bilgie/internal/auto"
	"github.com/smykla-skalski/bilgie/internal/config"
	"github.com/smykla-skalski/bilgie/pkg/size"
	"github.com/spf13/cobra"
)

// TickCmd is the scheduled entry point: a cheap free-space check that starts
// a full pass only when the free-space level, the forecast or the routine
// interval calls for one.
var TickCmd = &cobra.Command{
	Use:   "tick",
	Short: "Check free space and start a cleanup pass when it is needed (for the scheduler)",
	Long: `Reads free space with one statfs call and decides, without walking any directory:

  healthy    nothing to do, unless the routine pass is due (auto.interval)
  low        a full pass, at most every auto.low_cooldown
  critical   a full pass, at most every auto.critical_cooldown
  emergency  the same, with the stale-directory sweeps first
  falling    a pass when the median recent rate of change projects the low threshold
             inside auto.forecast

Thresholds ease with hysteresis so space hovering at a limit does not flap. A tick exits at once
when another pass holds the run lock, and never deletes while the first-run dry-run is pending.
A healthy tick prints nothing; --verbose explains the decision.`,
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE:         runTick,
}

func init() {
	TickCmd.Flags().Bool("dry-run", false, "Preview without deleting")
	TickCmd.Flags().Bool("verbose", false, "Explain the decision and list every entry")
	TickCmd.Flags().String("assume-free", "", "Pretend this much space is free (e.g. 3G) to exercise the tiers; implies --dry-run")
	_ = TickCmd.Flags().MarkHidden("assume-free")
}

func runTick(cmd *cobra.Command, _ []string) error {
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	assume, _ := cmd.Flags().GetString("assume-free")
	verbose, _ := cmd.Flags().GetBool("verbose")

	env, err := defaultAutoEnv()
	if err != nil {
		return err
	}
	env.verbose = verbose
	if assume != "" {
		free, err := assumedFree(assume, env.free)
		if err != nil {
			return err
		}
		env.free = free
		dryRun = true
	}

	ctx, stop := interruptContext()
	defer stop()
	return runTickWithLoader(ctx, config.NewLoader(), env, dryRun)
}

func runTickWithLoader(ctx context.Context, loader *config.Loader, env autoEnv, dryRun bool) error {
	cfg, err := loadValidConfig(loader)
	if err != nil {
		return err
	}
	limits, err := cfg.Auto.Limits()
	if err != nil {
		return err
	}

	fs, err := env.free()
	if err != nil {
		return fmt.Errorf("read free space: %w", err)
	}
	now := env.clock()

	tick, err := auto.ReadTickState(env.stateDir)
	if err != nil {
		return err
	}
	pass, err := auto.ReadPassState(env.stateDir)
	if err != nil {
		return err
	}

	d := auto.Decide(auto.TickInput{Now: now, Free: fs, Limits: limits, Tick: tick, Pass: pass})
	state := auto.TickState{
		Time: now.UTC(), Tier: d.Tier.String(), Action: auto.ActionIdle, Reason: d.Reason,
		Samples: d.Samples, Free: fs.Free, Total: fs.Total,
	}
	if d.Run {
		state.Action = auto.ActionPass
	}
	if !dryRun {
		env.saveTick(state)
	}

	if !d.Run {
		env.explainf("tick: free %s of %s, %s", size.FormatSize(fs.Free), size.FormatSize(fs.Total), d.Reason)
		return nil
	}

	lock, ok, err := auto.AcquireRunLock(env.stateDir)
	if err != nil {
		return err
	}
	if !ok {
		state.Action, state.Reason = auto.ActionSkip, "another pass holds the run lock"
		if !dryRun {
			env.saveTick(state)
		}
		env.explainf("tick: %s", state.Reason)
		return nil
	}
	defer lock.Release()

	env.explainf("tick: free %s of %s, %s", size.FormatSize(fs.Free), size.FormatSize(fs.Total), d.Reason)
	return env.pass(ctx, cfg, dryRun, passOptions{minTier: d.Tier, predicted: d.Predicted, preview: dryRun})
}

func (e autoEnv) saveTick(state auto.TickState) {
	if err := auto.WriteTickState(e.stateDir, state); err != nil {
		fmt.Fprintf(e.out, "warning: record tick: %v\n", err)
	}
}

func (e autoEnv) explainf(format string, args ...any) {
	if e.verbose {
		fmt.Fprintf(e.out, format+"\n", args...)
	}
}
