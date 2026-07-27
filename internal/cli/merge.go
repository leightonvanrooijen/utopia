package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/leightonvanrooijen/utopia/internal"
	"github.com/leightonvanrooijen/utopia/internal/cli/ui"
	"github.com/leightonvanrooijen/utopia/internal/domain"
	"github.com/spf13/cobra"
)

// Flag for merge command (package-level for Cobra compatibility)
var mergeDryRunFlag bool

func init() {
	rootCmd.AddCommand(newMergeCmd())
}

// newMergeCmd creates the merge command with flag bindings.
func newMergeCmd() *cobra.Command {
	cmd := &cobra.Command{
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
		RunE: func(cmd *cobra.Command, args []string) error {
			return runMerge(cmd, args, mergeDryRunFlag)
		},
	}

	cmd.Flags().BoolVar(&mergeDryRunFlag, "dry-run", false,
		"Preview changes without applying them")

	return cmd
}

func runMerge(cmd *cobra.Command, args []string, dryRun bool) error {
	out := ui.NewPrinter(cmd.OutOrStdout(), cmd.ErrOrStderr())
	changeRequestID := args[0]

	_, utopiaDir, store, err := ResolveProject(cmd)
	if err != nil {
		return err
	}

	// Load the change request
	cr, err := store.LoadChangeRequest(changeRequestID)
	if err != nil {
		return &domain.NotFoundError{Resource: "change request", ID: changeRequestID}
	}

	out.Printf("Change Request: %s\n", cr.Title)
	out.Printf("Type: %s\n", cr.Type)

	// Refactor and bugfix CRs don't modify specs - just delete the CR
	if cr.Type == domain.CRTypeRefactor || cr.Type == domain.CRTypeBugfix {
		return mergeRefactor(out, cr, changeRequestID, utopiaDir, store, dryRun)
	}

	// Initiative CRs have phases that each need to be merged
	if cr.Type == domain.CRTypeInitiative {
		return mergeInitiative(out, cr, changeRequestID, utopiaDir, store, dryRun)
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
		out.Printf("Target specs: %d\n\n", len(specIDs)+len(deleteSpecChanges))
		out.Printf("Changes to apply:\n")
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
		out.Printf("  %s: +%d ~%d -%d\n", specID, addCount, modifyCount, removeCount)
		for _, change := range changes {
			switch change.Operation {
			case "add":
				if change.Feature != nil {
					out.Printf("    + Add feature: %s\n", change.Feature.ID)
				}
				if len(change.DomainKnowledge) > 0 {
					out.Printf("    + Add %d domain knowledge item(s)\n", len(change.DomainKnowledge))
				}
			case "modify":
				if change.FeatureID != "" {
					out.Printf("    ~ Modify feature: %s\n", change.FeatureID)
				}
				if change.DomainKnowledgeMod != nil {
					out.Printf("    ~ Modify domain knowledge\n")
				}
			case "remove":
				if change.FeatureID != "" {
					out.Printf("    "+ui.Bullet+" Remove feature: %s\n", change.FeatureID)
				}
				if len(change.DomainKnowledge) > 0 {
					out.Printf("    "+ui.Bullet+" Remove %d domain knowledge item(s)\n", len(change.DomainKnowledge))
				}
			}
		}
	}

	// Summarize delete-spec operations
	for _, change := range deleteSpecChanges {
		out.Printf("  %s: DELETE SPEC\n", change.Spec)
		out.Printf("    %s Delete entire spec file\n", ui.Failure)
		if change.Reason != "" {
			out.Printf("      Reason: %s\n", change.Reason)
		}
	}

	if len(specIDs) > 0 || len(deleteSpecChanges) > 0 {
		out.Printf("\n")
	}

	if dryRun {
		out.Printf("Dry run mode - no changes applied\n")
		out.Printf("\nWould merge %d add, %d modify, %d remove, %d delete-spec operation(s)\n",
			totalAdd, totalModify, totalRemove, totalDeleteSpec)
		return nil
	}

	// Filter out delete-spec targets that no longer exist (idempotent merge)
	var validDeleteSpecs []domain.Change
	for _, change := range deleteSpecChanges {
		if _, err := store.LoadSpec(change.Spec); err != nil {
			out.Warnf("Spec %q not found (already deleted?), skipping", change.Spec)
			continue
		}
		validDeleteSpecs = append(validDeleteSpecs, change)
	}
	deleteSpecChanges = validDeleteSpecs

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
				return &domain.NotFoundError{Resource: "spec", ID: specID}
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
			return fmt.Errorf("failed to save spec %s: %w - some specs may have been saved, manual cleanup may be required", specID, err)
		}
		if createdSpecs[specID] {
			out.Successf("Created spec: %s", specID)
		} else {
			out.Successf("Updated spec: %s", specID)
		}
	}

	// Delete specs (after all other operations succeed)
	deletedSpecs := make(map[string]bool)
	for _, change := range deleteSpecChanges {
		if err := store.DeleteSpec(change.Spec); err != nil {
			return fmt.Errorf("failed to delete spec %s: %w", change.Spec, err)
		}
		deletedSpecs[change.Spec] = true
		out.Successf("Deleted spec: %s", change.Spec)
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
	out.Successf("Deleted change request: %s", changeRequestID)

	// Delete work items directory if it exists
	workItemsDir := filepath.Join(utopiaDir, "work-items", changeRequestID)
	if _, err := os.Stat(workItemsDir); err == nil {
		if err := os.RemoveAll(workItemsDir); err != nil {
			return fmt.Errorf("failed to delete work items: %w", err)
		}
		out.Successf("Deleted work items: %s", changeRequestID)
	}

	// Print summary
	out.Printf("\nMerge Summary:\n")
	out.Printf("  Total changes: %d\n", len(cr.Changes))
	out.Printf("  Specs affected: %d\n", len(specIDs)+len(deletedSpecs))
	for _, specID := range specIDs {
		if createdSpecs[specID] {
			out.Printf("    "+ui.Bullet+" %s (created)\n", specID)
		} else {
			out.Printf("    "+ui.Bullet+" %s (updated)\n", specID)
		}
	}
	for specID := range deletedSpecs {
		out.Printf("    "+ui.Bullet+" %s (deleted)\n", specID)
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
func mergeInitiative(out *ui.Printer, cr *domain.ChangeRequest, changeRequestID, utopiaDir string, store *internal.YAMLStore, dryRun bool) error {
	out.Printf("Phases: %d total\n", len(cr.Phases))

	// Check all phases are complete
	for i, phase := range cr.Phases {
		if phase.Status != domain.PhaseStatusComplete {
			return fmt.Errorf("phase %d is not complete (status: %s)\n\nComplete all phases before merging", i+1, phase.Status)
		}
	}

	out.Printf("\n")

	// Collect all changes from non-refactor/non-bugfix phases
	var allChanges []domain.Change
	var taskOnlyPhaseCount int
	for i, phase := range cr.Phases {
		if phase.Type == domain.CRTypeRefactor || phase.Type == domain.CRTypeBugfix {
			taskOnlyPhaseCount++
			out.Printf("Phase %d (%s): %d tasks - no spec changes\n", i+1, phase.Type, len(phase.Tasks))
			continue
		}
		out.Printf("Phase %d (%s): %d changes\n", i+1, phase.Type, len(phase.Changes))
		allChanges = append(allChanges, phase.Changes...)
	}
	out.Printf("\n")

	if len(allChanges) == 0 && taskOnlyPhaseCount == len(cr.Phases) {
		// All phases were refactors/bugfixes - just clean up
		out.Printf("All phases were refactors/bugfixes - no spec modifications needed\n")
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

		out.Printf("Changes to apply:\n")
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
			out.Printf("  %s: +%d ~%d -%d\n", specID, addCount, modifyCount, removeCount)
		}
		for _, change := range deleteSpecChanges {
			out.Printf("  %s: DELETE SPEC\n", change.Spec)
		}
		out.Printf("\n")

		if dryRun {
			out.Printf("Dry run mode - no changes applied\n")
			out.Printf("\nWould merge %d add, %d modify, %d remove, %d delete-spec operation(s)\n",
				totalAdd, totalModify, totalRemove, totalDeleteSpec)
			return nil
		}

		// Filter out delete-spec targets that no longer exist (idempotent merge)
		var validDeleteSpecs []domain.Change
		for _, change := range deleteSpecChanges {
			if _, err := store.LoadSpec(change.Spec); err != nil {
				out.Warnf("Spec %q not found (already deleted?), skipping", change.Spec)
				continue
			}
			validDeleteSpecs = append(validDeleteSpecs, change)
		}
		deleteSpecChanges = validDeleteSpecs

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
					return &domain.NotFoundError{Resource: "spec", ID: specID}
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
				out.Successf("Created spec: %s", specID)
			} else {
				out.Successf("Updated spec: %s", specID)
			}
		}

		// Delete specs (after all other operations succeed)
		for _, change := range deleteSpecChanges {
			if err := store.DeleteSpec(change.Spec); err != nil {
				return fmt.Errorf("failed to delete spec %s: %w", change.Spec, err)
			}
			out.Successf("Deleted spec: %s", change.Spec)
		}
	}

	if dryRun {
		out.Printf("Dry run mode - no changes applied\n")
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
	out.Successf("Deleted change request: %s", changeRequestID)

	// Delete work items for all phases
	for i := range cr.Phases {
		phaseWorkDir := filepath.Join(utopiaDir, "work-items", changeRequestID, fmt.Sprintf("phase-%d", i))
		if _, err := os.Stat(phaseWorkDir); err == nil {
			if err := os.RemoveAll(phaseWorkDir); err != nil {
				return fmt.Errorf("failed to delete work items for phase %d: %w", i+1, err)
			}
			out.Successf("Deleted work items: phase %d", i+1)
		}
	}

	// Also delete the parent work-items directory for this CR if it's empty
	crWorkDir := filepath.Join(utopiaDir, "work-items", changeRequestID)
	if entries, err := os.ReadDir(crWorkDir); err == nil && len(entries) == 0 {
		os.Remove(crWorkDir)
	}

	out.Printf("\nSuccessfully merged initiative: %s\n", cr.Title)

	return nil
}

// mergeRefactor handles merge for refactor CRs, which don't modify specs.
// Refactors only restructure code while preserving behavior, so merge
// simply deletes the CR and its work items.
func mergeRefactor(out *ui.Printer, cr *domain.ChangeRequest, changeRequestID, utopiaDir string, store *internal.YAMLStore, dryRun bool) error {
	out.Printf("Tasks completed: %d\n\n", len(cr.Tasks))

	// Summarize tasks
	out.Printf("Completed tasks:\n")
	for _, task := range cr.Tasks {
		out.Printf("  %s %s: %s\n", ui.Success, task.ID, task.Description)
	}
	out.Printf("\n")

	if dryRun {
		out.Printf("Dry run mode - no changes applied\n")
		out.Printf("\nWould delete refactor CR: %s (no specs modified)\n", changeRequestID)
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
	out.Successf("Deleted change request: %s", changeRequestID)

	// Delete work items directory if it exists
	workItemsDir := filepath.Join(utopiaDir, "work-items", changeRequestID)
	if _, err := os.Stat(workItemsDir); err == nil {
		if err := os.RemoveAll(workItemsDir); err != nil {
			return fmt.Errorf("failed to delete work items: %w", err)
		}
		out.Successf("Deleted work items: %s", changeRequestID)
	}

	out.Printf("\nSuccessfully completed refactor: %s (no specs modified)\n", cr.Title)

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
func PerformMerge(out *ui.Printer, cr *domain.ChangeRequest, store *internal.YAMLStore) (*MergeResult, error) {
	result := &MergeResult{}

	// Refactor and bugfix CRs don't modify specs
	if cr.Type == domain.CRTypeRefactor || cr.Type == domain.CRTypeBugfix {
		result.IsRefactor = true
		return result, nil
	}

	// Initiative CRs have phases
	if cr.Type == domain.CRTypeInitiative {
		return performMergeInitiative(out, cr, store)
	}

	// Feature/enhancement/removal CRs modify specs
	return performMergeChanges(out, cr.Changes, store)
}

// performMergeChanges applies a set of changes to specs.
// Used for both regular CRs and initiative phases.
func performMergeChanges(out *ui.Printer, changes []domain.Change, store *internal.YAMLStore) (*MergeResult, error) {
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

	// Filter out delete-spec targets that no longer exist (idempotent merge)
	var validDeleteSpecs []domain.Change
	for _, change := range deleteSpecChanges {
		if _, err := store.LoadSpec(change.Spec); err != nil {
			out.Warnf("Spec %q not found (already deleted?), skipping", change.Spec)
			continue
		}
		validDeleteSpecs = append(validDeleteSpecs, change)
	}
	deleteSpecChanges = validDeleteSpecs

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
				return nil, &domain.NotFoundError{Resource: "spec", ID: specID}
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
func performMergeInitiative(out *ui.Printer, cr *domain.ChangeRequest, store *internal.YAMLStore) (*MergeResult, error) {
	result := &MergeResult{}

	// Check all phases are complete
	for i, phase := range cr.Phases {
		if phase.Status != domain.PhaseStatusComplete {
			return nil, fmt.Errorf("phase %d is not complete (status: %s)", i+1, phase.Status)
		}
	}

	// Collect all changes from non-refactor/non-bugfix phases
	var allChanges []domain.Change
	allTaskOnly := true
	for _, phase := range cr.Phases {
		if phase.Type == domain.CRTypeRefactor || phase.Type == domain.CRTypeBugfix {
			continue
		}
		allTaskOnly = false
		allChanges = append(allChanges, phase.Changes...)
	}

	if allTaskOnly {
		result.IsRefactor = true
		return result, nil
	}

	if len(allChanges) > 0 {
		phaseResult, err := performMergeChanges(out, allChanges, store)
		if err != nil {
			return nil, err
		}
		result.SpecsModified = phaseResult.SpecsModified
		result.SpecsDeleted = phaseResult.SpecsDeleted
	}

	return result, nil
}

// AutoMergeCR performs the merge after all work items complete successfully.
// It applies CR changes to specs, creates a git commit, then cleans up CR/work items.
// On failure, work item completion state is preserved for manual retry.
func AutoMergeCR(out *ui.Printer, cr *domain.ChangeRequest, crID string, store *internal.YAMLStore, projectDir, utopiaDir string) error {
	// Step 1: Apply changes to specs (without deleting CR/work items)
	mergeResult, err := PerformMerge(out, cr, store)
	if err != nil {
		return fmt.Errorf("failed to apply spec changes: %w", err)
	}

	// Print merge summary
	if mergeResult.IsRefactor {
		out.Printf("Refactor CR - no spec modifications\n")
	} else {
		for _, specID := range mergeResult.SpecsModified {
			out.Successf("Updated spec: %s", specID)
		}
		for _, specID := range mergeResult.SpecsDeleted {
			out.Successf("Deleted spec: %s", specID)
		}
	}

	// Step 2: Create git commit for spec changes
	if err := GitCommitSpecMerge(projectDir, cr, mergeResult); err != nil {
		return fmt.Errorf("failed to create git commit: %w", err)
	}
	out.Successf("Created git commit for spec merge")

	// Step 3: Clean up CR and work items (now safe - commit exists for rollback)
	if err := CleanupAfterMerge(cr, crID, utopiaDir, store); err != nil {
		// Log but don't fail - commit succeeded, cleanup is non-critical
		out.Warnf("Cleanup warning: %s", err)
	} else {
		out.Successf("Cleaned up CR and work items")

		// Step 4: Create cleanup commit for removed CR and work items
		if err := gitCommitCleanup(projectDir, crID, utopiaDir); err != nil {
			out.Warnf("Cleanup commit warning: %s", err)
		} else {
			out.Successf("Created cleanup commit")
		}
	}

	// Step 5: Mark conversations that reference this CR as ready for harvest
	// Transitions pending-execution → unprocessed so harvest can find them
	if err := store.MarkConversationsReadyForHarvest(crID); err != nil {
		out.Warnf("Failed to update conversation status: %s", err)
	}

	out.Printf("\nSuccessfully merged: %s\n", cr.Title)
	return nil
}

// CleanupAfterMerge deletes the CR and work items after a successful merge and git commit.
func CleanupAfterMerge(cr *domain.ChangeRequest, crID, utopiaDir string, store *internal.YAMLStore) error {
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
