package main

import (
	"fmt"
	"os"

	"github.com/Automaat/cache-buster/internal/cli"
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "cache-buster",
	Short: "macOS developer cache manager with size limits",
	Long:  `A CLI tool to manage developer caches on macOS with configurable size limits.`,
}

func init() {
	rootCmd.AddCommand(cli.StatusCmd)
	rootCmd.AddCommand(cli.CleanCmd)
	rootCmd.AddCommand(cli.ConfigCmd)
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
