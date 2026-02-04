package cli

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/Automaat/cache-buster/internal/config"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

// ConfigCmd manages configuration.
var ConfigCmd = &cobra.Command{
	Use:   "config",
	Short: "Manage configuration",
}

var configShowCmd = &cobra.Command{
	Use:   "show",
	Short: "Display current configuration",
	RunE:  runConfigShow,
}

var configInitCmd = &cobra.Command{
	Use:   "init",
	Short: "Create default configuration file",
	RunE:  runConfigInit,
}

var configEditCmd = &cobra.Command{
	Use:   "edit",
	Short: "Open configuration in $EDITOR",
	RunE:  runConfigEdit,
}

func init() {
	ConfigCmd.AddCommand(configShowCmd)
	ConfigCmd.AddCommand(configInitCmd)
	ConfigCmd.AddCommand(configEditCmd)
}

func runConfigShow(_ *cobra.Command, _ []string) error {
	return runConfigShowWithLoader(config.NewLoader())
}

func runConfigShowWithLoader(loader *config.Loader) error {
	cfg, _, err := loader.LoadOrCreate()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	out, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}

	configPath, _ := config.Path()
	fmt.Printf("# %s\n", configPath)
	fmt.Print(string(out))
	return nil
}

func runConfigInit(_ *cobra.Command, _ []string) error {
	return runConfigInitWithLoader(config.NewLoader())
}

func runConfigInitWithLoader(loader *config.Loader) error {
	created, err := loader.InitDefault()
	if err != nil {
		return fmt.Errorf("init config: %w", err)
	}

	configPath, _ := config.Path()
	if created {
		fmt.Printf("Created %s\n", configPath)
	} else {
		fmt.Printf("Config already exists: %s\n", configPath)
	}
	return nil
}

func runConfigEdit(_ *cobra.Command, _ []string) error {
	return runConfigEditWithLoader(config.NewLoader(), os.Getenv("EDITOR"))
}

func runConfigEditWithLoader(loader *config.Loader, editor string) error {
	if editor == "" {
		editor = "vi"
	}

	// Ensure config exists
	if _, err := loader.InitDefault(); err != nil {
		return fmt.Errorf("init config: %w", err)
	}

	configPath, err := config.Path()
	if err != nil {
		return fmt.Errorf("get config path: %w", err)
	}

	cmd := exec.Command(editor, configPath)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Run()
}
