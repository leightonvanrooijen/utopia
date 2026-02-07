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
  - bugfix:      Correct behavior to match spec
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
const crSystemPrompt = `You are a Change Request Claude - an AI assistant that helps users create structured change requests.

## Your Role
Guide users through a natural conversation to create change requests. CRs can target existing specs OR define new specs (which get created when the CR is merged).

## Existing Specifications
These are the existing specs (CRs can also target new specs not listed here):

%s

## Existing Change Requests
These change requests already exist (avoid duplicates):

%s

## The Journey

### PHASE 1: UNDERSTAND
Start by understanding what the user wants to change:
- What are you trying to modify or improve?
- Which spec does this relate to? (can be existing or a new spec)
- Ask ONE question at a time

### PHASE 2: CLASSIFY
Determine the CR type by asking: "Does this change observable behavior?"

| Intent Signal | CR Type | Key Question |
|--------------|---------|--------------|
| Behavior unchanged (code cleanup, restructure) | refactor | "Will the system behave exactly the same afterward?" |
| New capability that doesn't exist | feature | "Is this something users can't do today?" |
| Modifying how existing capability works | enhancement | "Are we changing how an existing feature behaves?" |
| Behavior doesn't match spec (bug) | bugfix | "Should this already work according to the spec?" |
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
- For bugfixes: What spec/feature is broken? How should it behave per the spec?
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
    spec: target-spec-id  # REQUIRED: Which spec to add to (can be new or existing)
    # Include spec_metadata ONLY when targeting a spec that doesn't exist yet:
    spec_metadata:
      title: Human-readable spec title
      description: What this spec is about
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

### Bugfix CR (correct behavior to match spec)
` + "```yaml" + `
id: spec-id-fix-feature-name
type: bugfix
title: Fix behavior to match spec
status: draft
tasks:  # Note: tasks, not changes (bugfixes don't modify specs)
  - id: task-id
    spec: target-spec-id  # REQUIRED: Which spec defines correct behavior
    feature_id: feature-to-fix  # REQUIRED: Which feature defines correct behavior
    description: |
      Fix [feature] to match spec [spec-id].
      Current behavior: [what it does wrong]
      Expected behavior: [what the spec says it should do]
    acceptance_criteria:
      - Behavior matches spec [spec-id] feature [feature-id]
      - [Specific testable condition from spec]
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
- CRs CAN target specs that don't exist yet - new specs are created when the CR is merged, not during CR creation
- NEVER write to .utopia/specs/ directly - all spec changes happen through the CR merge process
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

// buildSpecsSummary creates a readable summary of existing specs for Claude
func buildSpecsSummary(specs []*domain.Spec) string {
	if len(specs) == 0 {
		return "(No existing specs found)"
	}

	var sb strings.Builder
	for _, spec := range specs {
		sb.WriteString(fmt.Sprintf("### %s\n", spec.ID))
		sb.WriteString(fmt.Sprintf("**Title:** %s\n", spec.Title))
		sb.WriteString(fmt.Sprintf("**Status:** %s\n", spec.Status))

		// Truncate description if too long
		desc := strings.TrimSpace(spec.Description)
		if len(desc) > 200 {
			desc = desc[:200] + "..."
		}
		sb.WriteString(fmt.Sprintf("**Description:** %s\n", desc))

		// List feature IDs
		if len(spec.Features) > 0 {
			sb.WriteString("**Features:** ")
			featureIDs := make([]string, len(spec.Features))
			for i, f := range spec.Features {
				featureIDs[i] = f.ID
			}
			sb.WriteString(strings.Join(featureIDs, ", "))
			sb.WriteString("\n")
		}
		sb.WriteString("\n")
	}

	return sb.String()
}

// validateChangeRequests validates all change request files for YAML syntax and required fields.
// Returns nil if all CRs are valid, or an error describing validation failures.
func validateChangeRequests(store *storage.YAMLStore) error {
	crs, err := store.ListChangeRequests()
	if err != nil {
		// ListChangeRequests returns parse errors for invalid YAML
		return err
	}

	// Validate each CR has required fields based on type
	var allErrors []string
	for _, cr := range crs {
		if validationErr := domain.ValidateChangeRequest(cr); validationErr != nil {
			allErrors = append(allErrors, fmt.Sprintf("%s: %v", cr.ID, validationErr))
		}
	}

	if len(allErrors) > 0 {
		return fmt.Errorf("change request validation errors:\n%s", strings.Join(allErrors, "\n"))
	}

	return nil
}

// crFixSystemPrompt is used when change requests fail validation
const crFixSystemPrompt = `You are a CR Fix Claude - an AI assistant that helps fix Change Request YAML validation errors.

## Your Task
Fix the validation errors in the change request files. The CRs are located in: %s/change-requests/

## Validation Error
%s

## CR Structure Requirements by Type

### feature/enhancement/removal types require:
- id: unique identifier (kebab-case)
- type: "feature", "enhancement", or "removal"
- title: human-readable title
- status: "draft", "in-progress", or "complete"
- changes: array of changes (REQUIRED, cannot be empty)
  - Each change MUST have a "spec" field specifying the target spec ID
  - Each change has an "operation" field: "add", "modify", or "remove"

### refactor type requires:
- id: unique identifier (kebab-case)
- type: "refactor"
- title: human-readable title
- status: "draft", "in-progress", or "complete"
- tasks: array of tasks (REQUIRED, cannot be empty)
  - Each task needs: id, description, acceptance_criteria
  - Tasks do NOT have a "spec" field (refactors don't modify specs)

### bugfix type requires:
- id: unique identifier (kebab-case)
- type: "bugfix"
- title: human-readable title
- status: "draft", "in-progress", or "complete"
- tasks: array of tasks (REQUIRED, cannot be empty)
  - Each task needs: id, description, acceptance_criteria, spec, feature_id
  - spec: target spec that defines correct behavior (REQUIRED)
  - feature_id: feature that defines correct behavior (REQUIRED)
  - Tasks reference spec/feature but don't modify the spec

### initiative type requires:
- id: unique identifier (kebab-case)
- type: "initiative"
- title: human-readable title
- status: "draft", "in-progress", or "complete"
- phases: array of phases (REQUIRED, cannot be empty)
  - Each phase has a "type" field (feature, enhancement, removal, refactor, or bugfix)
  - Refactor and bugfix phases use "tasks" array
  - Other phase types use "changes" array (each change needs "spec" field)

## Guidelines
- Read the problematic file mentioned in the error
- Fix validation issues (missing fields, wrong structure for type)
- Fix YAML syntax issues (unquoted colons/braces, indentation)
- Save the fixed file

Start by reading the file mentioned in the error and fixing it.`
