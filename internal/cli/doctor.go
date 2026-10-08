package cli

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"time"

	"github.com/smykla-skalski/bilgie/internal/auto"
	"github.com/smykla-skalski/bilgie/internal/config"
	"github.com/smykla-skalski/bilgie/internal/doctor"
	"github.com/spf13/cobra"
)

// DoctorCmd checks that the agent works and the config lets it act.
var DoctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Check that the agent runs and the config lets it clean",
	Long: `Checks, without changing anything:

  agent      installed and loaded in the scheduler of this OS
  cadence    tick and full-pass intervals, the last tick and the next expected full pass
  last run   time, tier, bytes freed and errors from runs.jsonl
  free space current free space and its 7-day trend from the run history
  config     disabled providers, providers skipped on every recent run, protected-path conflicts
  notifier   whether the low-space notification program exists

Exits non-zero when a finding needs attention; each one says what to do.`,
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE:         runDoctor,
}

func runDoctor(_ *cobra.Command, _ []string) error {
	env, err := defaultAutoEnv()
	if err != nil {
		return err
	}
	ctx, stop := interruptContext()
	defer stop()
	return runDoctorWithLoader(ctx, config.NewLoader(), env)
}

func runDoctorWithLoader(ctx context.Context, loader *config.Loader, env autoEnv) error {
	goos := env.goos
	if goos == "" {
		goos = runtime.GOOS
	}
	now := env.now
	if now == nil {
		now = time.Now
	}
	lookPath := env.lookPath
	if lookPath == nil {
		lookPath = exec.LookPath
	}

	in := doctor.Input{Now: now(), GOOS: goos, NotifierProgram: auto.NotifierProgram(goos)}

	cfg, cfgErr := loadValidConfig(loader)
	in.Cfg, in.ConfigErr = cfg, cfgErr

	agent := env.agent(0)
	in.LogPath = agent.LogFile()
	in.Agent, in.AgentErr = agent.Status(ctx)

	in.Runs, in.Corrupt, in.RunsErr = auto.ReadRuns(env.stateDir, 0)
	in.FirstRunPending = auto.FirstRunPending(env.stateDir)
	in.Tick, _ = auto.ReadTickState(env.stateDir)
	in.Pass, _ = auto.ReadPassState(env.stateDir)

	in.Free, in.FreeErr = env.free()

	if in.NotifierProgram != "" {
		_, in.NotifierErr = lookPath(in.NotifierProgram)
	}
	if cfg != nil {
		in.Conflicts = auto.ProtectionConflicts(cfg, env.home, env.newProvider)
	}

	rep := doctor.Diagnose(in)
	rep.Write(env.out)
	if n := rep.Attention(); n > 0 {
		return fmt.Errorf("doctor: %d finding(s) need attention", n)
	}
	return nil
}

func loadValidConfig(loader *config.Loader) (*config.Config, error) {
	cfg, err := loader.Load()
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("validate config: %w", err)
	}
	return cfg, nil
}
