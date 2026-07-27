package cli

import (
	"errors"
	"fmt"
	"time"

	"github.com/leightonvanrooijen/utopia/internal"
	"github.com/leightonvanrooijen/utopia/internal/domain"
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
	_, _, store, err := ResolveProject(cmd)
	if err != nil {
		return err
	}

	if promoteListFlag {
		return listDraftsForPromotion(store)
	}
	if len(args) == 0 {
		return fmt.Errorf("draft ID required (use --list to see available drafts)")
	}

	draftID := args[0]
	draft, err := store.LoadDraft(draftID)
	if err != nil {
		var nfe *domain.NotFoundError
		if errors.As(err, &nfe) {
			return fmt.Errorf("draft '%s' not found (use --list to see available drafts)", draftID)
		}
		return fmt.Errorf("failed to load draft: %w", err)
	}

	existingSpec, err := store.LoadSpec(draftID)
	if err == nil && existingSpec != nil {
		return fmt.Errorf("spec '%s' already exists - cannot overwrite existing spec", draftID)
	}

	spec := draftToSpec(draft)
	if err := store.SaveSpec(spec); err != nil {
		return fmt.Errorf("failed to save spec: %w", err)
	}
	if err := store.DeleteDraft(draftID); err != nil {
		fmt.Printf("Warning: spec created but failed to delete draft: %v\n", err)
	}

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

func draftToSpec(draft *domain.DraftSpec) *domain.Spec {
	return &domain.Spec{
		ID: draft.ID, Title: draft.Title, Created: draft.Created, Updated: time.Now(),
		Description: draft.Description, DomainKnowledge: draft.DomainKnowledge, Features: draft.Features,
	}
}

func listDraftsForPromotion(store *internal.YAMLStore) error {
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

// ============================================================================
// DOMAIN PROMOTION - Subcommand for domain document promotion
// ============================================================================

var promoteDomainCmd = &cobra.Command{
	Use:   "domain [draft-id]",
	Short: "Promote a validated domain draft to an official domain document",
	Long: `Promote a domain draft from .utopia/drafts/domain/ to .utopia/domain/.

The promote domain command:
  1. Loads the specified draft from .utopia/drafts/domain/
  2. Creates a new domain doc with the draft's content
  3. Removes draft-specific fields (confidence, discovered_from, evidence, etc.)
  4. If a domain doc already exists for this bounded context, merges terms
  5. Saves the domain doc to .utopia/domain/
  6. Deletes the original draft

This is typically run after 'utopia shape domain' has validated the draft.

Examples:
  utopia promote domain my-context    # Promote the 'my-context' draft
  utopia promote domain --list        # List available domain drafts to promote`,
	RunE: runPromoteDomain,
}

var promoteDomainListFlag bool

func init() {
	promoteDomainCmd.Flags().BoolVarP(&promoteDomainListFlag, "list", "l", false, "list available domain drafts")
	promoteCmd.AddCommand(promoteDomainCmd)
}

func runPromoteDomain(cmd *cobra.Command, args []string) error {
	_, _, store, err := ResolveProject(cmd)
	if err != nil {
		return err
	}

	if promoteDomainListFlag {
		return listDomainDraftsForPromotion(store)
	}
	if len(args) == 0 {
		return fmt.Errorf("draft ID required (use --list to see available domain drafts)")
	}

	draftID := args[0]
	draft, err := store.LoadDraftDomainDoc(draftID)
	if err != nil {
		var nfe *domain.NotFoundError
		if errors.As(err, &nfe) {
			return fmt.Errorf("domain draft '%s' not found (use --list to see available drafts)", draftID)
		}
		return fmt.Errorf("failed to load domain draft: %w", err)
	}

	existingDoc, err := store.LoadDomainDoc(draft.BoundedContext)
	if err == nil && existingDoc != nil {
		newTermCount := countNewTerms(existingDoc, draft)
		newEntityCount := countNewEntities(existingDoc, draft)
		mergedDoc := mergeDomainDocs(existingDoc, draft)

		if err := store.SaveDomainDoc(mergedDoc); err != nil {
			return fmt.Errorf("failed to save merged domain doc: %w", err)
		}
		if err := store.DeleteDraftDomainDoc(draftID); err != nil {
			fmt.Printf("Warning: domain doc merged but failed to delete draft: %v\n", err)
		}

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

	doc := draftDomainToDoc(draft)
	if err := store.SaveDomainDoc(doc); err != nil {
		return fmt.Errorf("failed to save domain doc: %w", err)
	}
	if err := store.DeleteDraftDomainDoc(draftID); err != nil {
		fmt.Printf("Warning: domain doc created but failed to delete draft: %v\n", err)
	}

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

func draftDomainToDoc(draft *domain.DraftDomainDoc) *domain.DomainDoc {
	terms := make([]domain.DomainTerm, len(draft.Terms))
	for i, t := range draft.Terms {
		terms[i] = domain.DomainTerm{
			Term: t.Term, Definition: t.Definition, Canonical: t.Canonical,
			CodeUsage: t.CodeUsage, Aliases: t.Aliases, CrossContextNote: t.CrossContextNote,
		}
	}
	return &domain.DomainDoc{
		ID: draft.BoundedContext, Title: draft.Title, BoundedContext: draft.BoundedContext,
		Description: draft.Description, Terms: terms, Entities: draft.Entities,
	}
}

func mergeDomainDocs(existing *domain.DomainDoc, draft *domain.DraftDomainDoc) *domain.DomainDoc {
	existingTerms := make(map[string]bool)
	for _, t := range existing.Terms {
		existingTerms[t.Term] = true
	}
	for _, t := range draft.Terms {
		if !existingTerms[t.Term] {
			existing.Terms = append(existing.Terms, domain.DomainTerm{
				Term: t.Term, Definition: t.Definition, Canonical: t.Canonical,
				CodeUsage: t.CodeUsage, Aliases: t.Aliases, CrossContextNote: t.CrossContextNote,
			})
		}
	}

	existingEntities := make(map[string]bool)
	for _, e := range existing.Entities {
		existingEntities[e.Name] = true
	}
	for _, e := range draft.Entities {
		if !existingEntities[e.Name] {
			existing.Entities = append(existing.Entities, e)
		}
	}
	return existing
}

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

func listDomainDraftsForPromotion(store *internal.YAMLStore) error {
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
	fmt.Println("Usage: utopia promote domain <draft-id>")
	return nil
}
