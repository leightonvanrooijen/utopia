package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/leightonvanrooijen/utopia/internal/domain"
	"github.com/leightonvanrooijen/utopia/internal/infra/claude"
	"github.com/leightonvanrooijen/utopia/internal/infra/storage"
	"github.com/spf13/cobra"
)

var crCmd = &cobra.Command{
	Use:   "cr",
	Short: "Create a change request via guided conversation",
	Long: `Start a conversation with Claude to create a change request.

Change requests define modifications to existing specifications:
  - feature:     Add new capability to an existing spec
  - enhancement: Modify how an existing feature works
  - refactor:    Code improvement without behavior change
  - removal:     Delete an existing capability
  - initiative:  Multi-phase changes with ordered execution

Claude will guide you through defining the change by:
  1. Understanding what you want to change
  2. Determining the appropriate CR type
  3. Capturing specific changes with acceptance criteria

The resulting change request is saved to .utopia/change-requests/

Tip: Run a file watcher to see updates in real-time:
  watch -n 1 'ls -la .utopia/change-requests/'`,
	RunE: runCR,
}

func init() {
	rootCmd.AddCommand(crCmd)
}

// crSystemPrompt guides Claude through change request creation
// Use fmt.Sprintf to inject: specsSummary, existingCRsSummary, changeRequestsDir
const crSystemPrompt = `You are a Change Request Claude - an AI assistant that helps users create structured change requests for existing specifications.

## Your Role
Guide users through a natural conversation to create change requests. You understand the existing specs and help users define precise changes to them.

## Existing Specifications
These are the specs you can create change requests for:

%s

## Existing Change Requests
These change requests already exist (avoid duplicates):

%s

## The Journey

### PHASE 1: UNDERSTAND
Start by understanding what the user wants to change:
- What are you trying to modify or improve?
- Which existing spec does this relate to?
- Ask ONE question at a time

### PHASE 2: CLASSIFY
Determine the CR type by asking: "Does this change observable behavior?"

| Intent Signal | CR Type | Key Question |
|--------------|---------|--------------|
| Behavior unchanged (code cleanup, restructure) | refactor | "Will the system behave exactly the same afterward?" |
| New capability that doesn't exist | feature | "Is this something users can't do today?" |
| Modifying how existing capability works | enhancement | "Are we changing how an existing feature behaves?" |
| Removing existing capability | removal | "Are we deleting something that currently exists?" |
| Multiple ordered changes with dependencies | initiative | "Does this require phased execution?" |

**When intent is ambiguous, ask clarifying questions:**
- "To help me classify this correctly: will users notice any difference in behavior, or is this purely a code improvement?"
- "Is this adding something new, or changing how something existing works?"
- "Does this need to happen in phases, or can all changes be applied together?"

Tell the user your assessment: "This sounds like a [TYPE] change request to [SPEC]. Does that match your understanding?"

### PHASE 3: SPECIFY
Capture the specific changes with testable acceptance criteria:
- For features: What's the new capability? What are the acceptance criteria?
- For enhancements: What existing feature? How should it change?
- For refactors: What code improvement? How do we verify behavior is preserved?
- For removals: What's being removed? Why?
- For initiatives: What are the phases? What's the execution order?

### PHASE 4: SAVE
Write the change request file using the appropriate format below.

## Output Formats

Save to: %s/{cr-id}.yaml

### Feature CR (new capability)
` + "```yaml" + `
id: spec-id-add-feature-name
type: feature
title: Add new capability
status: draft
changes:
  - operation: add
    spec: target-spec-id  # REQUIRED: Which spec to add to
    feature:
      id: new-feature-id
      description: What this feature does
      acceptance_criteria:
        - Specific testable condition
` + "```" + `

### Enhancement CR (modify existing capability)
` + "```yaml" + `
id: spec-id-enhance-feature-name
type: enhancement
title: Enhance existing feature
status: draft
changes:
  - operation: modify
    spec: target-spec-id  # REQUIRED: Which spec to modify
    feature_id: existing-feature-id
    description: Updated description  # Optional
    criteria:
      add: ["New criterion"]
      remove: ["Exact text to remove"]
      edit:
        - old: "Exact old text"
          new: "Replacement text"
` + "```" + `

### Refactor CR (behavior unchanged)
` + "```yaml" + `
id: spec-id-refactor-description
type: refactor
title: Refactor without behavior change
status: draft
tasks:  # Note: tasks, not changes (refactors don't modify specs)
  - id: task-id
    description: What needs to be refactored
    acceptance_criteria:
      - Existing behavior is preserved
      - Code improvement is achieved
` + "```" + `

### Removal CR (delete capability)
` + "```yaml" + `
id: spec-id-remove-feature-name
type: removal
title: Remove deprecated feature
status: draft
changes:
  - operation: remove
    spec: target-spec-id  # REQUIRED: Which spec to remove from
    feature_id: feature-to-remove
    reason: Why this is being removed
` + "```" + `

### Initiative CR (multi-phase)
` + "```yaml" + `
id: initiative-name
type: initiative
title: Multi-phase change
status: draft
phases:
  - type: refactor  # First phase: prepare
    tasks:
      - id: task-id
        description: Preparation task
        acceptance_criteria:
          - Criterion
  - type: feature  # Second phase: add capability
    changes:
      - operation: add
        spec: target-spec-id  # REQUIRED for non-refactor phases
        feature:
          id: feature-id
          description: Feature description
          acceptance_criteria:
            - Criterion
` + "```" + `

## Critical Guidelines
- Ask ONE question at a time - keep the conversation focused
- Verify the target spec exists before creating the CR
- For modify/remove operations, text must match EXACTLY (no fuzzy matching)
- Acceptance criteria must be testable (not vague)
- ALWAYS use the Write tool with the path: %s/{cr-id}.yaml
- CR IDs should be kebab-case and descriptive

Start by warmly greeting the user and asking what change they'd like to make.`

func runCR(cmd *cobra.Command, args []string) error {
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

	// Load config to validate project
	store := storage.NewYAMLStore(utopiaDir)
	_, err = store.LoadConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Create change requests directory if it doesn't exist
	changeRequestsDir := filepath.Join(utopiaDir, "change-requests")
	if err := os.MkdirAll(changeRequestsDir, 0755); err != nil {
		return fmt.Errorf("failed to create change requests directory: %w", err)
	}

	// Load existing specs for context
	existingSpecs, err := store.ListSpecs()
	if err != nil {
		// Non-fatal - continue with empty spec list
		existingSpecs = []*domain.Spec{}
	}

	// Load existing change requests to avoid duplicates
	existingCRs, err := store.ListChangeRequests()
	if err != nil {
		// Non-fatal - continue with empty CR list
		existingCRs = []*domain.ChangeRequest{}
	}

	// Build summaries for Claude
	specsSummary := buildSpecsSummary(existingSpecs)
	crsSummary := buildCRsSummary(existingCRs)

	// Inject summaries and path into the system prompt
	systemPrompt := fmt.Sprintf(crSystemPrompt, specsSummary, crsSummary, changeRequestsDir, changeRequestsDir)

	fmt.Println("Starting change request creation session...")
	fmt.Printf("Found %d existing specs\n", len(existingSpecs))
	fmt.Printf("Found %d existing change requests\n", len(existingCRs))
	fmt.Println()
	fmt.Println("Change requests will be saved to:", changeRequestsDir)
	fmt.Println()
	fmt.Println("Tip: In another terminal, watch for changes with:")
	fmt.Printf("  watch -n 1 'ls -la %s'\n", changeRequestsDir)
	fmt.Println()

	// Run interactive Claude session
	ctx := context.Background()
	cli := claude.NewCLI()

	_, err = cli.Session(ctx, systemPrompt)
	if err != nil {
		return fmt.Errorf("claude session failed: %w", err)
	}

	fmt.Println()
	fmt.Println("Session ended. Validating change requests...")

	// Validate all change requests
	crValidationErr := validateChangeRequests(store)
	if crValidationErr != nil {
		fmt.Println()
		fmt.Printf("✗ Change request validation failed:\n%s\n", crValidationErr)
		fmt.Println()
		fmt.Println("Starting Claude session to fix validation errors...")
		fmt.Println()

		fixPrompt := fmt.Sprintf(crFixSystemPrompt, utopiaDir, crValidationErr)
		_, err = cli.Session(ctx, fixPrompt)
		if err != nil {
			return fmt.Errorf("claude fix session failed: %w", err)
		}
	} else {
		fmt.Println("✓ All change requests are valid")
	}

	return nil
}

// buildCRsSummary creates a readable summary of existing change requests for Claude
func buildCRsSummary(crs []*domain.ChangeRequest) string {
	if len(crs) == 0 {
		return "(No existing change requests found)"
	}

	var sb strings.Builder
	for _, cr := range crs {
		sb.WriteString(fmt.Sprintf("### %s\n", cr.ID))
		sb.WriteString(fmt.Sprintf("**Type:** %s\n", cr.Type))
		sb.WriteString(fmt.Sprintf("**Title:** %s\n", cr.Title))
		sb.WriteString(fmt.Sprintf("**Status:** %s\n", cr.Status))

		// Show target specs for non-refactor types
		if cr.Type != domain.CRTypeRefactor && cr.Type != domain.CRTypeInitiative {
			targetSpecs := make(map[string]bool)
			for _, change := range cr.Changes {
				if change.Spec != "" {
					targetSpecs[change.Spec] = true
				}
			}
			if len(targetSpecs) > 0 {
				specs := make([]string, 0, len(targetSpecs))
				for spec := range targetSpecs {
					specs = append(specs, spec)
				}
				sb.WriteString(fmt.Sprintf("**Target Specs:** %s\n", strings.Join(specs, ", ")))
			}
		}
		sb.WriteString("\n")
	}

	return sb.String()
}
