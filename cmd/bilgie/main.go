package main

import (
	"fmt"
	"os"

	"github.com/smykla-skalski/bilgie/internal/appname"
	"github.com/smykla-skalski/bilgie/internal/cli"
	"github.com/smykla-skalski/bilgie/internal/config"
	"github.com/spf13/cobra"
)

var version = "dev"

var rootCmd = &cobra.Command{
	Use:     appname.Name,
	Version: version,
	Short:   "Developer cache manager that keeps free disk space in check",
	Long: `Bilgie works like a bilge pump with a float switch: it idles while free space is above the
configured level, pumps caches out below it and sweeps hard at the critical level.
It manages developer caches on macOS, Linux and Windows with configurable size limits.`,
	Args:              cobra.NoArgs,
	PersistentPreRunE: func(cmd *cobra.Command, _ []string) error { cli.MigrateLegacy(cmd.ErrOrStderr()); return nil },
	RunE:              runRoot,
}

func runRoot(_ *cobra.Command, _ []string) error {
	loader := config.NewLoader()
	return cli.RunInteractiveWithLoader(loader, false, true)
}

func init() {
	rootCmd.AddCommand(cli.StatusCmd)
	rootCmd.AddCommand(cli.CleanCmd)
	rootCmd.AddCommand(cli.ConfigCmd)
	rootCmd.AddCommand(cli.InteractiveCmd)
	rootCmd.AddCommand(cli.AutoCmd)
	rootCmd.AddCommand(cli.HistoryCmd)
	rootCmd.AddCommand(cli.DoctorCmd)
	rootCmd.AddCommand(cli.InstallAgentCmd)
	rootCmd.AddCommand(cli.UninstallAgentCmd)
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
