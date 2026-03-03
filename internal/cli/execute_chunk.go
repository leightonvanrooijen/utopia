package cli

import (
	"fmt"

	"github.com/leightonvanrooijen/utopia/internal/domain"
	"github.com/leightonvanrooijen/utopia/internal/infra/storage"
	chunkStrategy "github.com/leightonvanrooijen/utopia/internal/strategies/chunk"
)

// SpecLoaderConfigurable is an optional interface that chunking strategies can implement
// to receive a spec loader for loading referenced specs during bugfix chunking.
type SpecLoaderConfigurable interface {
	SetSpecLoader(loader func(specID string) (*domain.Spec, error))
}

// chunkCR invokes the chunking strategy to produce work items from a change request.
func chunkCR(cr *domain.ChangeRequest, crID string, store *storage.YAMLStore, config *domain.Config, registry *chunkStrategy.Registry, projectDir string) ([]*domain.WorkItem, error) {
	fmt.Printf("Chunking change request: %s\n", cr.Title)

	// Update CR status to in-progress when chunking begins
	cr.Status = domain.ChangeRequestInProgress
	if err := store.SaveChangeRequest(cr); err != nil {
		return nil, fmt.Errorf("failed to update CR status: %w", err)
	}

	// Determine which chunking strategy to use
	strategyName := executeChunkStrategyFlag
	if strategyName == "" {
		strategyName = config.Strategies.Chunk
	}

	strategy, ok := registry.Get(strategyName)
	if !ok {
		available := registry.List()
		if len(available) == 0 {
			return nil, fmt.Errorf("no chunking strategies registered")
		}
		return nil, fmt.Errorf("unknown chunk strategy %q (available: %v)", strategyName, available)
	}

	// Configure spec loader if the strategy supports it (needed for bugfix CRs)
	if configurable, ok := strategy.(SpecLoaderConfigurable); ok {
		configurable.SetSpecLoader(store.LoadSpec)
	}

	fmt.Printf("Using '%s' chunk strategy: %s\n", strategy.Name(), strategy.Description())

	// Run the chunking strategy (includes validation)
	workItems, err := strategy.Chunk(cr)
	if err != nil {
		return nil, fmt.Errorf("chunking failed: %w", err)
	}

	// Save work items to .utopia/work-items/<id>/
	for _, item := range workItems {
		if err := store.SaveWorkItemForSpec(crID, item); err != nil {
			return nil, fmt.Errorf("failed to save work item %s: %w", item.ID, err)
		}
	}

	fmt.Printf("Created %d work item(s)\n\n", len(workItems))

	// Commit work items to git
	if err := gitCommitChunk(projectDir, crID); err != nil {
		// Log but don't fail - work items are saved, commit is non-critical
		fmt.Printf("⚠ Git commit warning: %s\n", err)
	} else {
		fmt.Printf("✓ Committed work items for %s\n", crID)
	}

	// Print summary
	fmt.Println("Work items:")
	for _, item := range workItems {
		fmt.Printf("  [%d] %s\n", item.Order, item.ID)
	}
	fmt.Println()

	return workItems, nil
}
