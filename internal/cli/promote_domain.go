package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/leightonvanrooijen/utopia/internal/domain"
	"github.com/leightonvanrooijen/utopia/internal/infra/storage"
	"github.com/spf13/cobra"
)

var promoteDomainCmd = &cobra.Command{
	Use:   "promote-domain [draft-id]",
	Short: "Promote a validated domain draft to an official domain document",
	Long: `Promote a domain draft from .utopia/drafts/domain/ to .utopia/domain/.

The promote-domain command:
  1. Loads the specified draft from .utopia/drafts/domain/
  2. Creates a new domain doc with the draft's content
  3. Removes draft-specific fields (confidence, discovered_from, evidence, etc.)
  4. If a domain doc already exists for this bounded context, merges terms
  5. Saves the domain doc to .utopia/domain/
  6. Deletes the original draft

This is typically run after 'utopia shape domain' has validated the draft.

Examples:
  utopia promote-domain my-context    # Promote the 'my-context' draft
  utopia promote-domain --list        # List available domain drafts to promote`,
	RunE: runPromoteDomain,
}

var promoteDomainListFlag bool

func init() {
	promoteDomainCmd.Flags().BoolVarP(&promoteDomainListFlag, "list", "l", false, "list available domain drafts")
	rootCmd.AddCommand(promoteDomainCmd)
}

func runPromoteDomain(cmd *cobra.Command, args []string) error {
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
	if promoteDomainListFlag {
		return listDomainDraftsForPromotion(store)
	}

	// Require draft ID argument
	if len(args) == 0 {
		return fmt.Errorf("draft ID required (use --list to see available domain drafts)")
	}

	draftID := args[0]

	// Load the draft
	draft, err := store.LoadDraftDomainDoc(draftID)
	if err != nil {
		if _, ok := err.(*domain.NotFoundError); ok {
			return fmt.Errorf("domain draft '%s' not found (use --list to see available drafts)", draftID)
		}
		return fmt.Errorf("failed to load domain draft: %w", err)
	}

	// Check if domain doc already exists for this bounded context
	existingDoc, err := store.LoadDomainDoc(draft.BoundedContext)
	if err == nil && existingDoc != nil {
		// Calculate stats before merge (merge modifies existingDoc in place)
		newTermCount := countNewTerms(existingDoc, draft)
		newEntityCount := countNewEntities(existingDoc, draft)

		// Merge terms from draft into existing doc
		mergedDoc := mergeDomainDocs(existingDoc, draft)

		// Save the merged doc
		if err := store.SaveDomainDoc(mergedDoc); err != nil {
			return fmt.Errorf("failed to save merged domain doc: %w", err)
		}

		// Delete the draft
		if err := store.DeleteDraftDomainDoc(draftID); err != nil {
			fmt.Printf("Warning: domain doc merged but failed to delete draft: %v\n", err)
		}

		// Success output for merge
		fmt.Println("═══════════════════════════════════════════════════════════════")
		fmt.Printf("           DOMAIN DRAFT MERGED INTO EXISTING DOC\n")
		fmt.Println("═══════════════════════════════════════════════════════════════")
		fmt.Println()
		fmt.Printf("  Draft:          %s\n", draft.ID)
		fmt.Printf("  Bounded Context: %s\n", draft.BoundedContext)
		fmt.Printf("  Terms Added:     %d\n", newTermCount)
		fmt.Printf("  Entities Added:  %d\n", newEntityCount)
		fmt.Printf("  Total Terms:     %d\n", len(mergedDoc.Terms))
		fmt.Println()
		fmt.Printf("Domain doc updated: .utopia/domain/%s.yaml\n", mergedDoc.ID)
		fmt.Printf("Draft removed:      .utopia/drafts/domain/%s.yaml\n", draft.ID)
		fmt.Println()

		return nil
	}

	// No existing doc - convert draft to new domain doc
	doc := draftDomainToDoc(draft)

	// Save the domain doc
	if err := store.SaveDomainDoc(doc); err != nil {
		return fmt.Errorf("failed to save domain doc: %w", err)
	}

	// Delete the draft
	if err := store.DeleteDraftDomainDoc(draftID); err != nil {
		// Doc was saved, but draft deletion failed - warn but don't fail
		fmt.Printf("Warning: domain doc created but failed to delete draft: %v\n", err)
	}

	// Success output
	fmt.Println("═══════════════════════════════════════════════════════════════")
	fmt.Printf("            DOMAIN DRAFT PROMOTED TO DOMAIN DOC\n")
	fmt.Println("═══════════════════════════════════════════════════════════════")
	fmt.Println()
	fmt.Printf("  Draft:          %s\n", draft.ID)
	fmt.Printf("  Title:          %s\n", doc.Title)
	fmt.Printf("  Bounded Context: %s\n", doc.BoundedContext)
	fmt.Printf("  Terms:          %d\n", len(doc.Terms))
	fmt.Printf("  Entities:       %d\n", len(doc.Entities))
	fmt.Println()
	fmt.Printf("Domain doc saved to: .utopia/domain/%s.yaml\n", doc.ID)
	fmt.Printf("Draft removed:       .utopia/drafts/domain/%s.yaml\n", draft.ID)
	fmt.Println()

	return nil
}

// draftDomainToDoc converts a DraftDomainDoc to a DomainDoc, removing draft-specific fields.
// Removed fields: Confidence, DiscoveredFrom, UncertaintyNotes, Evidence, and per-term Evidence.
func draftDomainToDoc(draft *domain.DraftDomainDoc) *domain.DomainDoc {
	// Convert terms, stripping Evidence from each
	terms := make([]domain.DomainTerm, len(draft.Terms))
	for i, t := range draft.Terms {
		terms[i] = domain.DomainTerm{
			Term:             t.Term,
			Definition:       t.Definition,
			Canonical:        t.Canonical,
			CodeUsage:        t.CodeUsage,
			Aliases:          t.Aliases,
			CrossContextNote: t.CrossContextNote,
			// Evidence is intentionally omitted
		}
	}

	doc := &domain.DomainDoc{
		ID:             draft.BoundedContext, // Use bounded context as ID (matches existing pattern)
		Title:          draft.Title,
		BoundedContext: draft.BoundedContext,
		Description:    draft.Description,
		Terms:          terms,
		Entities:       draft.Entities,
	}

	return doc
}

// mergeDomainDocs merges terms from a draft into an existing domain doc.
// New terms are added; existing terms are preserved (not overwritten).
func mergeDomainDocs(existing *domain.DomainDoc, draft *domain.DraftDomainDoc) *domain.DomainDoc {
	// Create a map of existing terms for quick lookup
	existingTerms := make(map[string]bool)
	for _, t := range existing.Terms {
		existingTerms[t.Term] = true
	}

	// Add new terms from draft (without Evidence field)
	for _, t := range draft.Terms {
		if !existingTerms[t.Term] {
			existing.Terms = append(existing.Terms, domain.DomainTerm{
				Term:             t.Term,
				Definition:       t.Definition,
				Canonical:        t.Canonical,
				CodeUsage:        t.CodeUsage,
				Aliases:          t.Aliases,
				CrossContextNote: t.CrossContextNote,
				// Evidence is intentionally omitted
			})
		}
	}

	// Create a map of existing entities for quick lookup
	existingEntities := make(map[string]bool)
	for _, e := range existing.Entities {
		existingEntities[e.Name] = true
	}

	// Add new entities from draft
	for _, e := range draft.Entities {
		if !existingEntities[e.Name] {
			existing.Entities = append(existing.Entities, e)
		}
	}

	return existing
}

// countNewTerms returns the number of terms in draft that don't exist in existing doc
func countNewTerms(existing *domain.DomainDoc, draft *domain.DraftDomainDoc) int {
	existingTerms := make(map[string]bool)
	for _, t := range existing.Terms {
		existingTerms[t.Term] = true
	}

	count := 0
	for _, t := range draft.Terms {
		if !existingTerms[t.Term] {
			count++
		}
	}
	return count
}

// countNewEntities returns the number of entities in draft that don't exist in existing doc
func countNewEntities(existing *domain.DomainDoc, draft *domain.DraftDomainDoc) int {
	existingEntities := make(map[string]bool)
	for _, e := range existing.Entities {
		existingEntities[e.Name] = true
	}

	count := 0
	for _, e := range draft.Entities {
		if !existingEntities[e.Name] {
			count++
		}
	}
	return count
}

// listDomainDraftsForPromotion displays available domain drafts that can be promoted
func listDomainDraftsForPromotion(store *storage.YAMLStore) error {
	drafts, err := store.ListDraftDomainDocs()
	if err != nil {
		return fmt.Errorf("failed to load domain drafts: %w", err)
	}

	if len(drafts) == 0 {
		fmt.Println("No domain drafts available for promotion.")
		fmt.Println("Run 'utopia discover domain' to analyze your codebase and create domain drafts.")
		return nil
	}

	fmt.Println("Available domain drafts for promotion:")
	fmt.Println()

	for _, draft := range drafts {
		fmt.Printf("  %s\n", draft.ID)
		fmt.Printf("    Title:          %s\n", draft.Title)
		fmt.Printf("    Bounded Context: %s\n", draft.BoundedContext)
		fmt.Printf("    Confidence:     %s\n", draft.Confidence)
		fmt.Printf("    Terms:          %d\n", len(draft.Terms))
		fmt.Printf("    Entities:       %d\n", len(draft.Entities))
		fmt.Println()
	}

	fmt.Println("Usage: utopia promote-domain <draft-id>")

	return nil
}
