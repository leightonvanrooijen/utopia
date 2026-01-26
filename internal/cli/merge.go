package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/leightonvanrooijen/utopia/internal/infra/storage"
	"github.com/spf13/cobra"
)

var (
	mergeDryRun bool
)

var mergeCmd = &cobra.Command{
	Use:   "merge <change-request-id>",
	Short: "Merge a change request into its parent spec",
	Long: `Merge a completed change request into its parent specification.

This command:
  1. Loads the change request from .utopia/specs/_changerequests/
  2. Loads the parent spec referenced by the change request
  3. Applies all changes (add, modify, remove operations)
  4. Saves the updated parent spec
  5. Deletes the change request and its work items

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
	fmt.Printf("Parent Spec: %s\n", cr.ParentSpec)
	fmt.Println()

	// Load the parent spec
	parentSpec, err := store.LoadSpec(cr.ParentSpec)
	if err != nil {
		return fmt.Errorf("parent spec not found: %s\n\nThe change request references a spec that doesn't exist", cr.ParentSpec)
	}

	// Summarize changes
	fmt.Println("Changes to apply:")
	var addCount, modifyCount, removeCount int
	for _, change := range cr.Changes {
		switch change.Operation {
		case "add":
			addCount++
			if change.Feature != nil {
				fmt.Printf("  + Add feature: %s\n", change.Feature.ID)
			}
			if len(change.DomainKnowledge) > 0 {
				fmt.Printf("  + Add %d domain knowledge item(s)\n", len(change.DomainKnowledge))
			}
		case "modify":
			modifyCount++
			if change.FeatureID != "" {
				fmt.Printf("  ~ Modify feature: %s\n", change.FeatureID)
			}
			if change.DomainKnowledgeMod != nil {
				fmt.Printf("  ~ Modify domain knowledge\n")
			}
		case "remove":
			removeCount++
			if change.FeatureID != "" {
				fmt.Printf("  - Remove feature: %s\n", change.FeatureID)
			}
			if len(change.DomainKnowledge) > 0 {
				fmt.Printf("  - Remove %d domain knowledge item(s)\n", len(change.DomainKnowledge))
			}
		}
	}
	fmt.Println()

	if mergeDryRun {
		fmt.Println("Dry run mode - no changes applied")
		fmt.Printf("\nWould merge %d add, %d modify, %d remove operation(s) into %s\n",
			addCount, modifyCount, removeCount, cr.ParentSpec)
		return nil
	}

	// Apply the changes
	if err := cr.ApplyChanges(parentSpec); err != nil {
		return fmt.Errorf("failed to apply changes: %w", err)
	}

	// Save the updated parent spec
	if err := store.SaveSpec(parentSpec); err != nil {
		return fmt.Errorf("failed to save updated spec: %w", err)
	}
	fmt.Printf("✓ Updated spec: %s\n", cr.ParentSpec)

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

	fmt.Println()
	fmt.Printf("Successfully merged %d change(s) into %s\n", len(cr.Changes), cr.ParentSpec)

	return nil
}
