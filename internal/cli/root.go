package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/leightonvanrooijen/utopia/internal"
	"github.com/spf13/cobra"
)

// Version information - injected at build time via ldflags
var (
	Version   = "dev"     // -X github.com/leightonvanrooijen/utopia/internal/cli.Version=v1.0.0
	Commit    = "none"    // -X github.com/leightonvanrooijen/utopia/internal/cli.Commit=$(git rev-parse --short HEAD)
	BuildDate = "unknown" // -X github.com/leightonvanrooijen/utopia/internal/cli.BuildDate=$(date -u +%Y-%m-%dT%H:%M:%SZ)
)

var rootCmd = &cobra.Command{
	Use:   "utopia",
	Short: "AI-assisted development system",
	Long: `Utopia is a two-layer system where humans define intent through conversation,
and AI agents execute the work using the Ralph methodology.

Core Workflow:

  init     Set up a new Utopia project
              ↓
  cr       Create a change request through guided conversation
              ↓
  execute  AI executes the CR using Ralph loops until complete
              ↓
  harvest  Extract ADRs, concepts, and domain knowledge from conversations

Quick Start:
  utopia init              # Initialize project
  utopia cr                # Define what you want to build
  utopia execute           # Let AI implement it
  utopia harvest           # Capture learnings

Other Commands:
  merge    Manually merge a CR (execute does this automatically)
  status   View project state
  format   Format YAML files`,
}

// Execute runs the CLI
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func init() {
	// Global flags
	rootCmd.PersistentFlags().StringP("project", "p", ".", "project directory")

	// Version flag
	rootCmd.Version = Version
	rootCmd.SetVersionTemplate(`Utopia {{.Version}}
Commit:  ` + Commit + `
Built:   ` + BuildDate + `
`)
}

// GetProjectDir returns the project directory from flags
func GetProjectDir(cmd *cobra.Command) string {
	dir, _ := cmd.Flags().GetString("project")
	return dir
}

// ResolveProjectDir resolves the project directory to an absolute path.
// Unlike ResolveProject, this does NOT check for .utopia existence,
// making it suitable for 'init' which creates the .utopia directory.
func ResolveProjectDir(cmd *cobra.Command) (string, error) {
	projectDir := GetProjectDir(cmd)

	absPath, err := filepath.Abs(projectDir)
	if err != nil {
		return "", fmt.Errorf("failed to resolve project path: %w", err)
	}

	return absPath, nil
}

// ResolveProject resolves the project path, checks for .utopia directory,
// and returns the initialized store. This handles the common pattern used
// by most CLI commands that require an initialized Utopia project.
func ResolveProject(cmd *cobra.Command) (projectDir, utopiaDir string, store *internal.YAMLStore, err error) {
	projectDir = GetProjectDir(cmd)

	projectDir, err = filepath.Abs(projectDir)
	if err != nil {
		return "", "", nil, fmt.Errorf("failed to resolve project path: %w", err)
	}

	utopiaDir = filepath.Join(projectDir, ".utopia")

	// Check if initialized
	if _, err := os.Stat(utopiaDir); os.IsNotExist(err) {
		return "", "", nil, fmt.Errorf("not a Utopia project (run 'utopia init' first)")
	}

	store = internal.NewYAMLStore(utopiaDir)

	return projectDir, utopiaDir, store, nil
}
