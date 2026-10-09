package cli

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/smykla-skalski/bilgie/internal/migrate"
)

// ErrReported marks a failure whose message was already shown earlier; the
// caller exits non-zero without printing it again.
var ErrReported = errors.New("already reported")

// BeforeCommand runs before every command. It moves the config and state dirs
// of the pre-rename name to the current ones, except for help and completion,
// which must never touch the disk. While the legacy config has not reached the
// current location it refuses the commands that delete or schedule deletion
// rather than let them run on default config.
func BeforeCommand(cmd *cobra.Command) (err error) {
	if skipsMigration(cmd) {
		return nil
	}
	out := cmd.ErrOrStderr()
	defer func() {
		r := recover()
		if r == nil {
			return
		}
		fmt.Fprintf(out, "warning: the legacy migration check failed: %v\n", r)
		err = nil
		if isDestructive(cmd) {
			cmd.SilenceUsage = true
			err = fmt.Errorf("refusing to run %s: could not check whether the legacy config was migrated", cmd.CommandPath())
		}
	}()
	return beforeCommand(cmd, out)
}

// Seams for tests.
var (
	runLegacy     = migrate.Legacy
	checkPending  = migrate.PendingConfig
	userHomeDir   = os.UserHomeDir
	reportOnce    = migrate.ShouldReport
	forgetFailure = migrate.Resolved
)

func beforeCommand(cmd *cobra.Command, out io.Writer) error {
	home, err := userHomeDir()
	if err != nil {
		fmt.Fprintf(out, "warning: not migrating legacy dirs: %v\n", err)
		return nil
	}
	runLegacy(home, out)

	pending := checkPending(home)
	if pending == nil {
		forgetFailure(home, pendingKey)
		return nil
	}
	msg := fmt.Sprintf("the legacy config %s was not migrated to %s, so defaults would ignore your settings; run: %s (on a permission error, make both directories writable first)",
		pending.Old, pending.New, pending.Step())
	first := reportOnce(home, pendingKey, msg)
	if !isDestructive(cmd) {
		if first {
			fmt.Fprintf(out, "warning: running on default config: %s\n", msg)
		}
		return nil
	}
	cmd.SilenceUsage = true
	if !first && cmd.Name() == "tick" {
		return ErrReported
	}
	return fmt.Errorf("refusing to run %s: %s", cmd.CommandPath(), msg)
}

const pendingKey = "pending-config"

// skipsMigration is true for help and shell completion, which run on every TAB.
func skipsMigration(cmd *cobra.Command) bool {
	for c := cmd; c != nil && c.HasParent(); c = c.Parent() {
		switch c.Name() {
		case "help", "completion", cobra.ShellCompRequestCmd, cobra.ShellCompNoDescRequestCmd:
			return true
		}
	}
	return false
}

// isDestructive is true for a command that can delete, or schedules one that
// will. The commands that only preview never read the config to delete.
func isDestructive(cmd *cobra.Command) bool {
	switch cmd.Name() {
	case "install-agent":
		return true
	case "init", "edit":
		// they write a default config.yaml, which would hide the pending one
		return cmd.Parent() != nil && cmd.Parent().Name() == "config"
	case "clean", "interactive":
		return !flagSet(cmd, "dry-run")
	case "auto", "tick":
		return !flagSet(cmd, "dry-run") && !flagSet(cmd, "assume-free")
	}
	return !cmd.HasParent()
}

func flagSet(cmd *cobra.Command, name string) bool {
	f := cmd.Flags().Lookup(name)
	return f != nil && f.Value.String() != "" && f.Value.String() != "false"
}
