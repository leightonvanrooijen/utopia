package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/leightonvanrooijen/utopia/internal/domain"
	"github.com/leightonvanrooijen/utopia/internal/infra/storage"
	"github.com/spf13/cobra"
)

var promoteCmd = &cobra.Command{
	Use:   "promote [draft-id]",
	Short: "Promote a validated draft to an official specification",
	Long: `Promote a draft specification from .utopia/drafts/specs/ to .utopia/specs/.

The promote command:
  1. Loads the specified draft from .utopia/drafts/specs/
  2. Creates a new spec with the draft's content
  3. Removes draft-specific fields (confidence, discovered_from, etc.)
  4. Saves the spec to .utopia/specs/
  5. Deletes the original draft

This is typically run after 'utopia shape' has validated the draft.

Examples:
  utopia promote my-feature      # Promote the 'my-feature' draft
  utopia promote --list          # List available drafts to promote`,
	RunE: runPromote,
}

var promoteListFlag bool

func init() {
	promoteCmd.Flags().BoolVarP(&promoteListFlag, "list", "l", false, "list available drafts")
	rootCmd.AddCommand(promoteCmd)
}

func runPromote(cmd *cobra.Command, args []string) error {
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

	// Handle --list flag
	if promoteListFlag {
		return listDraftsForPromotion(store)
	}

	// Require draft ID argument
	if len(args) == 0 {
		return fmt.Errorf("draft ID required (use --list to see available drafts)")
	}

	draftID := args[0]

	// Load the draft
	draft, err := store.LoadDraft(draftID)
	if err != nil {
		if _, ok := err.(*domain.NotFoundError); ok {
			return fmt.Errorf("draft '%s' not found (use --list to see available drafts)", draftID)
		}
		return fmt.Errorf("failed to load draft: %w", err)
	}

	// Check if spec already exists
	existingSpec, err := store.LoadSpec(draftID)
	if err == nil && existingSpec != nil {
		return fmt.Errorf("spec '%s' already exists - cannot overwrite existing spec", draftID)
	}

	// Convert draft to spec
	spec := draftToSpec(draft)

	// Save the spec
	if err := store.SaveSpec(spec); err != nil {
		return fmt.Errorf("failed to save spec: %w", err)
	}

	// Delete the draft
	if err := store.DeleteDraft(draftID); err != nil {
		// Spec was saved, but draft deletion failed - warn but don't fail
		fmt.Printf("Warning: spec created but failed to delete draft: %v\n", err)
	}

	// Success output
	fmt.Println("═══════════════════════════════════════════════════════════════")
	fmt.Printf("                 DRAFT PROMOTED TO SPEC\n")
	fmt.Println("═══════════════════════════════════════════════════════════════")
	fmt.Println()
	fmt.Printf("  Draft:  %s\n", draft.ID)
	fmt.Printf("  Title:  %s\n", spec.Title)
	fmt.Printf("  Features: %d\n", len(spec.Features))
	fmt.Println()
	fmt.Printf("Spec saved to: .utopia/specs/%s.yaml\n", spec.ID)
	fmt.Printf("Draft removed: .utopia/drafts/specs/%s.yaml\n", draft.ID)
	fmt.Println()

	return nil
}

// draftToSpec converts a DraftSpec to a Spec, removing draft-specific fields
func draftToSpec(draft *domain.DraftSpec) *domain.Spec {
	now := time.Now()

	spec := &domain.Spec{
		ID:              draft.ID,
		Title:           draft.Title,
		Created:         draft.Created,
		Updated:         now,
		Description:     draft.Description,
		DomainKnowledge: draft.DomainKnowledge,
		Features:        draft.Features,
	}

	return spec
}

// listDraftsForPromotion displays available drafts that can be promoted
func listDraftsForPromotion(store *storage.YAMLStore) error {
	drafts, err := store.ListDrafts()
	if err != nil {
		return fmt.Errorf("failed to load drafts: %w", err)
	}

	if len(drafts) == 0 {
		fmt.Println("No drafts available for promotion.")
		fmt.Println("Run 'utopia discover' to analyze your codebase and create drafts.")
		return nil
	}

	fmt.Println("Available drafts for promotion:")
	fmt.Println()

	for _, draft := range drafts {
		fmt.Printf("  %s\n", draft.ID)
		fmt.Printf("    Title:      %s\n", draft.Title)
		fmt.Printf("    Confidence: %s\n", draft.Confidence)
		fmt.Printf("    Features:   %d\n", len(draft.Features))
		fmt.Println()
	}

	fmt.Println("Usage: utopia promote <draft-id>")

	return nil
}
