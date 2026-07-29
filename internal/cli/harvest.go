package cli

import (
	"context"
	"fmt"

	"github.com/leightonvanrooijen/utopia/internal/cli/ui"
	"github.com/leightonvanrooijen/utopia/internal/harvest"
	"github.com/spf13/cobra"
)

var (
	harvestModelFlag string
	harvestAuthFlag  string
)

var harvestCmd = &cobra.Command{
	Use:   "harvest",
	Short: "Qualification-based analysis of conversations for documentation candidates",
	Long: `Scan unprocessed conversations and apply qualification tests to identify documentation candidates.

The command will:
  1. Find unprocessed conversations from .utopia/conversations/ and unprocessed
     execution runs (what was actually built) from .utopia/runs/
  2. Apply qualification tests to identify documentation candidates:
     - ADR candidates (architectural decisions that pass category + reversal cost tests)
     - Concept candidates (educational content that passes orientation + independence tests)
     - Domain candidates (terms that pass domain specificity + precision + consistency tests)
  3. Present qualified candidates grouped by type with confidence levels
  4. Cross-reference existing docs to avoid duplicates
  5. Let you select which docs to create (individual, multiple, or all)
  6. Flow context between creations (ADR created first is known when creating Concept)
  7. Allow created docs to reference each other
  8. Mark conversations and execution runs as processed only after you complete or exit

Benefits over individual commands (/adr, /concept, /domain):
  - Single pass through conversations and execution runs (efficiency)
  - Cross-type awareness (related candidates linked)
  - Context flows between doc creations
  - Documents can reference each other naturally`,
	RunE: runHarvest,
}

func init() {
	rootCmd.AddCommand(harvestCmd)
	harvestCmd.Flags().StringVar(&harvestModelFlag, "model", "", "model alias (haiku, sonnet, opus, fable) or a full model identifier")
	harvestCmd.Flags().StringVar(&harvestAuthFlag, "auth", "", "credential to use (api-key, subscription), overriding config.auth.mode")
}

func runHarvest(cmd *cobra.Command, args []string) error {
	out := ui.NewPrinter(cmd.OutOrStdout(), cmd.ErrOrStderr())

	// Validate model flag early before any work
	modelID, err := ResolveModelFlag(cmd)
	if err != nil {
		return err
	}

	// Resolve and report the credential this invocation runs with, before any
	// work starts
	authMode, err := ResolveAuth(cmd)
	if err != nil {
		return err
	}

	absPath, utopiaDir, store, err := ResolveProject(cmd)
	if err != nil {
		return err
	}

	result, err := harvest.Run(context.Background(), store, harvest.Options{
		ProjectDir: absPath,
		UtopiaDir:  utopiaDir,
		Model:      modelID,
		Auth:       authMode,
	})
	if err != nil {
		return fmt.Errorf("harvest failed: %w", err)
	}

	if result.UnprocessedConversations == 0 && result.UnprocessedRuns == 0 {
		out.Printf("No unprocessed conversations or execution runs found.\n")
		out.Printf("Conversations are created when you use /cr or other interactive commands.\n")
		out.Printf("Execution runs are created when work items execute.\n")
	}
	return nil
}
