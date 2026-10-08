package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/Automaat/cache-buster/internal/auto"
	"github.com/Automaat/cache-buster/internal/config"
	"github.com/Automaat/cache-buster/internal/provider"
	"github.com/Automaat/cache-buster/pkg/size"
	"github.com/spf13/cobra"
)

// autoRunTimeout caps one unattended run so a stuck provider cannot pile up runs.
const autoRunTimeout = 45 * time.Minute

// AutoCmd trims caches based on free disk space.
var AutoCmd = &cobra.Command{
	Use:   "auto",
	Short: "Trim caches based on free disk space (for unattended runs)",
	Long: `Reads free space of the data volume and picks a pressure tier:

  ok        free space above min_free and min_free_pct: smart-trim only providers over their limit
  low       below either floor: smart-trim every enabled provider, cheapest to rebuild first,
            stopping once free space recovers
  critical  under 5 GiB free: also sweep stale directories (dir-pattern providers, even when disabled)

The docker-volumes provider never runs. Providers pointing at Downloads, opencode
or worktrees are skipped. The first run after install-agent is a dry-run.`,
	Args: cobra.NoArgs,
	RunE: runAuto,
}

// InstallAgentCmd installs the launchd agent that runs auto.
var InstallAgentCmd = &cobra.Command{
	Use:   "install-agent",
	Short: "Install the launchd agent that runs auto periodically",
	Args:  cobra.NoArgs,
	RunE:  runInstallAgent,
}

// UninstallAgentCmd removes the launchd agent.
var UninstallAgentCmd = &cobra.Command{
	Use:   "uninstall-agent",
	Short: "Remove the launchd agent",
	Args:  cobra.NoArgs,
	RunE:  runUninstallAgent,
}

func init() {
	AutoCmd.Flags().Bool("dry-run", false, "Preview without deleting")
	AutoCmd.Flags().String("assume-free", "", "Pretend this much space is free (e.g. 3G); for testing the tiers")
	_ = AutoCmd.Flags().MarkHidden("assume-free")
}

// autoEnv holds everything auto touches outside the config, so tests can
// replace the disk, the state directory and launchctl.
type autoEnv struct {
	free        auto.FreeFunc
	newProvider func(name string, cfg config.Provider) (provider.Provider, error)
	exec        auto.Executor
	out         io.Writer
	stateDir    string
	home        string
	exe         string
	uid         int
}

func defaultAutoEnv() (autoEnv, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return autoEnv{}, fmt.Errorf("get home dir: %w", err)
	}
	stateDir, err := auto.StateDir()
	if err != nil {
		return autoEnv{}, err
	}
	exe, err := os.Executable()
	if err != nil {
		return autoEnv{}, fmt.Errorf("find own binary: %w", err)
	}
	return autoEnv{
		free:        auto.StatfsFree(auto.DataVolumePath()),
		newProvider: provider.NewProvider,
		exec:        auto.ExecCommand,
		out:         os.Stdout,
		stateDir:    stateDir,
		home:        home,
		exe:         exe,
		uid:         os.Getuid(),
	}, nil
}

func runAuto(cmd *cobra.Command, _ []string) error {
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	assume, _ := cmd.Flags().GetString("assume-free")

	env, err := defaultAutoEnv()
	if err != nil {
		return err
	}
	if assume != "" {
		free, err := assumedFree(assume, env.free)
		if err != nil {
			return err
		}
		env.free = free
	}

	ctx, stop := interruptContext()
	defer stop()
	return runAutoWithLoader(ctx, config.NewLoader(), env, dryRun)
}

func assumedFree(value string, statfs auto.FreeFunc) (auto.FreeFunc, error) {
	free, err := size.ParseSize(value)
	if err != nil {
		return nil, fmt.Errorf("--assume-free: %w", err)
	}
	return func() (auto.FreeSpace, error) {
		fs, err := statfs()
		if err != nil {
			return auto.FreeSpace{}, err
		}
		fs.Free = free
		return fs, nil
	}, nil
}

func runAutoWithLoader(ctx context.Context, loader *config.Loader, env autoEnv, dryRun bool) error {
	cfg, err := loader.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("validate config: %w", err)
	}

	lock, ok, err := auto.AcquireRunLock(env.stateDir)
	if err != nil {
		return err
	}
	if !ok {
		fmt.Fprintln(env.out, "another auto run is in progress, exiting")
		return nil
	}
	defer lock.Release()

	firstRun := auto.FirstRunPending(env.stateDir)
	if firstRun && !dryRun {
		fmt.Fprintln(env.out, "first run after install-agent: dry-run, nothing is deleted")
		dryRun = true
	}

	ctx, cancel := context.WithTimeout(ctx, autoRunTimeout)
	defer cancel()

	report, err := auto.Run(ctx, cfg, dryRun, auto.Deps{
		Free:        env.free,
		NewProvider: env.newProvider,
		Out:         env.out,
		Home:        env.home,
	})
	if err != nil {
		return err
	}

	if firstRun && dryRun {
		if clearErr := auto.ClearFirstRun(env.stateDir); clearErr != nil {
			return clearErr
		}
	}
	return report.Err()
}

func runInstallAgent(cmd *cobra.Command, _ []string) error {
	env, err := defaultAutoEnv()
	if err != nil {
		return err
	}
	return runInstallAgentWithLoader(cmd.Context(), config.NewLoader(), env)
}

func runInstallAgentWithLoader(ctx context.Context, loader *config.Loader, env autoEnv) error {
	cfg, err := loader.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("validate config: %w", err)
	}
	interval, err := cfg.Auto.IntervalDuration()
	if err != nil {
		return err
	}
	return env.agent(interval).Install(ctx)
}

func runUninstallAgent(cmd *cobra.Command, _ []string) error {
	env, err := defaultAutoEnv()
	if err != nil {
		return err
	}
	return env.agent(0).Uninstall(cmd.Context())
}

func (e autoEnv) agent(interval time.Duration) auto.Agent {
	return auto.Agent{
		Exec:     e.exec,
		Out:      e.out,
		Home:     e.home,
		Exe:      e.exe,
		StateDir: e.stateDir,
		UID:      e.uid,
		Interval: interval,
	}
}
