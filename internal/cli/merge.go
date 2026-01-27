package cli

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
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
  1. Loads the change request from .utopia/change-requests/
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
		return fmt.Errorf("change request not found: %s\n\nCheck .utopia/change-requests/ for available change requests", changeRequestID)
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
	// Separate delete-spec operations from other operations
	var regularChanges []domain.Change
	var deleteSpecChanges []domain.Change
	for _, change := range cr.Changes {
		if change.Operation == "delete-spec" {
			deleteSpecChanges = append(deleteSpecChanges, change)
		} else {
			regularChanges = append(regularChanges, change)
		}
	}

	// Group regular changes by target spec
	changesBySpec := groupChangesBySpec(regularChanges)
	specIDs := sortedSpecIDs(changesBySpec)

	// Count totals including delete-spec
	var totalAdd, totalModify, totalRemove, totalDeleteSpec int
	totalDeleteSpec = len(deleteSpecChanges)

	// Print summary header
	if len(specIDs) > 0 || len(deleteSpecChanges) > 0 {
		fmt.Printf("Target specs: %d\n", len(specIDs)+len(deleteSpecChanges))
		fmt.Println()
		fmt.Println("Changes to apply:")
	}

	// Summarize regular changes per spec
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

	// Summarize delete-spec operations
	for _, change := range deleteSpecChanges {
		fmt.Printf("  %s: DELETE SPEC\n", change.Spec)
		fmt.Printf("    ✗ Delete entire spec file\n")
		if change.Reason != "" {
			fmt.Printf("      Reason: %s\n", change.Reason)
		}
	}

	if len(specIDs) > 0 || len(deleteSpecChanges) > 0 {
		fmt.Println()
	}

	if mergeDryRun {
		fmt.Println("Dry run mode - no changes applied")
		fmt.Printf("\nWould merge %d add, %d modify, %d remove, %d delete-spec operation(s)\n",
			totalAdd, totalModify, totalRemove, totalDeleteSpec)
		return nil
	}

	// Validate delete-spec targets exist before proceeding
	for _, change := range deleteSpecChanges {
		if _, err := store.LoadSpec(change.Spec); err != nil {
			return fmt.Errorf("cannot delete spec %q: spec not found", change.Spec)
		}
	}

	// Load all specs for regular operations (or create for add-only operations)
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

	// Apply regular changes to each spec (in memory first for atomicity)
	for _, specID := range specIDs {
		spec := specs[specID]
		changes := changesBySpec[specID]
		tempCR := &domain.ChangeRequest{Changes: changes}
		if err := tempCR.ApplyChanges(spec); err != nil {
			return fmt.Errorf("failed to apply changes to spec %s: %w", specID, err)
		}
	}

	// Save all modified specs (atomic commit phase)
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

	// Delete specs (after all other operations succeed)
	deletedSpecs := make(map[string]bool)
	for _, change := range deleteSpecChanges {
		if err := store.DeleteSpec(change.Spec); err != nil {
			return fmt.Errorf("failed to delete spec %s: %w", change.Spec, err)
		}
		deletedSpecs[change.Spec] = true
		fmt.Printf("✓ Deleted spec: %s\n", change.Spec)
	}

	// Mark CR as complete before deletion
	cr.Status = domain.ChangeRequestComplete
	if err := store.SaveChangeRequest(cr); err != nil {
		return fmt.Errorf("failed to update CR status: %w", err)
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
	fmt.Printf("  Specs affected: %d\n", len(specIDs)+len(deletedSpecs))
	for _, specID := range specIDs {
		if createdSpecs[specID] {
			fmt.Printf("    - %s (created)\n", specID)
		} else {
			fmt.Printf("    - %s (updated)\n", specID)
		}
	}
	for specID := range deletedSpecs {
		fmt.Printf("    - %s (deleted)\n", specID)
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
		// Separate delete-spec operations from other operations
		var regularChanges []domain.Change
		var deleteSpecChanges []domain.Change
		for _, change := range allChanges {
			if change.Operation == "delete-spec" {
				deleteSpecChanges = append(deleteSpecChanges, change)
			} else {
				regularChanges = append(regularChanges, change)
			}
		}

		// Group regular changes by target spec
		changesBySpec := groupChangesBySpec(regularChanges)
		specIDs := sortedSpecIDs(changesBySpec)

		fmt.Println("Changes to apply:")
		var totalAdd, totalModify, totalRemove, totalDeleteSpec int
		totalDeleteSpec = len(deleteSpecChanges)
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
		for _, change := range deleteSpecChanges {
			fmt.Printf("  %s: DELETE SPEC\n", change.Spec)
		}
		fmt.Println()

		if mergeDryRun {
			fmt.Println("Dry run mode - no changes applied")
			fmt.Printf("\nWould merge %d add, %d modify, %d remove, %d delete-spec operation(s)\n",
				totalAdd, totalModify, totalRemove, totalDeleteSpec)
			return nil
		}

		// Validate delete-spec targets exist before proceeding
		for _, change := range deleteSpecChanges {
			if _, err := store.LoadSpec(change.Spec); err != nil {
				return fmt.Errorf("cannot delete spec %q: spec not found", change.Spec)
			}
		}

		// Load all specs for regular operations (or create for add-only operations)
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

		// Apply regular changes to each spec
		for _, specID := range specIDs {
			spec := specs[specID]
			changes := changesBySpec[specID]
			tempCR := &domain.ChangeRequest{Changes: changes}
			if err := tempCR.ApplyChanges(spec); err != nil {
				return fmt.Errorf("failed to apply changes to spec %s: %w", specID, err)
			}
		}

		// Save all modified specs
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

		// Delete specs (after all other operations succeed)
		for _, change := range deleteSpecChanges {
			if err := store.DeleteSpec(change.Spec); err != nil {
				return fmt.Errorf("failed to delete spec %s: %w", change.Spec, err)
			}
			fmt.Printf("✓ Deleted spec: %s\n", change.Spec)
		}
	}

	if mergeDryRun {
		fmt.Println("Dry run mode - no changes applied")
		return nil
	}

	// Mark CR as complete before deletion
	cr.Status = domain.ChangeRequestComplete
	if err := store.SaveChangeRequest(cr); err != nil {
		return fmt.Errorf("failed to update CR status: %w", err)
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

	// Mark CR as complete before deletion
	cr.Status = domain.ChangeRequestComplete
	if err := store.SaveChangeRequest(cr); err != nil {
		return fmt.Errorf("failed to update CR status: %w", err)
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

// MergeResult contains the outcome of a merge operation for auto-merge flows
type MergeResult struct {
	SpecsModified []string // IDs of specs that were modified/created
	SpecsDeleted  []string // IDs of specs that were deleted
	IsRefactor    bool     // True if this was a refactor (no spec changes)
}

// PerformMerge applies CR changes to specs without deleting the CR or work items.
// This is used by the execute command to auto-merge after successful completion.
// Returns MergeResult on success, or error if merge fails.
// Note: This does NOT delete the CR or work items - caller handles cleanup after git commit.
func PerformMerge(cr *domain.ChangeRequest, store *storage.YAMLStore) (*MergeResult, error) {
	result := &MergeResult{}

	// Refactor CRs don't modify specs
	if cr.Type == domain.CRTypeRefactor {
		result.IsRefactor = true
		return result, nil
	}

	// Initiative CRs have phases
	if cr.Type == domain.CRTypeInitiative {
		return performMergeInitiative(cr, store)
	}

	// Feature/enhancement/removal CRs modify specs
	return performMergeChanges(cr.Changes, store)
}

// performMergeChanges applies a set of changes to specs.
// Used for both regular CRs and initiative phases.
func performMergeChanges(changes []domain.Change, store *storage.YAMLStore) (*MergeResult, error) {
	result := &MergeResult{}

	// Separate delete-spec operations from other operations
	var regularChanges []domain.Change
	var deleteSpecChanges []domain.Change
	for _, change := range changes {
		if change.Operation == "delete-spec" {
			deleteSpecChanges = append(deleteSpecChanges, change)
		} else {
			regularChanges = append(regularChanges, change)
		}
	}

	// Group regular changes by target spec
	changesBySpec := groupChangesBySpec(regularChanges)
	specIDs := sortedSpecIDs(changesBySpec)

	// Validate delete-spec targets exist before proceeding
	for _, change := range deleteSpecChanges {
		if _, err := store.LoadSpec(change.Spec); err != nil {
			return nil, fmt.Errorf("cannot delete spec %q: spec not found", change.Spec)
		}
	}

	// Load all specs for regular operations (or create for add-only operations)
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
				return nil, fmt.Errorf("spec not found: %s (non-add operations require existing spec)", specID)
			}
		}
		specs[specID] = spec
	}

	// Apply regular changes to each spec (in memory first for atomicity)
	for _, specID := range specIDs {
		spec := specs[specID]
		specChanges := changesBySpec[specID]
		tempCR := &domain.ChangeRequest{Changes: specChanges}
		if err := tempCR.ApplyChanges(spec); err != nil {
			return nil, fmt.Errorf("failed to apply changes to spec %s: %w", specID, err)
		}
	}

	// Save all modified specs (atomic commit phase)
	for _, specID := range specIDs {
		if err := store.SaveSpec(specs[specID]); err != nil {
			return nil, fmt.Errorf("failed to save spec %s: %w", specID, err)
		}
		result.SpecsModified = append(result.SpecsModified, specID)
	}

	// Delete specs (after all other operations succeed)
	for _, change := range deleteSpecChanges {
		if err := store.DeleteSpec(change.Spec); err != nil {
			return nil, fmt.Errorf("failed to delete spec %s: %w", change.Spec, err)
		}
		result.SpecsDeleted = append(result.SpecsDeleted, change.Spec)
	}

	return result, nil
}

// performMergeInitiative applies all phase changes from an initiative CR.
func performMergeInitiative(cr *domain.ChangeRequest, store *storage.YAMLStore) (*MergeResult, error) {
	result := &MergeResult{}

	// Check all phases are complete
	for i, phase := range cr.Phases {
		if phase.Status != domain.PhaseStatusComplete {
			return nil, fmt.Errorf("phase %d is not complete (status: %s)", i+1, phase.Status)
		}
	}

	// Collect all changes from non-refactor phases
	var allChanges []domain.Change
	allRefactors := true
	for _, phase := range cr.Phases {
		if phase.Type == domain.CRTypeRefactor {
			continue
		}
		allRefactors = false
		allChanges = append(allChanges, phase.Changes...)
	}

	if allRefactors {
		result.IsRefactor = true
		return result, nil
	}

	if len(allChanges) > 0 {
		phaseResult, err := performMergeChanges(allChanges, store)
		if err != nil {
			return nil, err
		}
		result.SpecsModified = phaseResult.SpecsModified
		result.SpecsDeleted = phaseResult.SpecsDeleted
	}

	return result, nil
}

// CleanupAfterMerge deletes the CR and work items after a successful merge and git commit.
func CleanupAfterMerge(cr *domain.ChangeRequest, crID, utopiaDir string, store *storage.YAMLStore) error {
	// Mark CR as complete before deletion
	cr.Status = domain.ChangeRequestComplete
	if err := store.SaveChangeRequest(cr); err != nil {
		return fmt.Errorf("failed to update CR status: %w", err)
	}

	// Delete the change request
	if err := store.DeleteChangeRequest(crID); err != nil {
		return fmt.Errorf("failed to delete change request: %w", err)
	}

	// Delete work items
	if cr.Type == domain.CRTypeInitiative {
		// Delete work items for all phases
		for i := range cr.Phases {
			phaseWorkDir := filepath.Join(utopiaDir, "work-items", crID, fmt.Sprintf("phase-%d", i))
			if _, err := os.Stat(phaseWorkDir); err == nil {
				if err := os.RemoveAll(phaseWorkDir); err != nil {
					return fmt.Errorf("failed to delete work items for phase %d: %w", i+1, err)
				}
			}
		}
		// Remove parent directory if empty
		crWorkDir := filepath.Join(utopiaDir, "work-items", crID)
		if entries, err := os.ReadDir(crWorkDir); err == nil && len(entries) == 0 {
			os.Remove(crWorkDir)
		}
	} else {
		// Delete work items directory for regular CRs
		workItemsDir := filepath.Join(utopiaDir, "work-items", crID)
		if _, err := os.Stat(workItemsDir); err == nil {
			if err := os.RemoveAll(workItemsDir); err != nil {
				return fmt.Errorf("failed to delete work items: %w", err)
			}
		}
	}

	return nil
}

// GitCommitSpecMerge creates a git commit for spec merge changes.
// Returns nil if commit succeeds, or error describing the failure.
func GitCommitSpecMerge(projectDir string, cr *domain.ChangeRequest, mergeResult *MergeResult) error {
	// Build commit message
	var msg string
	if mergeResult.IsRefactor {
		msg = fmt.Sprintf("spec: merge refactor CR '%s'\n\nNo spec modifications (refactor only).", cr.Title)
	} else {
		msg = fmt.Sprintf("spec: merge CR '%s'", cr.Title)
		if len(mergeResult.SpecsModified) > 0 || len(mergeResult.SpecsDeleted) > 0 {
			msg += "\n\nModified specs:"
			for _, s := range mergeResult.SpecsModified {
				msg += fmt.Sprintf("\n  - %s", s)
			}
			for _, s := range mergeResult.SpecsDeleted {
				msg += fmt.Sprintf("\n  - %s (deleted)", s)
			}
		}
	}

	// Stage spec changes
	specsDir := filepath.Join(projectDir, ".utopia", "specs")
	addCmd := exec.Command("git", "add", specsDir)
	addCmd.Dir = projectDir
	var addStderr bytes.Buffer
	addCmd.Stderr = &addStderr
	if err := addCmd.Run(); err != nil {
		return fmt.Errorf("git add failed: %w (%s)", err, addStderr.String())
	}

	// Check if there are changes to commit
	diffCmd := exec.Command("git", "diff", "--cached", "--quiet")
	diffCmd.Dir = projectDir
	if err := diffCmd.Run(); err == nil {
		// No changes to commit (exit code 0 means no diff)
		return nil
	}

	// Commit
	commitCmd := exec.Command("git", "commit", "-m", msg)
	commitCmd.Dir = projectDir
	var commitStderr bytes.Buffer
	commitCmd.Stderr = &commitStderr
	if err := commitCmd.Run(); err != nil {
		return fmt.Errorf("git commit failed: %w (%s)", err, commitStderr.String())
	}

	return nil
}
