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
	Use:   "chunk <spec-id>",
	Short: "Chunk a spec into work items",
	Long: `Transform a specification into discrete work items for Ralph execution.

The chunking strategy determines how features are mapped to work items:
  - ralph-sequential: One work item per feature, executed in order

Work items are saved to .utopia/work-items/<spec-id>/`,
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
	specID := args[0]
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

	// Load the spec (or change request converted to spec)
	spec, isChangeRequest, err := store.LoadSpecOrChangeRequest(specID)
	if err != nil {
		return fmt.Errorf("spec not found: %s\n\nCheck .utopia/specs/ or .utopia/specs/_changerequests/ for available specs", specID)
	}

	if isChangeRequest {
		fmt.Printf("Loaded change request: %s\n", specID)
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

	// Save work items to .utopia/work-items/<spec-id>/
	for _, item := range workItems {
		if err := store.SaveWorkItemForSpec(specID, item); err != nil {
			return fmt.Errorf("failed to save work item %s: %w", item.ID, err)
		}
	}

	fmt.Printf("Created %d work item(s) in .utopia/work-items/%s/\n", len(workItems), specID)

	// Print summary
	fmt.Println("\nWork items:")
	for _, item := range workItems {
		fmt.Printf("  [%d] %s\n", item.Order, item.ID)
	}

	return nil
}
