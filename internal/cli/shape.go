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

var shapeCmd = &cobra.Command{
	Use:   "shape",
	Short: "Validate and refine draft specifications through guided conversation",
	Long: `Walk through draft specifications one at a time, validating and refining them.

The command will:
  1. Load all draft specs from .utopia/drafts/
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

// shapeSystemPrompt guides Claude through draft validation conversation
// Use fmt.Sprintf to inject: draftYAML, draftsDir
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

	// Check if initialized
	if _, err := os.Stat(utopiaDir); os.IsNotExist(err) {
		return fmt.Errorf("not a Utopia project (run 'utopia init' first)")
	}

	store := storage.NewYAMLStore(utopiaDir)
	draftsDir := filepath.Join(utopiaDir, "drafts")

	// Ensure drafts directory exists
	if err := os.MkdirAll(draftsDir, 0755); err != nil {
		return fmt.Errorf("failed to create drafts directory: %w", err)
	}

	// Load all drafts
	drafts, err := store.ListDrafts()
	if err != nil {
		return fmt.Errorf("failed to load drafts: %w", err)
	}

	if len(drafts) == 0 {
		fmt.Println("No draft specifications found.")
		fmt.Println("Run 'utopia discover' to analyze your codebase and create drafts.")
		return nil
	}

	// Sort drafts by confidence (lowest first - these need most attention)
	sortDraftsByConfidence(drafts)

	// Count by confidence
	counts := countDraftsByConfidence(drafts)

	// Display summary
	fmt.Println("Starting draft validation session...")
	fmt.Printf("Found %d draft specifications:\n", len(drafts))
	fmt.Printf("  - LOW confidence:    %d (will validate first)\n", counts[domain.DraftConfidenceLow])
	fmt.Printf("  - MEDIUM confidence: %d\n", counts[domain.DraftConfidenceMedium])
	fmt.Printf("  - HIGH confidence:   %d\n", counts[domain.DraftConfidenceHigh])
	fmt.Println()

	// Process each draft
	ctx := context.Background()
	cli := claude.NewCLI()

	for i, draft := range drafts {
		fmt.Println("═══════════════════════════════════════════════════════════════")
		fmt.Printf("Draft %d of %d: %s\n", i+1, len(drafts), draft.Title)
		fmt.Printf("Confidence: %s\n", strings.ToUpper(string(draft.Confidence)))
		fmt.Println("═══════════════════════════════════════════════════════════════")
		fmt.Println()

		// Build draft YAML for Claude
		draftYAML, err := yaml.Marshal(draft)
		if err != nil {
			return fmt.Errorf("failed to serialize draft %s: %w", draft.ID, err)
		}

		systemPrompt := fmt.Sprintf(shapeSystemPrompt, string(draftYAML), draftsDir)

		// Run interactive session for this draft
		transcript, sessionErr := cli.SessionWithCapture(ctx, systemPrompt)

		if sessionErr != nil {
			// Check if user interrupted (Ctrl+C)
			fmt.Println()
			fmt.Println("Session interrupted.")
			fmt.Println("Progress saved. Run 'utopia shape' again to continue.")
			return nil
		}

		// Parse the shape result from the transcript
		result, err := parseShapeResult(transcript)
		if err != nil {
			// If we can't parse the result, log and continue
			fmt.Printf("Note: Could not parse shape result for %s: %v\n", draft.ID, err)
			fmt.Println("Draft unchanged.")
			continue
		}

		// Apply the result
		if err := applyShapeResult(store, draft, result); err != nil {
			return fmt.Errorf("failed to apply shape result for %s: %w", draft.ID, err)
		}

		fmt.Println()
		fmt.Println("───────────────────────────────────────────────────────────────")

		// Ask if user wants to continue to next draft
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

// sortDraftsByConfidence sorts drafts with lowest confidence first
func sortDraftsByConfidence(drafts []*domain.DraftSpec) {
	confidenceOrder := map[domain.DraftConfidence]int{
		domain.DraftConfidenceLow:    0, // First (needs most attention)
		domain.DraftConfidenceMedium: 1,
		domain.DraftConfidenceHigh:   2, // Last (already confident)
	}

	sort.Slice(drafts, func(i, j int) bool {
		return confidenceOrder[drafts[i].Confidence] < confidenceOrder[drafts[j].Confidence]
	})
}

// countDraftsByConfidence returns a map of confidence level to count
func countDraftsByConfidence(drafts []*domain.DraftSpec) map[domain.DraftConfidence]int {
	counts := map[domain.DraftConfidence]int{}
	for _, d := range drafts {
		counts[d.Confidence]++
	}
	return counts
}

// shapeResult represents the parsed output from a shape session
type shapeResult struct {
	DraftID         string        `yaml:"draft_id"`
	Action          string        `yaml:"action"` // "update", "reject_all", "no_changes"
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

type shapeResultWrapper struct {
	ShapeResult shapeResult `yaml:"shape_result"`
}

// parseShapeResult extracts the shape result from Claude's transcript
func parseShapeResult(transcript string) (*shapeResult, error) {
	// Find YAML block in transcript
	yamlContent := extractYAMLBlock(transcript)
	if yamlContent == "" {
		return nil, fmt.Errorf("no YAML block found in transcript")
	}

	var wrapper shapeResultWrapper
	if err := yaml.Unmarshal([]byte(yamlContent), &wrapper); err != nil {
		return nil, fmt.Errorf("failed to parse shape result YAML: %w", err)
	}

	if wrapper.ShapeResult.DraftID == "" && wrapper.ShapeResult.Action == "" {
		return nil, fmt.Errorf("invalid shape result: missing draft_id and action")
	}

	return &wrapper.ShapeResult, nil
}

// applyShapeResult applies the shape result to update or delete the draft
func applyShapeResult(store *storage.YAMLStore, original *domain.DraftSpec, result *shapeResult) error {
	switch result.Action {
	case "reject_all":
		// Delete the draft entirely
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

		// Convert the updated draft back to domain type
		updated := convertShapeUpdatedToDraft(result.UpdatedDraft, original)

		// Save the updated draft
		if err := store.SaveDraft(updated); err != nil {
			return fmt.Errorf("failed to save updated draft: %w", err)
		}

		// Print summary of changes
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

// convertShapeUpdatedToDraft converts the parsed YAML back to a domain.DraftSpec
func convertShapeUpdatedToDraft(updated *shapeUpdated, original *domain.DraftSpec) *domain.DraftSpec {
	confidence := domain.DraftConfidenceMedium
	switch strings.ToLower(updated.Confidence) {
	case "high":
		confidence = domain.DraftConfidenceHigh
	case "low":
		confidence = domain.DraftConfidenceLow
	}

	draft := &domain.DraftSpec{
		ID:               updated.ID,
		Title:            updated.Title,
		Created:          original.Created, // Preserve original creation time
		Description:      updated.Description,
		Confidence:       confidence,
		DiscoveredFrom:   updated.DiscoveredFrom,
		UncertaintyNotes: updated.UncertaintyNotes,
		Evidence: domain.DraftEvidence{
			CodeFiles: updated.Evidence.CodeFiles,
			TestFiles: updated.Evidence.TestFiles,
			DocFiles:  updated.Evidence.DocFiles,
			Comments:  updated.Evidence.Comments,
		},
		DomainKnowledge: updated.DomainKnowledge,
	}

	for _, f := range updated.Features {
		draft.Features = append(draft.Features, domain.Feature{
			ID:                 f.ID,
			Description:        f.Description,
			AcceptanceCriteria: f.AcceptanceCriteria,
		})
	}

	return draft
}
