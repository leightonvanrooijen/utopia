package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/leightonvanrooijen/utopia/internal/domain"
	"github.com/leightonvanrooijen/utopia/internal/infra/claude"
	"github.com/leightonvanrooijen/utopia/internal/infra/storage"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var shapeDomainCmd = &cobra.Command{
	Use:   "domain",
	Short: "Validate and refine draft domain documents through guided conversation",
	Long: `Walk through draft domain documents one bounded context at a time, validating and refining terms.

The command will:
  1. Load all draft domain docs from .utopia/drafts/domain/
  2. Present drafts starting with lowest confidence (most uncertain)
  3. For each draft, guide you through validating:
     - Whether the proposed terms match your domain vocabulary
     - Clarifying uncertain areas noted during discovery
     - Confirming, correcting, or rejecting individual terms
  4. Support term operations:
     - Confirm: Accept term as-is
     - Correct: Update the definition or details
     - Reject: Remove the term from the draft
     - Alias: Mark a term as an alias of another canonical term
     - Merge: Combine two terms that represent the same concept
     - Split: Separate a term with multiple meanings into distinct terms
  5. Update drafts based on your responses

After shaping, validated drafts can be promoted to official domain documents.

The shape domain command is typically run after 'utopia discover domain' to validate
the automatically discovered draft domain documents.`,
	RunE: runShapeDomain,
}

func init() {
	shapeCmd.AddCommand(shapeDomainCmd)
}

// shapeDomainSystemPrompt guides Claude through domain draft validation conversation
// Use fmt.Sprintf to inject: draftYAML, draftsDir, existingTermsList
const shapeDomainSystemPrompt = `You are a Domain Shape Claude - an AI assistant that helps validate and refine draft domain documents through guided conversation.

## Your Role
Walk through the provided draft domain document with the user, asking clarifying questions about uncertain terms and helping them confirm, correct, reject, alias, merge, or split proposed terms.

## Current Draft Domain Document to Validate
%s

## Other Terms in This Session (for alias/merge targets)
%s

## Guidelines

### Conversation Flow
1. **Present the Draft**: Start by summarizing the bounded context - its title, description, confidence level, and any uncertainty notes
2. **Address Uncertainties First**: For LOW/MEDIUM confidence drafts, start with the uncertainty notes and ask clarifying questions
3. **Validate Each Term**: Go through terms one by one:
   - State the term and its proposed definition
   - Note any aliases already suggested
   - Ask if this matches the user's domain vocabulary
   - Offer options: confirm, correct, reject, alias, merge, or split
4. **Handle Special Operations**:
   - **Alias**: If user says a term is an alias, ask which canonical term it should map to
   - **Merge**: If user says two terms are the same concept, confirm which term should be canonical
   - **Split**: If user says a term has multiple meanings, help define each distinct meaning
5. **Validate Entities**: After terms, briefly review any proposed entities
6. **Summarize Changes**: At the end, summarize all changes that will be made

### Asking Good Questions
- Be specific about what you're uncertain about
- Offer concrete options when possible (e.g., "Is 'Order' the canonical term, or should it be 'PurchaseOrder'?")
- One question at a time - wait for answers before proceeding
- If the user seems unsure, provide context from the evidence (source files, code locations)

### Handling User Responses
- **Confirm**: Term stays as-is
- **Correct**: User provides new definition/details - capture exactly
- **Reject**: Term should be removed from the draft
- **Alias**: Term becomes an alias of another canonical term
- **Merge**: Two terms become one (with aliases preserved)
- **Split**: One term becomes multiple distinct terms

### Output Format
After the conversation, output the updated draft in this EXACT format:

` + "```yaml" + `
domain_shape_result:
  draft_id: "the-draft-id"
  action: "update" | "reject_all" | "no_changes"
  updated_draft:
    # Full updated DraftDomainDoc YAML (only if action is "update")
    id: draft-id
    title: "Updated Title"
    bounded_context: bounded-context-name
    description: |
      Updated description if changed.
    confidence: high|medium|low  # May upgrade if uncertainties resolved
    created: "2024-01-01T00:00:00Z"  # Preserve original
    discovered_from:
      - original-files.go
    uncertainty_notes: []  # Clear resolved uncertainties
    evidence:
      type_files: [...]
      package_files: [...]
      schema_files: [...]
      comments: [...]
    terms:
      - term: CanonicalTermName
        definition: "Confirmed or corrected definition"
        canonical: true
        code_usage: "Where this term appears in code"
        aliases:
          - "AlternativeName"
        cross_context_note: "Optional note"
        evidence:
          files:
            - "path/to/file.go"
          lines:
            - "path/to/file.go:42"
    entities:
      - name: EntityName
        description: "What this entity represents"
        relationships:
          - type: contains
            target: OtherEntity
  removed_terms:
    - term: "RejectedTermName"
      reason: "User stated this is not domain vocabulary"
  aliased_terms:
    - term: "AliasTermName"
      canonical_target: "CanonicalTermName"
      reason: "User confirmed this is an alias"
  merged_terms:
    - from_term: "OldTermName"
      into_term: "CanonicalTermName"
      reason: "User confirmed these represent the same concept"
  split_terms:
    - original_term: "AmbiguousTermName"
      new_terms:
        - term: "SpecificTermA"
          definition: "First meaning"
        - term: "SpecificTermB"
          definition: "Second meaning"
      reason: "User clarified distinct meanings"
  changes_summary:
    - "Changed term X definition to clarify Y"
    - "Removed term Z per user feedback"
    - "Merged TermA into TermB as they represent the same concept"
    - "Upgraded confidence from low to medium after clarifying uncertainties"
` + "```" + `

## Important Rules
1. Ask ONE question at a time - wait for user response
2. Don't assume - always verify with the user
3. For LOW confidence drafts, be especially thorough about uncertainties
4. Keep the conversation focused on THIS bounded context only
5. If ALL terms are rejected, action should be "reject_all"
6. If NO changes needed, action should be "no_changes"
7. Capture the user's exact words for corrections - don't paraphrase intent
8. When creating aliases, the alias term is REMOVED from the terms list and added to the canonical term's aliases array
9. When merging, all aliases from both terms should be combined into the surviving canonical term
10. When splitting, the original term is removed and new terms are added

## Drafts Directory
Updated drafts will be saved to: %s

Begin by presenting the bounded context and addressing any uncertainty notes first.`

func runShapeDomain(cmd *cobra.Command, args []string) error {
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
	draftsDir := filepath.Join(utopiaDir, "drafts", "domain")

	// Ensure drafts/domain directory exists
	if err := os.MkdirAll(draftsDir, 0755); err != nil {
		return fmt.Errorf("failed to create drafts/domain directory: %w", err)
	}

	// Load all domain drafts
	drafts, err := store.ListDraftDomainDocs()
	if err != nil {
		return fmt.Errorf("failed to load domain drafts: %w", err)
	}

	if len(drafts) == 0 {
		fmt.Println("No draft domain documents found.")
		fmt.Println("Run 'utopia discover domain' to analyze your codebase and create drafts.")
		return nil
	}

	// Sort drafts by confidence (lowest first - these need most attention)
	sortDomainDraftsByConfidence(drafts)

	// Count by confidence
	counts := countDomainDraftsByConfidence(drafts)

	// Build a list of all terms across all drafts (for alias/merge targets)
	allTerms := collectAllDomainTerms(drafts)

	// Display summary
	fmt.Println("Starting domain draft validation session...")
	fmt.Printf("Found %d draft domain documents:\n", len(drafts))
	fmt.Printf("  - LOW confidence:    %d (will validate first)\n", counts[domain.DraftDomainConfidenceLow])
	fmt.Printf("  - MEDIUM confidence: %d\n", counts[domain.DraftDomainConfidenceMedium])
	fmt.Printf("  - HIGH confidence:   %d\n", counts[domain.DraftDomainConfidenceHigh])
	fmt.Println()

	// Process each draft
	ctx := context.Background()
	cli := claude.NewCLI()

	for i, draft := range drafts {
		fmt.Println("═══════════════════════════════════════════════════════════════")
		fmt.Printf("Domain Draft %d of %d: %s\n", i+1, len(drafts), draft.Title)
		fmt.Printf("Bounded Context: %s\n", draft.BoundedContext)
		fmt.Printf("Confidence: %s\n", strings.ToUpper(string(draft.Confidence)))
		fmt.Printf("Terms: %d\n", len(draft.Terms))
		fmt.Println("═══════════════════════════════════════════════════════════════")
		fmt.Println()

		// Build draft YAML for Claude
		draftYAML, err := yaml.Marshal(draft)
		if err != nil {
			return fmt.Errorf("failed to serialize draft %s: %w", draft.ID, err)
		}

		// Build list of other terms (excluding current draft's terms)
		otherTerms := buildOtherTermsList(allTerms, draft.ID)

		systemPrompt := fmt.Sprintf(shapeDomainSystemPrompt, string(draftYAML), otherTerms, draftsDir)

		// Run interactive session for this draft
		transcript, sessionErr := cli.SessionWithCapture(ctx, systemPrompt)

		if sessionErr != nil {
			// Check if user interrupted (Ctrl+C)
			fmt.Println()
			fmt.Println("Session interrupted.")
			fmt.Println("Progress saved. Run 'utopia shape domain' again to continue.")
			return nil
		}

		// Parse the domain shape result from the transcript
		result, err := parseDomainShapeResult(transcript)
		if err != nil {
			// If we can't parse the result, log and continue
			fmt.Printf("Note: Could not parse domain shape result for %s: %v\n", draft.ID, err)
			fmt.Println("Draft unchanged.")
			continue
		}

		// Apply the result
		if err := applyDomainShapeResult(store, draft, result, drafts); err != nil {
			return fmt.Errorf("failed to apply domain shape result for %s: %w", draft.ID, err)
		}

		fmt.Println()
		fmt.Println("───────────────────────────────────────────────────────────────")

		// Ask if user wants to continue to next draft
		if i < len(drafts)-1 {
			fmt.Println()
			fmt.Printf("Completed %d of %d domain drafts. Press Enter to continue to next draft, or Ctrl+C to exit.\n", i+1, len(drafts))
			fmt.Scanln()
		}
	}

	fmt.Println()
	fmt.Println("═══════════════════════════════════════════════════════════════")
	fmt.Println("                 DOMAIN SHAPING COMPLETE")
	fmt.Println("═══════════════════════════════════════════════════════════════")
	fmt.Println()
	fmt.Println("All domain drafts have been validated.")
	fmt.Println()
	fmt.Println("Next steps:")
	fmt.Println("  1. Review updated drafts in", draftsDir)
	fmt.Println("  2. Use 'utopia promote domain' to move validated drafts to official domain docs")
	fmt.Println()

	return nil
}

// sortDomainDraftsByConfidence sorts drafts with lowest confidence first
func sortDomainDraftsByConfidence(drafts []*domain.DraftDomainDoc) {
	confidenceOrder := map[domain.DraftDomainConfidence]int{
		domain.DraftDomainConfidenceLow:    0, // First (needs most attention)
		domain.DraftDomainConfidenceMedium: 1,
		domain.DraftDomainConfidenceHigh:   2, // Last (already confident)
	}

	sort.Slice(drafts, func(i, j int) bool {
		return confidenceOrder[drafts[i].Confidence] < confidenceOrder[drafts[j].Confidence]
	})
}

// countDomainDraftsByConfidence returns a map of confidence level to count
func countDomainDraftsByConfidence(drafts []*domain.DraftDomainDoc) map[domain.DraftDomainConfidence]int {
	counts := map[domain.DraftDomainConfidence]int{}
	for _, d := range drafts {
		counts[d.Confidence]++
	}
	return counts
}

// termInfo holds basic info about a term for alias/merge references
type termInfo struct {
	Term           string
	BoundedContext string
	DraftID        string
}

// collectAllDomainTerms gathers all terms from all drafts
func collectAllDomainTerms(drafts []*domain.DraftDomainDoc) []termInfo {
	var terms []termInfo
	for _, draft := range drafts {
		for _, term := range draft.Terms {
			terms = append(terms, termInfo{
				Term:           term.Term,
				BoundedContext: draft.BoundedContext,
				DraftID:        draft.ID,
			})
		}
	}
	return terms
}

// buildOtherTermsList creates a formatted list of terms from other drafts
func buildOtherTermsList(allTerms []termInfo, currentDraftID string) string {
	var lines []string
	for _, t := range allTerms {
		if t.DraftID != currentDraftID {
			lines = append(lines, fmt.Sprintf("- %s (context: %s)", t.Term, t.BoundedContext))
		}
	}
	if len(lines) == 0 {
		return "(No other terms in this session)"
	}
	return strings.Join(lines, "\n")
}

// domainShapeResult represents the parsed output from a domain shape session
type domainShapeResult struct {
	DraftID        string                   `yaml:"draft_id"`
	Action         string                   `yaml:"action"` // "update", "reject_all", "no_changes"
	UpdatedDraft   *domainShapeUpdatedDraft `yaml:"updated_draft,omitempty"`
	RemovedTerms   []domainRemovedTerm      `yaml:"removed_terms,omitempty"`
	AliasedTerms   []domainAliasedTerm      `yaml:"aliased_terms,omitempty"`
	MergedTerms    []domainMergedTerm       `yaml:"merged_terms,omitempty"`
	SplitTerms     []domainSplitTerm        `yaml:"split_terms,omitempty"`
	ChangesSummary []string                 `yaml:"changes_summary,omitempty"`
}

type domainShapeUpdatedDraft struct {
	ID               string   `yaml:"id"`
	Title            string   `yaml:"title"`
	BoundedContext   string   `yaml:"bounded_context"`
	Description      string   `yaml:"description"`
	Confidence       string   `yaml:"confidence"`
	Created          string   `yaml:"created"`
	DiscoveredFrom   []string `yaml:"discovered_from,omitempty"`
	UncertaintyNotes []string `yaml:"uncertainty_notes,omitempty"`
	Evidence         struct {
		TypeFiles    []string `yaml:"type_files,omitempty"`
		PackageFiles []string `yaml:"package_files,omitempty"`
		SchemaFiles  []string `yaml:"schema_files,omitempty"`
		Comments     []string `yaml:"comments,omitempty"`
	} `yaml:"evidence"`
	Terms    []domainShapeTerm   `yaml:"terms,omitempty"`
	Entities []domainShapeEntity `yaml:"entities,omitempty"`
}

type domainShapeTerm struct {
	Term             string   `yaml:"term"`
	Definition       string   `yaml:"definition"`
	Canonical        bool     `yaml:"canonical"`
	CodeUsage        string   `yaml:"code_usage,omitempty"`
	Aliases          []string `yaml:"aliases,omitempty"`
	CrossContextNote string   `yaml:"cross_context_note,omitempty"`
	Evidence         *struct {
		Files []string `yaml:"files,omitempty"`
		Lines []string `yaml:"lines,omitempty"`
	} `yaml:"evidence,omitempty"`
}

type domainShapeEntity struct {
	Name          string `yaml:"name"`
	Description   string `yaml:"description,omitempty"`
	Relationships []struct {
		Type   string `yaml:"type"`
		Target string `yaml:"target"`
	} `yaml:"relationships,omitempty"`
}

type domainRemovedTerm struct {
	Term   string `yaml:"term"`
	Reason string `yaml:"reason"`
}

type domainAliasedTerm struct {
	Term            string `yaml:"term"`
	CanonicalTarget string `yaml:"canonical_target"`
	Reason          string `yaml:"reason"`
}

type domainMergedTerm struct {
	FromTerm string `yaml:"from_term"`
	IntoTerm string `yaml:"into_term"`
	Reason   string `yaml:"reason"`
}

type domainSplitTerm struct {
	OriginalTerm string `yaml:"original_term"`
	NewTerms     []struct {
		Term       string `yaml:"term"`
		Definition string `yaml:"definition"`
	} `yaml:"new_terms"`
	Reason string `yaml:"reason"`
}

type domainShapeResultWrapper struct {
	DomainShapeResult domainShapeResult `yaml:"domain_shape_result"`
}

// parseDomainShapeResult extracts the domain shape result from Claude's transcript
func parseDomainShapeResult(transcript string) (*domainShapeResult, error) {
	// Find YAML block in transcript
	yamlContent := extractYAMLBlock(transcript)
	if yamlContent == "" {
		return nil, fmt.Errorf("no YAML block found in transcript")
	}

	var wrapper domainShapeResultWrapper
	if err := yaml.Unmarshal([]byte(yamlContent), &wrapper); err != nil {
		return nil, fmt.Errorf("failed to parse domain shape result YAML: %w", err)
	}

	if wrapper.DomainShapeResult.DraftID == "" && wrapper.DomainShapeResult.Action == "" {
		return nil, fmt.Errorf("invalid domain shape result: missing draft_id and action")
	}

	return &wrapper.DomainShapeResult, nil
}

// applyDomainShapeResult applies the shape result to update or delete the draft
func applyDomainShapeResult(store *storage.YAMLStore, original *domain.DraftDomainDoc, result *domainShapeResult, allDrafts []*domain.DraftDomainDoc) error {
	switch result.Action {
	case "reject_all":
		// Delete the draft entirely
		fmt.Printf("Rejecting domain draft: %s\n", original.ID)
		if err := store.DeleteDraftDomainDoc(original.ID); err != nil {
			return fmt.Errorf("failed to delete rejected domain draft: %w", err)
		}
		fmt.Println("Domain draft removed.")
		return nil

	case "no_changes":
		fmt.Printf("No changes to domain draft: %s\n", original.ID)
		return nil

	case "update":
		if result.UpdatedDraft == nil {
			return fmt.Errorf("update action but no updated_draft provided")
		}

		// Convert the updated draft back to domain type
		updated := convertDomainShapeUpdatedToDraft(result.UpdatedDraft, original)

		// Save the updated draft
		if err := store.SaveDraftDomainDoc(updated); err != nil {
			return fmt.Errorf("failed to save updated domain draft: %w", err)
		}

		// Print summary of changes
		fmt.Printf("Updated domain draft: %s\n", updated.ID)
		if len(result.ChangesSummary) > 0 {
			fmt.Println("Changes:")
			for _, change := range result.ChangesSummary {
				fmt.Printf("  - %s\n", change)
			}
		}
		if len(result.RemovedTerms) > 0 {
			fmt.Println("Removed terms:")
			for _, t := range result.RemovedTerms {
				fmt.Printf("  - %s: %s\n", t.Term, t.Reason)
			}
		}
		if len(result.AliasedTerms) > 0 {
			fmt.Println("Aliased terms:")
			for _, t := range result.AliasedTerms {
				fmt.Printf("  - %s → %s: %s\n", t.Term, t.CanonicalTarget, t.Reason)
			}
		}
		if len(result.MergedTerms) > 0 {
			fmt.Println("Merged terms:")
			for _, t := range result.MergedTerms {
				fmt.Printf("  - %s → %s: %s\n", t.FromTerm, t.IntoTerm, t.Reason)
			}
		}
		if len(result.SplitTerms) > 0 {
			fmt.Println("Split terms:")
			for _, t := range result.SplitTerms {
				newNames := make([]string, len(t.NewTerms))
				for i, nt := range t.NewTerms {
					newNames[i] = nt.Term
				}
				fmt.Printf("  - %s → [%s]: %s\n", t.OriginalTerm, strings.Join(newNames, ", "), t.Reason)
			}
		}
		return nil

	default:
		return fmt.Errorf("unknown action: %s", result.Action)
	}
}

// convertDomainShapeUpdatedToDraft converts the parsed YAML back to a domain.DraftDomainDoc
func convertDomainShapeUpdatedToDraft(updated *domainShapeUpdatedDraft, original *domain.DraftDomainDoc) *domain.DraftDomainDoc {
	confidence := domain.DraftDomainConfidenceMedium
	switch strings.ToLower(updated.Confidence) {
	case "high":
		confidence = domain.DraftDomainConfidenceHigh
	case "low":
		confidence = domain.DraftDomainConfidenceLow
	}

	draft := &domain.DraftDomainDoc{
		ID:               updated.ID,
		Title:            updated.Title,
		BoundedContext:   updated.BoundedContext,
		Description:      updated.Description,
		Confidence:       confidence,
		Created:          original.Created, // Preserve original creation time
		DiscoveredFrom:   updated.DiscoveredFrom,
		UncertaintyNotes: updated.UncertaintyNotes,
		Evidence: domain.DraftDomainEvidence{
			TypeFiles:    updated.Evidence.TypeFiles,
			PackageFiles: updated.Evidence.PackageFiles,
			SchemaFiles:  updated.Evidence.SchemaFiles,
			Comments:     updated.Evidence.Comments,
		},
	}

	// Convert terms
	for _, t := range updated.Terms {
		term := domain.DomainTerm{
			Term:             t.Term,
			Definition:       t.Definition,
			Canonical:        t.Canonical,
			CodeUsage:        t.CodeUsage,
			Aliases:          t.Aliases,
			CrossContextNote: t.CrossContextNote,
		}
		if t.Evidence != nil {
			term.Evidence = &domain.TermEvidence{
				Files: t.Evidence.Files,
				Lines: t.Evidence.Lines,
			}
		}
		draft.Terms = append(draft.Terms, term)
	}

	// Convert entities
	for _, e := range updated.Entities {
		entity := domain.DomainEntity{
			Name:        e.Name,
			Description: e.Description,
		}
		for _, r := range e.Relationships {
			entity.Relationships = append(entity.Relationships, domain.EntityRelationship{
				Type:   r.Type,
				Target: r.Target,
			})
		}
		draft.Entities = append(draft.Entities, entity)
	}

	return draft
}
