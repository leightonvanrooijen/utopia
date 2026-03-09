package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/leightonvanrooijen/utopia/internal"
	"github.com/leightonvanrooijen/utopia/internal/domain"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var shapeCmd = &cobra.Command{
	Use:   "shape",
	Short: "Validate and refine draft specifications through guided conversation",
	Long: `Walk through draft specifications one at a time, validating and refining them.

The command will:
  1. Load all draft specs from .utopia/drafts/specs/
  2. Present drafts starting with lowest confidence (most uncertain)
  3. For each draft, guide you through validating:
     - Whether the proposed features match your intent
     - Clarifying uncertain areas noted during discovery
     - Confirming, correcting, or rejecting individual features
  4. Update drafts based on your responses
  5. Remove rejected features from drafts

After shaping, validated drafts can be promoted to official specifications
using 'utopia cr' to create change requests.

The shape command is typically run after 'utopia discover' to validate
the automatically discovered draft specifications.`,
	RunE: runShape,
}

func init() {
	rootCmd.AddCommand(shapeCmd)
}

const shapeSystemPrompt = `You are a Shape Claude - an AI assistant that helps validate and refine draft specifications through guided conversation.

## Your Role
Walk through the provided draft specification with the user, asking clarifying questions about uncertain areas and helping them confirm, correct, or reject proposed features.

## Current Draft to Validate
%s

## Guidelines

### Conversation Flow
1. **Present the Draft**: Start by summarizing the draft - its title, description, confidence level, and any uncertainty notes
2. **Address Uncertainties First**: For LOW/MEDIUM confidence drafts, start with the uncertainty notes and ask clarifying questions
3. **Validate Each Feature**: Go through features one by one:
   - Explain what the feature proposes
   - Ask if this matches the user's intent
   - Offer options: confirm, correct (with their input), or reject
4. **Capture Corrections**: When users provide corrections, note exactly what should change
5. **Summarize Changes**: At the end, summarize all changes that will be made

### Asking Good Questions
- Be specific about what you're uncertain about
- Offer concrete options when possible (e.g., "Did you mean A or B?")
- One question at a time - wait for answers before proceeding
- If the user seems unsure, provide context from the evidence (code files, tests)

### Handling User Responses
- **Confirm**: Feature stays as-is
- **Correct**: User provides new description/criteria - capture exactly
- **Reject**: Feature should be removed from the draft

### Output Format
After the conversation, output the updated draft in this EXACT format:

` + "```yaml" + `
shape_result:
  draft_id: "the-draft-id"
  action: "update" | "reject_all" | "no_changes"
  updated_draft:
    # Full updated DraftSpec YAML (only if action is "update")
    id: draft-id
    title: "Updated Title"
    description: |
      Updated description if changed.
    confidence: high|medium|low  # May upgrade if uncertainties resolved
    discovered_from:
      - original-files.go
    uncertainty_notes: []  # Clear resolved uncertainties
    evidence:
      code_files: [...]
      test_files: [...]
      doc_files: [...]
      comments: [...]
    features:
      - id: feature-id
        description: "Confirmed or corrected description"
        acceptance_criteria:
          - "Confirmed or corrected criteria"
    domain_knowledge:
      - "Any domain knowledge"
  removed_features:
    - id: "rejected-feature-id"
      reason: "User stated this doesn't match intent"
  changes_summary:
    - "Changed feature X description to clarify Y"
    - "Removed feature Z per user feedback"
    - "Upgraded confidence from low to medium after clarifying uncertainties"
` + "```" + `

## Important Rules
1. Ask ONE question at a time - wait for user response
2. Don't assume - always verify with the user
3. For LOW confidence drafts, be especially thorough about uncertainties
4. Keep the conversation focused on THIS draft only
5. If ALL features are rejected, action should be "reject_all"
6. If NO changes needed, action should be "no_changes"
7. Capture the user's exact words for corrections - don't paraphrase intent

## Drafts Directory
Updated drafts will be saved to: %s

Begin by presenting the draft and addressing any uncertainty notes first.`

func runShape(cmd *cobra.Command, args []string) error {
	projectDir := GetProjectDir(cmd)
	absPath, err := filepath.Abs(projectDir)
	if err != nil {
		return fmt.Errorf("failed to resolve project path: %w", err)
	}
	utopiaDir := filepath.Join(absPath, ".utopia")
	if _, err := os.Stat(utopiaDir); os.IsNotExist(err) {
		return fmt.Errorf("not a Utopia project (run 'utopia init' first)")
	}

	store := internal.NewYAMLStore(utopiaDir)
	draftsDir := filepath.Join(utopiaDir, "drafts", "specs")
	if err := os.MkdirAll(draftsDir, 0755); err != nil {
		return fmt.Errorf("failed to create drafts/specs directory: %w", err)
	}

	drafts, err := store.ListDrafts()
	if err != nil {
		return fmt.Errorf("failed to load drafts: %w", err)
	}
	if len(drafts) == 0 {
		fmt.Println("No draft specifications found.")
		fmt.Println("Run 'utopia discover' to analyze your codebase and create drafts.")
		return nil
	}

	sortDraftsByConfidence(drafts)
	counts := countDraftsByConfidence(drafts)

	fmt.Println("Starting draft validation session...")
	fmt.Printf("Found %d draft specifications:\n", len(drafts))
	fmt.Printf("  - LOW confidence:    %d (will validate first)\n", counts[domain.DraftConfidenceLow])
	fmt.Printf("  - MEDIUM confidence: %d\n", counts[domain.DraftConfidenceMedium])
	fmt.Printf("  - HIGH confidence:   %d\n", counts[domain.DraftConfidenceHigh])
	fmt.Println()

	ctx := context.Background()
	cli := internal.NewCLI()

	for i, draft := range drafts {
		fmt.Println("═══════════════════════════════════════════════════════════════")
		fmt.Printf("Draft %d of %d: %s\n", i+1, len(drafts), draft.Title)
		fmt.Printf("Confidence: %s\n", strings.ToUpper(string(draft.Confidence)))
		fmt.Println("═══════════════════════════════════════════════════════════════")
		fmt.Println()

		draftYAML, err := yaml.Marshal(draft)
		if err != nil {
			return fmt.Errorf("failed to serialize draft %s: %w", draft.ID, err)
		}

		systemPrompt := fmt.Sprintf(shapeSystemPrompt, string(draftYAML), draftsDir)
		transcript, sessionErr := cli.SessionWithCapture(ctx, systemPrompt)

		if sessionErr != nil {
			fmt.Println()
			fmt.Println("Session interrupted.")
			fmt.Println("Progress saved. Run 'utopia shape' again to continue.")
			return nil
		}

		result, err := parseShapeResult(transcript)
		if err != nil {
			fmt.Printf("Note: Could not parse shape result for %s: %v\n", draft.ID, err)
			fmt.Println("Draft unchanged.")
			continue
		}

		if err := applyShapeResult(store, draft, result); err != nil {
			return fmt.Errorf("failed to apply shape result for %s: %w", draft.ID, err)
		}

		fmt.Println()
		fmt.Println("───────────────────────────────────────────────────────────────")

		if i < len(drafts)-1 {
			fmt.Println()
			fmt.Printf("Completed %d of %d drafts. Press Enter to continue to next draft, or Ctrl+C to exit.\n", i+1, len(drafts))
			fmt.Scanln()
		}
	}

	fmt.Println()
	fmt.Println("═══════════════════════════════════════════════════════════════")
	fmt.Println("                    SHAPING COMPLETE")
	fmt.Println("═══════════════════════════════════════════════════════════════")
	fmt.Println()
	fmt.Println("All drafts have been validated.")
	fmt.Println()
	fmt.Println("Next steps:")
	fmt.Println("  1. Review updated drafts in", draftsDir)
	fmt.Println("  2. Use 'utopia cr' to create change requests from validated drafts")
	fmt.Println()
	return nil
}

func sortDraftsByConfidence(drafts []*domain.DraftSpec) {
	confidenceOrder := map[domain.DraftConfidence]int{
		domain.DraftConfidenceLow: 0, domain.DraftConfidenceMedium: 1, domain.DraftConfidenceHigh: 2,
	}
	sort.Slice(drafts, func(i, j int) bool {
		return confidenceOrder[drafts[i].Confidence] < confidenceOrder[drafts[j].Confidence]
	})
}

func countDraftsByConfidence(drafts []*domain.DraftSpec) map[domain.DraftConfidence]int {
	counts := map[domain.DraftConfidence]int{}
	for _, d := range drafts {
		counts[d.Confidence]++
	}
	return counts
}

// YAML parsing types for shape results
type shapeResult struct {
	DraftID         string        `yaml:"draft_id"`
	Action          string        `yaml:"action"`
	UpdatedDraft    *shapeUpdated `yaml:"updated_draft,omitempty"`
	RemovedFeatures []struct {
		ID     string `yaml:"id"`
		Reason string `yaml:"reason"`
	} `yaml:"removed_features,omitempty"`
	ChangesSummary []string `yaml:"changes_summary,omitempty"`
}

type shapeUpdated struct {
	ID               string   `yaml:"id"`
	Title            string   `yaml:"title"`
	Description      string   `yaml:"description"`
	Confidence       string   `yaml:"confidence"`
	DiscoveredFrom   []string `yaml:"discovered_from,omitempty"`
	UncertaintyNotes []string `yaml:"uncertainty_notes,omitempty"`
	Evidence         struct {
		CodeFiles []string `yaml:"code_files,omitempty"`
		TestFiles []string `yaml:"test_files,omitempty"`
		DocFiles  []string `yaml:"doc_files,omitempty"`
		Comments  []string `yaml:"comments,omitempty"`
	} `yaml:"evidence"`
	Features []struct {
		ID                 string   `yaml:"id"`
		Description        string   `yaml:"description"`
		AcceptanceCriteria []string `yaml:"acceptance_criteria"`
	} `yaml:"features"`
	DomainKnowledge []string `yaml:"domain_knowledge,omitempty"`
}

func parseShapeResult(transcript string) (*shapeResult, error) {
	yamlContent := extractYAMLBlock(transcript)
	if yamlContent == "" {
		return nil, fmt.Errorf("no YAML block found in transcript")
	}
	var wrapper struct {
		ShapeResult shapeResult `yaml:"shape_result"`
	}
	if err := yaml.Unmarshal([]byte(yamlContent), &wrapper); err != nil {
		return nil, fmt.Errorf("failed to parse shape result YAML: %w", err)
	}
	if wrapper.ShapeResult.DraftID == "" && wrapper.ShapeResult.Action == "" {
		return nil, fmt.Errorf("invalid shape result: missing draft_id and action")
	}
	return &wrapper.ShapeResult, nil
}

func applyShapeResult(store *internal.YAMLStore, original *domain.DraftSpec, result *shapeResult) error {
	switch result.Action {
	case "reject_all":
		fmt.Printf("Rejecting draft: %s\n", original.ID)
		if err := store.DeleteDraft(original.ID); err != nil {
			return fmt.Errorf("failed to delete rejected draft: %w", err)
		}
		fmt.Println("Draft removed.")
		return nil
	case "no_changes":
		fmt.Printf("No changes to draft: %s\n", original.ID)
		return nil
	case "update":
		if result.UpdatedDraft == nil {
			return fmt.Errorf("update action but no updated_draft provided")
		}
		updated := convertShapeUpdatedToDraft(result.UpdatedDraft, original)
		if err := store.SaveDraft(updated); err != nil {
			return fmt.Errorf("failed to save updated draft: %w", err)
		}
		fmt.Printf("Updated draft: %s\n", updated.ID)
		if len(result.ChangesSummary) > 0 {
			fmt.Println("Changes:")
			for _, change := range result.ChangesSummary {
				fmt.Printf("  - %s\n", change)
			}
		}
		if len(result.RemovedFeatures) > 0 {
			fmt.Println("Removed features:")
			for _, f := range result.RemovedFeatures {
				fmt.Printf("  - %s: %s\n", f.ID, f.Reason)
			}
		}
		return nil
	default:
		return fmt.Errorf("unknown action: %s", result.Action)
	}
}

func convertShapeUpdatedToDraft(updated *shapeUpdated, original *domain.DraftSpec) *domain.DraftSpec {
	confidence := domain.DraftConfidenceMedium
	switch strings.ToLower(updated.Confidence) {
	case "high":
		confidence = domain.DraftConfidenceHigh
	case "low":
		confidence = domain.DraftConfidenceLow
	}
	draft := &domain.DraftSpec{
		ID: updated.ID, Title: updated.Title, Created: original.Created, Description: updated.Description,
		Confidence: confidence, DiscoveredFrom: updated.DiscoveredFrom, UncertaintyNotes: updated.UncertaintyNotes,
		Evidence:        domain.DraftEvidence{CodeFiles: updated.Evidence.CodeFiles, TestFiles: updated.Evidence.TestFiles, DocFiles: updated.Evidence.DocFiles, Comments: updated.Evidence.Comments},
		DomainKnowledge: updated.DomainKnowledge,
	}
	for _, f := range updated.Features {
		draft.Features = append(draft.Features, domain.Feature{ID: f.ID, Description: f.Description, AcceptanceCriteria: f.AcceptanceCriteria})
	}
	return draft
}

// ============================================================================
// DOMAIN SHAPING - Subcommand for domain document validation
// ============================================================================

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

After shaping, validated drafts can be promoted to official domain documents.`,
	RunE: runShapeDomain,
}

func init() {
	shapeCmd.AddCommand(shapeDomainCmd)
}

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

### Output Format
After the conversation, output the updated draft in this EXACT format:

` + "```yaml" + `
domain_shape_result:
  draft_id: "the-draft-id"
  action: "update" | "reject_all" | "no_changes"
  updated_draft:
    id: draft-id
    title: "Updated Title"
    bounded_context: bounded-context-name
    description: |
      Updated description if changed.
    confidence: high|medium|low
    created: "2024-01-01T00:00:00Z"
    discovered_from:
      - original-files.go
    uncertainty_notes: []
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
` + "```" + `

## Important Rules
1. Ask ONE question at a time - wait for user response
2. Don't assume - always verify with the user
3. For LOW confidence drafts, be especially thorough about uncertainties
4. Keep the conversation focused on THIS bounded context only
5. If ALL terms are rejected, action should be "reject_all"
6. If NO changes needed, action should be "no_changes"

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
	if _, err := os.Stat(utopiaDir); os.IsNotExist(err) {
		return fmt.Errorf("not a Utopia project (run 'utopia init' first)")
	}

	store := internal.NewYAMLStore(utopiaDir)
	draftsDir := filepath.Join(utopiaDir, "drafts", "domain")
	if err := os.MkdirAll(draftsDir, 0755); err != nil {
		return fmt.Errorf("failed to create drafts/domain directory: %w", err)
	}

	drafts, err := store.ListDraftDomainDocs()
	if err != nil {
		return fmt.Errorf("failed to load domain drafts: %w", err)
	}
	if len(drafts) == 0 {
		fmt.Println("No draft domain documents found.")
		fmt.Println("Run 'utopia discover domain' to analyze your codebase and create drafts.")
		return nil
	}

	sortDomainDraftsByConfidence(drafts)
	counts := countDomainDraftsByConfidence(drafts)
	allTerms := collectAllDomainTerms(drafts)

	fmt.Println("Starting domain draft validation session...")
	fmt.Printf("Found %d draft domain documents:\n", len(drafts))
	fmt.Printf("  - LOW confidence:    %d (will validate first)\n", counts[domain.DraftDomainConfidenceLow])
	fmt.Printf("  - MEDIUM confidence: %d\n", counts[domain.DraftDomainConfidenceMedium])
	fmt.Printf("  - HIGH confidence:   %d\n", counts[domain.DraftDomainConfidenceHigh])
	fmt.Println()

	ctx := context.Background()
	cli := internal.NewCLI()

	for i, draft := range drafts {
		fmt.Println("═══════════════════════════════════════════════════════════════")
		fmt.Printf("Domain Draft %d of %d: %s\n", i+1, len(drafts), draft.Title)
		fmt.Printf("Bounded Context: %s\n", draft.BoundedContext)
		fmt.Printf("Confidence: %s\n", strings.ToUpper(string(draft.Confidence)))
		fmt.Printf("Terms: %d\n", len(draft.Terms))
		fmt.Println("═══════════════════════════════════════════════════════════════")
		fmt.Println()

		draftYAML, err := yaml.Marshal(draft)
		if err != nil {
			return fmt.Errorf("failed to serialize draft %s: %w", draft.ID, err)
		}

		otherTerms := buildOtherTermsList(allTerms, draft.ID)
		systemPrompt := fmt.Sprintf(shapeDomainSystemPrompt, string(draftYAML), otherTerms, draftsDir)
		transcript, sessionErr := cli.SessionWithCapture(ctx, systemPrompt)

		if sessionErr != nil {
			fmt.Println()
			fmt.Println("Session interrupted.")
			fmt.Println("Progress saved. Run 'utopia shape domain' again to continue.")
			return nil
		}

		result, err := parseDomainShapeResult(transcript)
		if err != nil {
			fmt.Printf("Note: Could not parse domain shape result for %s: %v\n", draft.ID, err)
			fmt.Println("Draft unchanged.")
			continue
		}

		if err := applyDomainShapeResult(store, draft, result); err != nil {
			return fmt.Errorf("failed to apply domain shape result for %s: %w", draft.ID, err)
		}

		fmt.Println()
		fmt.Println("───────────────────────────────────────────────────────────────")

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

func sortDomainDraftsByConfidence(drafts []*domain.DraftDomainDoc) {
	confidenceOrder := map[domain.DraftDomainConfidence]int{
		domain.DraftDomainConfidenceLow: 0, domain.DraftDomainConfidenceMedium: 1, domain.DraftDomainConfidenceHigh: 2,
	}
	sort.Slice(drafts, func(i, j int) bool {
		return confidenceOrder[drafts[i].Confidence] < confidenceOrder[drafts[j].Confidence]
	})
}

func countDomainDraftsByConfidence(drafts []*domain.DraftDomainDoc) map[domain.DraftDomainConfidence]int {
	counts := map[domain.DraftDomainConfidence]int{}
	for _, d := range drafts {
		counts[d.Confidence]++
	}
	return counts
}

func collectAllDomainTerms(drafts []*domain.DraftDomainDoc) []struct {
	Term, BoundedContext, DraftID string
} {
	var terms []struct{ Term, BoundedContext, DraftID string }
	for _, draft := range drafts {
		for _, term := range draft.Terms {
			terms = append(terms, struct{ Term, BoundedContext, DraftID string }{term.Term, draft.BoundedContext, draft.ID})
		}
	}
	return terms
}

func buildOtherTermsList(allTerms []struct{ Term, BoundedContext, DraftID string }, currentDraftID string) string {
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

// YAML parsing types for domain shape results
type domainShapeResult struct {
	DraftID        string                   `yaml:"draft_id"`
	Action         string                   `yaml:"action"`
	UpdatedDraft   *domainShapeUpdatedDraft `yaml:"updated_draft,omitempty"`
	RemovedTerms   []struct{ Term, Reason string }
	AliasedTerms   []struct{ Term, CanonicalTarget, Reason string }
	MergedTerms    []struct{ FromTerm, IntoTerm, Reason string }
	SplitTerms     []struct {
		OriginalTerm string
		NewTerms     []struct{ Term, Definition string }
		Reason       string
	}
	ChangesSummary []string `yaml:"changes_summary,omitempty"`
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
	Terms []struct {
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
	} `yaml:"terms,omitempty"`
	Entities []struct {
		Name          string `yaml:"name"`
		Description   string `yaml:"description,omitempty"`
		Relationships []struct {
			Type   string `yaml:"type"`
			Target string `yaml:"target"`
		} `yaml:"relationships,omitempty"`
	} `yaml:"entities,omitempty"`
}

func parseDomainShapeResult(transcript string) (*domainShapeResult, error) {
	yamlContent := extractYAMLBlock(transcript)
	if yamlContent == "" {
		return nil, fmt.Errorf("no YAML block found in transcript")
	}
	var wrapper struct {
		DomainShapeResult domainShapeResult `yaml:"domain_shape_result"`
	}
	if err := yaml.Unmarshal([]byte(yamlContent), &wrapper); err != nil {
		return nil, fmt.Errorf("failed to parse domain shape result YAML: %w", err)
	}
	if wrapper.DomainShapeResult.DraftID == "" && wrapper.DomainShapeResult.Action == "" {
		return nil, fmt.Errorf("invalid domain shape result: missing draft_id and action")
	}
	return &wrapper.DomainShapeResult, nil
}

func applyDomainShapeResult(store *internal.YAMLStore, original *domain.DraftDomainDoc, result *domainShapeResult) error {
	switch result.Action {
	case "reject_all":
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
		updated := convertDomainShapeUpdatedToDraft(result.UpdatedDraft, original)
		if err := store.SaveDraftDomainDoc(updated); err != nil {
			return fmt.Errorf("failed to save updated domain draft: %w", err)
		}
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
				var newNames []string
				for _, nt := range t.NewTerms {
					newNames = append(newNames, nt.Term)
				}
				fmt.Printf("  - %s → [%s]: %s\n", t.OriginalTerm, strings.Join(newNames, ", "), t.Reason)
			}
		}
		return nil
	default:
		return fmt.Errorf("unknown action: %s", result.Action)
	}
}

func convertDomainShapeUpdatedToDraft(updated *domainShapeUpdatedDraft, original *domain.DraftDomainDoc) *domain.DraftDomainDoc {
	confidence := domain.DraftDomainConfidenceMedium
	switch strings.ToLower(updated.Confidence) {
	case "high":
		confidence = domain.DraftDomainConfidenceHigh
	case "low":
		confidence = domain.DraftDomainConfidenceLow
	}
	draft := &domain.DraftDomainDoc{
		ID: updated.ID, Title: updated.Title, BoundedContext: updated.BoundedContext, Description: updated.Description,
		Confidence: confidence, Created: original.Created, DiscoveredFrom: updated.DiscoveredFrom, UncertaintyNotes: updated.UncertaintyNotes,
		Evidence: domain.DraftDomainEvidence{TypeFiles: updated.Evidence.TypeFiles, PackageFiles: updated.Evidence.PackageFiles, SchemaFiles: updated.Evidence.SchemaFiles, Comments: updated.Evidence.Comments},
	}
	for _, t := range updated.Terms {
		term := domain.DomainTerm{Term: t.Term, Definition: t.Definition, Canonical: t.Canonical, CodeUsage: t.CodeUsage, Aliases: t.Aliases, CrossContextNote: t.CrossContextNote}
		if t.Evidence != nil {
			term.Evidence = &domain.TermEvidence{Files: t.Evidence.Files, Lines: t.Evidence.Lines}
		}
		draft.Terms = append(draft.Terms, term)
	}
	for _, e := range updated.Entities {
		entity := domain.DomainEntity{Name: e.Name, Description: e.Description}
		for _, r := range e.Relationships {
			entity.Relationships = append(entity.Relationships, domain.EntityRelationship{Type: r.Type, Target: r.Target})
		}
		draft.Entities = append(draft.Entities, entity)
	}
	return draft
}
