package cli

import (
	"context"
	"fmt"

	"github.com/leightonvanrooijen/utopia/internal/harvest"
	"github.com/spf13/cobra"
)

var harvestModelFlag string

var harvestCmd = &cobra.Command{
	Use:   "harvest",
	Short: "Qualification-based analysis of conversations for documentation candidates",
	Long: `Scan unprocessed conversations and apply qualification tests to identify documentation candidates.

The command will:
  1. Find unprocessed conversations from .utopia/conversations/
  2. Apply qualification tests to identify documentation candidates:
     - ADR candidates (architectural decisions that pass category + reversal cost tests)
     - Concept candidates (educational content that passes orientation + independence tests)
     - Domain candidates (terms that pass domain specificity + precision + consistency tests)
  3. Present qualified candidates grouped by type with confidence levels
  4. Cross-reference existing docs to avoid duplicates
  5. Let you select which docs to create (individual, multiple, or all)
  6. Flow context between creations (ADR created first is known when creating Concept)
  7. Allow created docs to reference each other
  8. Mark conversation as processed only after you complete or exit

Benefits over individual commands (/adr, /concept, /domain):
  - Single pass through conversations (efficiency)
  - Cross-type awareness (related candidates linked)
  - Context flows between doc creations
  - Documents can reference each other naturally`,
	RunE: runHarvest,
}

func init() {
	rootCmd.AddCommand(harvestCmd)
	harvestCmd.Flags().StringVar(&harvestModelFlag, "model", "", "model to use (haiku, sonnet, opus)")
}

func runHarvest(cmd *cobra.Command, args []string) error {
	// Validate model flag early before any work
	modelID, err := ResolveModelFlag(cmd)
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
	})
	if err != nil {
		return err
	}

	if result.UnprocessedConversations == 0 {
		fmt.Println("No unprocessed conversations found.")
		fmt.Println("Conversations are created when you use /cr or other interactive commands.")
	}
	return nil
}
