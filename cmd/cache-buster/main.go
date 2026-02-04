package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "cache-buster",
	Short: "macOS developer cache manager with size limits",
	Long:  `A CLI tool to manage developer caches on macOS with configurable size limits.`,
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
