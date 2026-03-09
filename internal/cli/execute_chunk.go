package cli

import (
	"fmt"

	"github.com/leightonvanrooijen/utopia/internal"
	"github.com/leightonvanrooijen/utopia/internal/chunk"
	"github.com/leightonvanrooijen/utopia/internal/domain"
)

// chunkCR invokes the chunking logic to produce work items from a change request.
func chunkCR(cr *domain.ChangeRequest, crID string, store *internal.YAMLStore, config *domain.Config, projectDir string) ([]*domain.WorkItem, error) {
	fmt.Printf("Chunking change request: %s\n", cr.Title)

	// Update CR status to in-progress when chunking begins
	cr.Status = domain.ChangeRequestInProgress
	if err := store.SaveChangeRequest(cr); err != nil {
		return nil, fmt.Errorf("failed to update CR status: %w", err)
	}

	// Run the chunking (includes validation)
	// Pass store.LoadSpec as the spec loader for bugfix CRs
	workItems, err := chunk.Chunk(cr, store.LoadSpec)
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
		fmt.Printf("  Git commit warning: %s\n", err)
	} else {
		fmt.Printf("  Committed work items for %s\n", crID)
	}

	// Print summary
	fmt.Println("Work items:")
	for _, item := range workItems {
		fmt.Printf("  [%d] %s\n", item.Order, item.ID)
	}
	fmt.Println()

	return workItems, nil
}
