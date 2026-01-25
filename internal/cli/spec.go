package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/leightonvanrooijen/utopia/internal/infra/storage"
	"github.com/spf13/cobra"
)

var (
	specStrategyFlag string
)

var specCmd = &cobra.Command{
	Use:   "spec",
	Short: "Create or refine a specification",
	Long: `Start a conversation with Claude to create or refine a specification.

The conversation flows through stages:
  1. EXPLORE  - What problem are you solving?
  2. DEFINE   - What's in scope for v1?
  3. SPECIFY  - What are the features and acceptance criteria?

Claude guides you through these stages naturally. The resulting
specification is saved to .utopia/specs/`,
	RunE: runSpec,
}

func init() {
	rootCmd.AddCommand(specCmd)

	specCmd.Flags().StringVarP(&specStrategyFlag, "strategy", "s", "",
		"spec creation strategy (guided, minimal, template)")
}

func runSpec(cmd *cobra.Command, args []string) error {
	projectDir := GetProjectDir(cmd)

	absPath, err := filepath.Abs(projectDir)
	if err != nil {
		return fmt.Errorf("failed to resolve project path: %w", err)
	}

	utopiaDir := filepath.Join(absPath, ".utopia")

	// Check if initialized
	if _, err := os.Stat(utopiaDir); os.IsNotExist(err) {
		return fmt.Errorf("not a Utopia project (run 'utopia init' first)")
	}

	// Load config to validate project
	store := storage.NewYAMLStore(utopiaDir)
	_, err = store.LoadConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Create drafts directory if it doesn't exist
	draftsDir := filepath.Join(utopiaDir, "specs", "_drafts")
	if err := os.MkdirAll(draftsDir, 0755); err != nil {
		return fmt.Errorf("failed to create drafts directory: %w", err)
	}

	// Generate a spec path for the TUI to watch
	specPath := filepath.Join(draftsDir, "current-spec.yaml")

	// Run the TUI
	ctx := context.Background()
	if err := RunTUI(ctx, specPath); err != nil {
		return fmt.Errorf("TUI failed: %w", err)
	}

	fmt.Println("\nSession ended. Check .utopia/specs/_drafts/ for any generated specs.")

	return nil
}
