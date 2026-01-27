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

var (
	specStrategyFlag string
)

var specCmd = &cobra.Command{
	Use:   "spec",
	Short: "Create or refine a specification",
	Long: `Start a conversation with Claude to create or refine a specification.

The conversation flows through stages:
  1. EXPLORE  - What problem are you solving?
  2. DEFINE   - What's in scope for v1?
  3. SPECIFY  - What are the features and acceptance criteria?

Claude guides you through these stages naturally. The resulting
specification is saved to .utopia/specs/

Tip: Run a file watcher on the spec file to see updates in real-time:
  watch -n 1 cat .utopia/specs/_drafts/current-spec.yaml`,
	RunE: runSpec,
}

func init() {
	rootCmd.AddCommand(specCmd)

	specCmd.Flags().StringVarP(&specStrategyFlag, "strategy", "s", "",
		"spec creation strategy (guided, minimal, template)")
}

// specSystemPrompt guides Claude through the spec creation workflow
// Use fmt.Sprintf to inject: specPath, changeRequestPath, existingSpecsSummary
const specSystemPrompt = `You are a Specification Claude - an AI assistant that helps users transform ideas into structured specifications.

## Your Role
Guide users through a natural conversation to create specifications. You understand the existing spec landscape and intelligently decide whether to:
1. Create a NEW spec (for new systems/features)
2. Create a CHANGE REQUEST (for modifications to existing specs)
3. Create BOTH (when requirements span new and existing systems)

## Existing Specifications
Review these existing specs to understand what's already defined:

%s

## The Journey

### PHASE 1: UNDERSTAND
Start by understanding what the user wants to accomplish:
- What are you trying to build or change?
- Listen for signals about existing functionality vs new functionality

### PHASE 2: CLASSIFY
Based on the user's description, determine if this is:
- **New Spec**: A new system, feature area, or capability not covered by existing specs
- **Change Request**: Modifications, additions, or removals to an existing spec
- **Both**: New functionality that also requires changes to existing specs

**For Change Requests, classify the TYPE by asking: "Does this change observable behavior?"**

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

Tell the user your assessment: "This sounds like [new spec / a TYPE change request to X / both]. Does that match your understanding?"

### PHASE 3: EXPLORE & DEFINE
For new specs:
- What problem are you solving? Who is this for?
- What's in scope for v1? What are the constraints?

For change requests:
- Which existing spec are we modifying?
- What specifically needs to change? (add features, modify criteria, remove items)

### PHASE 4: SPECIFY
Capture detailed requirements with specific, testable acceptance criteria.

## Output Formats

### For NEW SPECS
Save to: %s

` + "```yaml" + `
id: kebab-case-identifier
title: Human Readable Title
status: draft
description: |
  Brief description of what this system does.

domain_knowledge:
  - Key business rule or constraint

features:
  - id: feature-id
    description: What this feature does
    acceptance_criteria:
      - Specific, testable condition
` + "```" + `

### For CHANGE REQUESTS
Save to: %s/{spec-id}-{change-description}.yaml

**Choose the correct type based on classification:**

#### Feature CR (new capability)
` + "```yaml" + `
id: spec-id-add-feature-name
type: feature
title: Add new capability
status: draft
changes:
  - operation: add
    spec: target-spec-id  # Which spec to add to
    feature:
      id: new-feature-id
      description: What this feature does
      acceptance_criteria:
        - Specific testable condition
` + "```" + `

#### Enhancement CR (modify existing capability)
` + "```yaml" + `
id: spec-id-enhance-feature-name
type: enhancement
title: Enhance existing feature
status: draft
changes:
  - operation: modify
    spec: target-spec-id
    feature_id: existing-feature-id
    description: Updated description  # Optional
    criteria:
      add: ["New criterion"]
      remove: ["Exact text to remove"]
      edit:
        - old: "Exact old text"
          new: "Replacement text"
` + "```" + `

#### Refactor CR (behavior unchanged)
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

#### Removal CR (delete capability)
` + "```yaml" + `
id: spec-id-remove-feature-name
type: removal
title: Remove deprecated feature
status: draft
changes:
  - operation: remove
    spec: target-spec-id
    feature_id: feature-to-remove
    reason: Why this is being removed
` + "```" + `

## Guidelines
- Ask ONE question at a time
- Summarize and confirm understanding frequently
- For change requests, verify the parent_spec ID matches exactly
- For modify/remove operations, text must match EXACTLY (no fuzzy matching)
- Acceptance criteria must be testable (not vague)
- ALWAYS use the Write tool with the appropriate path

Start by warmly greeting the user and asking what they'd like to work on today.`

func runSpec(cmd *cobra.Command, args []string) error {
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

	// Create drafts directory if it doesn't exist
	draftsDir := filepath.Join(utopiaDir, "specs", "_drafts")
	if err := os.MkdirAll(draftsDir, 0755); err != nil {
		return fmt.Errorf("failed to create drafts directory: %w", err)
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

	// Build spec summary for Claude
	specsSummary := buildSpecsSummary(existingSpecs)

	// Generate paths for Claude to write to
	specPath := filepath.Join(draftsDir, "current-spec.yaml")

	// Inject paths and spec summary into the system prompt
	systemPrompt := fmt.Sprintf(specSystemPrompt, specsSummary, specPath, changeRequestsDir)

	fmt.Println("Starting spec creation session...")
	fmt.Printf("Found %d existing specs\n", len(existingSpecs))
	fmt.Println()
	fmt.Println("New specs will be saved to:", specPath)
	fmt.Println("Change requests will be saved to:", changeRequestsDir)
	fmt.Println()
	fmt.Println("Tip: In another terminal, watch for changes with:")
	fmt.Printf("  watch -n 1 'ls -la %s'\n", filepath.Join(utopiaDir, "specs"))
	fmt.Println()

	// Run interactive Claude session directly (no TUI wrapper)
	ctx := context.Background()
	cli := claude.NewCLI()

	_, err = cli.Session(ctx, systemPrompt)
	if err != nil {
		return fmt.Errorf("claude session failed: %w", err)
	}

	fmt.Println()
	fmt.Println("Session ended. Validating specs...")

	// Validate all specs in the specs directory
	validationErr := validateSpecs(store)
	if validationErr == nil {
		fmt.Println("✓ All specs are valid YAML")
		return nil
	}

	// Validation failed - start a Claude session to fix the errors
	fmt.Println()
	fmt.Printf("✗ Spec validation failed:\n%s\n", validationErr)
	fmt.Println()
	fmt.Println("Starting Claude session to fix validation errors...")
	fmt.Println()

	fixPrompt := fmt.Sprintf(specFixSystemPrompt, utopiaDir, validationErr)
	_, err = cli.Session(ctx, fixPrompt)
	if err != nil {
		return fmt.Errorf("claude fix session failed: %w", err)
	}

	return nil
}

// validateSpecs attempts to load all specs and returns any validation errors
func validateSpecs(store *storage.YAMLStore) error {
	_, err := store.ListSpecs()
	return err
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

// specFixSystemPrompt is used when specs fail validation
const specFixSystemPrompt = `You are a YAML Fix Claude - an AI assistant that helps fix YAML validation errors in specification files.

## Your Task
Fix the YAML validation errors in the spec files. The specs are located in: %s/specs/

## Validation Error
%s

## Guidelines
- Read the problematic file(s) mentioned in the error
- Fix the YAML syntax issues (common problems: unquoted colons, unquoted curly braces, indentation)
- Strings containing colons followed by spaces need to be quoted
- Strings containing curly braces {} need to be quoted
- Save the fixed file(s)

## Common YAML Fixes
- "did not find expected key" usually means unquoted special characters
- "cannot unmarshal !!map into string" means YAML is parsing a string as a map (quote the string)

Start by reading the file mentioned in the error and fixing it.`
