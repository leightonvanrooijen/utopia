package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/leightonvanrooijen/utopia/internal/infra/storage"
	chunkStrategy "github.com/leightonvanrooijen/utopia/internal/strategies/chunk"
	"github.com/spf13/cobra"
)

var (
	chunkStrategyFlag string
	chunkRegistry     *chunkStrategy.Registry
)

var chunkCmd = &cobra.Command{
	Use:   "chunk <id>",
	Short: "Chunk a spec or refactor into work items",
	Long: `Transform a specification or refactor into discrete work items for Ralph execution.

The command searches for the ID in the following order:
  1. .utopia/specs/<id>.yaml (spec)
  2. .utopia/specs/_changerequests/<id>.yaml (change request)
  3. .utopia/refactors/<id>.yaml (refactor)

The chunking strategy determines how features/tasks are mapped to work items:
  - ralph-sequential: One work item per feature/task, executed in order

Work items are saved to .utopia/work-items/<id>/`,
	Args: cobra.ExactArgs(1),
	RunE: runChunk,
}

func init() {
	rootCmd.AddCommand(chunkCmd)

	chunkCmd.Flags().StringVarP(&chunkStrategyFlag, "strategy", "s", "",
		"chunking strategy (ralph-sequential)")

	// Initialize registry - strategies will be registered at startup
	chunkRegistry = chunkStrategy.NewRegistry()
}

// RegisterChunkStrategy adds a strategy to the registry (called from main)
func RegisterChunkStrategy(s chunkStrategy.Strategy) {
	chunkRegistry.Register(s)
}

func runChunk(cmd *cobra.Command, args []string) error {
	docID := args[0]
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

	// Load config
	store := storage.NewYAMLStore(utopiaDir)
	config, err := store.LoadConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Load the spec, change request, or refactor (all converted to spec)
	spec, sourceType, err := store.LoadSpecOrChangeRequestOrRefactor(docID)
	if err != nil {
		return fmt.Errorf("document not found: %s\n\nCheck .utopia/specs/, .utopia/specs/_changerequests/, or .utopia/refactors/ for available documents", docID)
	}

	switch sourceType {
	case storage.SourceChangeRequest:
		fmt.Printf("Loaded change request: %s\n", docID)
	case storage.SourceRefactor:
		fmt.Printf("Loaded refactor: %s\n", docID)
	}

	// Determine which strategy to use
	strategyName := chunkStrategyFlag
	if strategyName == "" {
		strategyName = config.Strategies.Chunk
	}

	strategy, ok := chunkRegistry.Get(strategyName)
	if !ok {
		available := chunkRegistry.List()
		if len(available) == 0 {
			return fmt.Errorf("no chunking strategies registered")
		}
		return fmt.Errorf("unknown strategy %q (available: %v)", strategyName, available)
	}

	fmt.Printf("Using '%s' strategy: %s\n", strategy.Name(), strategy.Description())
	fmt.Printf("Chunking spec: %s\n\n", spec.Title)

	// Run the strategy (includes validation)
	workItems, err := strategy.Chunk(spec)
	if err != nil {
		return fmt.Errorf("chunking failed: %w", err)
	}

	// Save work items to .utopia/work-items/<id>/
	for _, item := range workItems {
		if err := store.SaveWorkItemForSpec(docID, item); err != nil {
			return fmt.Errorf("failed to save work item %s: %w", item.ID, err)
		}
	}

	fmt.Printf("Created %d work item(s) in .utopia/work-items/%s/\n", len(workItems), docID)

	// Print summary
	fmt.Println("\nWork items:")
	for _, item := range workItems {
		fmt.Printf("  [%d] %s\n", item.Order, item.ID)
	}

	return nil
}
