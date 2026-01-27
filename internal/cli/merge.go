package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/leightonvanrooijen/utopia/internal/domain"
	"github.com/leightonvanrooijen/utopia/internal/infra/storage"
	"github.com/spf13/cobra"
)

var (
	mergeDryRun bool
)

var mergeCmd = &cobra.Command{
	Use:   "merge <change-request-id>",
	Short: "Merge a change request into target specs",
	Long: `Merge a completed change request into its target specifications.

This command:
  1. Loads the change request from .utopia/specs/_changerequests/
  2. Groups changes by target spec
  3. Loads each target spec (or creates it for add operations)
  4. Applies changes to each spec
  5. Saves all updated specs atomically
  6. Deletes the change request and its work items

Use --dry-run to preview changes without applying them.`,
	Args: cobra.ExactArgs(1),
	RunE: runMerge,
}

func init() {
	rootCmd.AddCommand(mergeCmd)

	mergeCmd.Flags().BoolVar(&mergeDryRun, "dry-run", false,
		"Preview changes without applying them")
}

func runMerge(cmd *cobra.Command, args []string) error {
	changeRequestID := args[0]
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

	store := storage.NewYAMLStore(utopiaDir)

	// Load the change request
	cr, err := store.LoadChangeRequest(changeRequestID)
	if err != nil {
		return fmt.Errorf("change request not found: %s\n\nCheck .utopia/specs/_changerequests/ for available change requests", changeRequestID)
	}

	fmt.Printf("Change Request: %s\n", cr.Title)
	fmt.Printf("Type: %s\n", cr.Type)

	// Refactor CRs don't modify specs - just delete the CR
	if cr.Type == domain.CRTypeRefactor {
		return mergeRefactor(cr, changeRequestID, utopiaDir, store)
	}

	// Initiative CRs have phases that each need to be merged
	if cr.Type == domain.CRTypeInitiative {
		return mergeInitiative(cr, changeRequestID, utopiaDir, store)
	}

	// Feature/enhancement/removal CRs modify specs
	// Group changes by target spec
	changesBySpec := groupChangesBySpec(cr.Changes)
	specIDs := sortedSpecIDs(changesBySpec)

	fmt.Printf("Target specs: %d\n", len(specIDs))
	fmt.Println()

	// Summarize changes per spec
	fmt.Println("Changes to apply:")
	var totalAdd, totalModify, totalRemove int
	for _, specID := range specIDs {
		changes := changesBySpec[specID]
		var addCount, modifyCount, removeCount int
		for _, change := range changes {
			switch change.Operation {
			case "add":
				addCount++
			case "modify":
				modifyCount++
			case "remove":
				removeCount++
			}
		}
		totalAdd += addCount
		totalModify += modifyCount
		totalRemove += removeCount
		fmt.Printf("  %s: +%d ~%d -%d\n", specID, addCount, modifyCount, removeCount)
		for _, change := range changes {
			switch change.Operation {
			case "add":
				if change.Feature != nil {
					fmt.Printf("    + Add feature: %s\n", change.Feature.ID)
				}
				if len(change.DomainKnowledge) > 0 {
					fmt.Printf("    + Add %d domain knowledge item(s)\n", len(change.DomainKnowledge))
				}
			case "modify":
				if change.FeatureID != "" {
					fmt.Printf("    ~ Modify feature: %s\n", change.FeatureID)
				}
				if change.DomainKnowledgeMod != nil {
					fmt.Printf("    ~ Modify domain knowledge\n")
				}
			case "remove":
				if change.FeatureID != "" {
					fmt.Printf("    - Remove feature: %s\n", change.FeatureID)
				}
				if len(change.DomainKnowledge) > 0 {
					fmt.Printf("    - Remove %d domain knowledge item(s)\n", len(change.DomainKnowledge))
				}
			}
		}
	}
	fmt.Println()

	if mergeDryRun {
		fmt.Println("Dry run mode - no changes applied")
		fmt.Printf("\nWould merge %d add, %d modify, %d remove operation(s) into %d spec(s)\n",
			totalAdd, totalModify, totalRemove, len(specIDs))
		return nil
	}

	// Load all specs (or create for add-only operations)
	specs := make(map[string]*domain.Spec)
	createdSpecs := make(map[string]bool)
	for _, specID := range specIDs {
		spec, err := store.LoadSpec(specID)
		if err != nil {
			// Check if all changes for this spec are "add" operations
			if allAdds(changesBySpec[specID]) {
				// Create a new spec
				spec = domain.NewSpec(specID, specID)
				createdSpecs[specID] = true
			} else {
				return fmt.Errorf("spec not found: %s\n\nThe change request references a spec that doesn't exist (non-add operations require existing spec)", specID)
			}
		}
		specs[specID] = spec
	}

	// Apply changes to each spec (in memory first for atomicity)
	for _, specID := range specIDs {
		spec := specs[specID]
		changes := changesBySpec[specID]
		tempCR := &domain.ChangeRequest{Changes: changes}
		if err := tempCR.ApplyChanges(spec); err != nil {
			return fmt.Errorf("failed to apply changes to spec %s: %w", specID, err)
		}
	}

	// Save all specs (atomic commit phase)
	for _, specID := range specIDs {
		if err := store.SaveSpec(specs[specID]); err != nil {
			return fmt.Errorf("failed to save spec %s: %w\n\nSome specs may have been saved. Manual cleanup may be required.", specID, err)
		}
		if createdSpecs[specID] {
			fmt.Printf("✓ Created spec: %s\n", specID)
		} else {
			fmt.Printf("✓ Updated spec: %s\n", specID)
		}
	}

	// Delete the change request
	if err := store.DeleteChangeRequest(changeRequestID); err != nil {
		return fmt.Errorf("failed to delete change request: %w", err)
	}
	fmt.Printf("✓ Deleted change request: %s\n", changeRequestID)

	// Delete work items directory if it exists
	workItemsDir := filepath.Join(utopiaDir, "work-items", changeRequestID)
	if _, err := os.Stat(workItemsDir); err == nil {
		if err := os.RemoveAll(workItemsDir); err != nil {
			return fmt.Errorf("failed to delete work items: %w", err)
		}
		fmt.Printf("✓ Deleted work items: %s\n", changeRequestID)
	}

	// Print summary
	fmt.Println()
	fmt.Println("Merge Summary:")
	fmt.Printf("  Total changes: %d\n", len(cr.Changes))
	fmt.Printf("  Specs affected: %d\n", len(specIDs))
	for _, specID := range specIDs {
		if createdSpecs[specID] {
			fmt.Printf("    - %s (created)\n", specID)
		} else {
			fmt.Printf("    - %s (updated)\n", specID)
		}
	}

	return nil
}

// groupChangesBySpec groups changes by their target spec ID
func groupChangesBySpec(changes []domain.Change) map[string][]domain.Change {
	result := make(map[string][]domain.Change)
	for _, change := range changes {
		specID := change.Spec
		result[specID] = append(result[specID], change)
	}
	return result
}

// sortedSpecIDs returns spec IDs in sorted order for deterministic processing
func sortedSpecIDs(changesBySpec map[string][]domain.Change) []string {
	ids := make([]string, 0, len(changesBySpec))
	for id := range changesBySpec {
		ids = append(ids, id)
	}
	// Simple insertion sort for deterministic order
	for i := 1; i < len(ids); i++ {
		for j := i; j > 0 && ids[j] < ids[j-1]; j-- {
			ids[j], ids[j-1] = ids[j-1], ids[j]
		}
	}
	return ids
}

// allAdds returns true if all changes are "add" operations
func allAdds(changes []domain.Change) bool {
	for _, c := range changes {
		if c.Operation != "add" {
			return false
		}
	}
	return true
}

// mergeInitiative handles merge for initiative CRs with multiple phases.
// Each phase's changes are applied to their target specs in order.
// Refactor phases don't modify specs (they only restructure code).
func mergeInitiative(cr *domain.ChangeRequest, changeRequestID, utopiaDir string, store *storage.YAMLStore) error {
	fmt.Printf("Phases: %d total\n", len(cr.Phases))

	// Check all phases are complete
	for i, phase := range cr.Phases {
		if phase.Status != domain.PhaseStatusComplete {
			return fmt.Errorf("phase %d is not complete (status: %s)\n\nComplete all phases before merging", i+1, phase.Status)
		}
	}

	fmt.Println()

	// Collect all changes from non-refactor phases
	var allChanges []domain.Change
	var refactorPhaseCount int
	for i, phase := range cr.Phases {
		if phase.Type == domain.CRTypeRefactor {
			refactorPhaseCount++
			fmt.Printf("Phase %d (refactor): %d tasks - no spec changes\n", i+1, len(phase.Tasks))
			continue
		}
		fmt.Printf("Phase %d (%s): %d changes\n", i+1, phase.Type, len(phase.Changes))
		allChanges = append(allChanges, phase.Changes...)
	}
	fmt.Println()

	if len(allChanges) == 0 && refactorPhaseCount == len(cr.Phases) {
		// All phases were refactors - just clean up
		fmt.Println("All phases were refactors - no spec modifications needed")
	} else if len(allChanges) > 0 {
		// Group changes by target spec
		changesBySpec := groupChangesBySpec(allChanges)
		specIDs := sortedSpecIDs(changesBySpec)

		fmt.Println("Changes to apply:")
		var totalAdd, totalModify, totalRemove int
		for _, specID := range specIDs {
			changes := changesBySpec[specID]
			var addCount, modifyCount, removeCount int
			for _, change := range changes {
				switch change.Operation {
				case "add":
					addCount++
				case "modify":
					modifyCount++
				case "remove":
					removeCount++
				}
			}
			totalAdd += addCount
			totalModify += modifyCount
			totalRemove += removeCount
			fmt.Printf("  %s: +%d ~%d -%d\n", specID, addCount, modifyCount, removeCount)
		}
		fmt.Println()

		if mergeDryRun {
			fmt.Println("Dry run mode - no changes applied")
			fmt.Printf("\nWould merge %d add, %d modify, %d remove operation(s) into %d spec(s)\n",
				totalAdd, totalModify, totalRemove, len(specIDs))
			return nil
		}

		// Load all specs (or create for add-only operations)
		specs := make(map[string]*domain.Spec)
		createdSpecs := make(map[string]bool)
		for _, specID := range specIDs {
			spec, err := store.LoadSpec(specID)
			if err != nil {
				if allAdds(changesBySpec[specID]) {
					spec = domain.NewSpec(specID, specID)
					createdSpecs[specID] = true
				} else {
					return fmt.Errorf("spec not found: %s\n\nThe change request references a spec that doesn't exist (non-add operations require existing spec)", specID)
				}
			}
			specs[specID] = spec
		}

		// Apply changes to each spec
		for _, specID := range specIDs {
			spec := specs[specID]
			changes := changesBySpec[specID]
			tempCR := &domain.ChangeRequest{Changes: changes}
			if err := tempCR.ApplyChanges(spec); err != nil {
				return fmt.Errorf("failed to apply changes to spec %s: %w", specID, err)
			}
		}

		// Save all specs
		for _, specID := range specIDs {
			if err := store.SaveSpec(specs[specID]); err != nil {
				return fmt.Errorf("failed to save spec %s: %w", specID, err)
			}
			if createdSpecs[specID] {
				fmt.Printf("✓ Created spec: %s\n", specID)
			} else {
				fmt.Printf("✓ Updated spec: %s\n", specID)
			}
		}
	}

	if mergeDryRun {
		fmt.Println("Dry run mode - no changes applied")
		return nil
	}

	// Delete the change request
	if err := store.DeleteChangeRequest(changeRequestID); err != nil {
		return fmt.Errorf("failed to delete change request: %w", err)
	}
	fmt.Printf("✓ Deleted change request: %s\n", changeRequestID)

	// Delete work items for all phases
	for i := range cr.Phases {
		phaseWorkDir := filepath.Join(utopiaDir, "work-items", changeRequestID, fmt.Sprintf("phase-%d", i))
		if _, err := os.Stat(phaseWorkDir); err == nil {
			if err := os.RemoveAll(phaseWorkDir); err != nil {
				return fmt.Errorf("failed to delete work items for phase %d: %w", i+1, err)
			}
			fmt.Printf("✓ Deleted work items: phase %d\n", i+1)
		}
	}

	// Also delete the parent work-items directory for this CR if it's empty
	crWorkDir := filepath.Join(utopiaDir, "work-items", changeRequestID)
	if entries, err := os.ReadDir(crWorkDir); err == nil && len(entries) == 0 {
		os.Remove(crWorkDir)
	}

	fmt.Println()
	fmt.Printf("Successfully merged initiative: %s\n", cr.Title)

	return nil
}

// mergeRefactor handles merge for refactor CRs, which don't modify specs.
// Refactors only restructure code while preserving behavior, so merge
// simply deletes the CR and its work items.
func mergeRefactor(cr *domain.ChangeRequest, changeRequestID, utopiaDir string, store *storage.YAMLStore) error {
	fmt.Printf("Tasks completed: %d\n", len(cr.Tasks))
	fmt.Println()

	// Summarize tasks
	fmt.Println("Completed tasks:")
	for _, task := range cr.Tasks {
		fmt.Printf("  ✓ %s: %s\n", task.ID, task.Description)
	}
	fmt.Println()

	if mergeDryRun {
		fmt.Println("Dry run mode - no changes applied")
		fmt.Printf("\nWould delete refactor CR: %s (no specs modified)\n", changeRequestID)
		return nil
	}

	// Delete the change request (no spec modifications for refactors)
	if err := store.DeleteChangeRequest(changeRequestID); err != nil {
		return fmt.Errorf("failed to delete change request: %w", err)
	}
	fmt.Printf("✓ Deleted change request: %s\n", changeRequestID)

	// Delete work items directory if it exists
	workItemsDir := filepath.Join(utopiaDir, "work-items", changeRequestID)
	if _, err := os.Stat(workItemsDir); err == nil {
		if err := os.RemoveAll(workItemsDir); err != nil {
			return fmt.Errorf("failed to delete work items: %w", err)
		}
		fmt.Printf("✓ Deleted work items: %s\n", changeRequestID)
	}

	fmt.Println()
	fmt.Printf("Successfully completed refactor: %s (no specs modified)\n", cr.Title)

	return nil
}
