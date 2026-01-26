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

var refactorCmd = &cobra.Command{
	Use:   "refactor",
	Short: "Create a refactor plan via AI-guided conversation",
	Long: `Start a conversation with Claude to define a code refactoring plan.

Refactors focus on HOW code is structured, not WHAT it does. They are
temporary work artifacts - deleted after completion.

Claude guides you through defining:
  - What needs to be restructured
  - Specific tasks with testable acceptance criteria
  - Behavior preservation constraints

The resulting refactor plan is saved to .utopia/refactors/

Tip: Run a file watcher to see updates in real-time:
  watch -n 1 cat .utopia/refactors/current-refactor.yaml`,
	RunE: runRefactor,
}

func init() {
	rootCmd.AddCommand(refactorCmd)
}

// refactorSystemPrompt guides Claude through the refactor creation workflow
// Use fmt.Sprintf to inject: existingRefactorsSummary, refactorPath
const refactorSystemPrompt = `You are a Refactor Claude - an AI assistant that helps users define code restructuring plans.

## Your Role
Guide users through a natural conversation to create refactor plans. Refactors focus on HOW code is structured, not WHAT it does. They are temporary work artifacts that are deleted after completion.

Key principle: **Behavior must be preserved.** Refactors change structure, not functionality.

## Existing Refactors
Review any existing refactors to understand what's already planned:

%s

## The Journey

### PHASE 1: UNDERSTAND
Start by understanding what the user wants to restructure:
- What code or system needs restructuring?
- Why is the current structure problematic?
- What would the ideal structure look like?

### PHASE 2: SCOPE
Define the boundaries of the refactor:
- Which files, packages, or modules are involved?
- What should NOT change? (preserve existing behavior)
- Are there dependencies that need consideration?

### PHASE 3: DEFINE TASKS
Break down the refactor into discrete tasks:
- Each task should be independently completable
- Tasks should have clear, testable acceptance criteria
- Order tasks by dependency (what needs to happen first?)

## Output Format
Save to: %s

` + "```yaml" + `
id: kebab-case-identifier
title: Human Readable Title
status: draft
tasks:
  - id: task-id
    description: |
      What this task accomplishes.
    acceptance_criteria:
      - Specific, testable condition
      - Another testable condition
      - All existing tests pass (behavior preserved)

  - id: another-task-id
    description: |
      What this task accomplishes.
    acceptance_criteria:
      - Specific condition
` + "```" + `

## Guidelines
- Ask ONE question at a time
- Summarize and confirm understanding frequently
- Each task should preserve existing behavior
- Acceptance criteria must be testable (not vague)
- Include "all existing tests pass" as a criterion where appropriate
- ALWAYS use the Write tool with the provided path

## Example Tasks
Good acceptance criteria examples:
- "All database operations are defined in a single interface"
- "No direct file system calls exist outside the storage package"
- "All existing tests pass without modification"
- "The public API remains unchanged"

Bad (vague) criteria:
- "Code is cleaner"
- "Better organized"
- "More maintainable"

Start by warmly greeting the user and asking what they'd like to refactor.`

func runRefactor(cmd *cobra.Command, args []string) error {
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

	// Create refactors directory if it doesn't exist
	refactorsDir := filepath.Join(utopiaDir, "refactors")
	if err := os.MkdirAll(refactorsDir, 0755); err != nil {
		return fmt.Errorf("failed to create refactors directory: %w", err)
	}

	// Load existing refactors for context
	existingRefactors, err := store.ListRefactors()
	if err != nil {
		// Non-fatal - continue with empty refactor list
		existingRefactors = []*domain.Refactor{}
	}

	// Build refactors summary for Claude
	refactorsSummary := buildRefactorsSummary(existingRefactors)

	// Generate path for Claude to write to
	refactorPath := filepath.Join(refactorsDir, "current-refactor.yaml")

	// Inject paths and summary into the system prompt
	systemPrompt := fmt.Sprintf(refactorSystemPrompt, refactorsSummary, refactorPath)

	fmt.Println("Starting refactor creation session...")
	fmt.Printf("Found %d existing refactors\n", len(existingRefactors))
	fmt.Println()
	fmt.Println("Refactor will be saved to:", refactorPath)
	fmt.Println()
	fmt.Println("Tip: In another terminal, watch for changes with:")
	fmt.Printf("  watch -n 1 cat %s\n", refactorPath)
	fmt.Println()

	// Run interactive Claude session
	ctx := context.Background()
	cli := claude.NewCLI()

	_, err = cli.Session(ctx, systemPrompt)
	if err != nil {
		return fmt.Errorf("claude session failed: %w", err)
	}

	fmt.Println()
	fmt.Println("Session ended. Validating refactor...")

	// Validate all refactors in the refactors directory
	validationErr := validateRefactors(store)
	if validationErr == nil {
		fmt.Println("✓ Refactor is valid YAML")
		return nil
	}

	// Validation failed - start a Claude session to fix the errors
	fmt.Println()
	fmt.Printf("✗ Refactor validation failed:\n%s\n", validationErr)
	fmt.Println()
	fmt.Println("Starting Claude session to fix validation errors...")
	fmt.Println()

	fixPrompt := fmt.Sprintf(refactorFixSystemPrompt, utopiaDir, validationErr)
	_, err = cli.Session(ctx, fixPrompt)
	if err != nil {
		return fmt.Errorf("claude fix session failed: %w", err)
	}

	return nil
}

// validateRefactors attempts to load all refactors and returns any validation errors
func validateRefactors(store *storage.YAMLStore) error {
	_, err := store.ListRefactors()
	return err
}

// buildRefactorsSummary creates a readable summary of existing refactors for Claude
func buildRefactorsSummary(refactors []*domain.Refactor) string {
	if len(refactors) == 0 {
		return "(No existing refactors found)"
	}

	var sb strings.Builder
	for _, r := range refactors {
		sb.WriteString(fmt.Sprintf("### %s\n", r.ID))
		sb.WriteString(fmt.Sprintf("**Title:** %s\n", r.Title))
		sb.WriteString(fmt.Sprintf("**Status:** %s\n", r.Status))

		// List task IDs
		if len(r.Tasks) > 0 {
			sb.WriteString("**Tasks:** ")
			taskIDs := make([]string, len(r.Tasks))
			for i, t := range r.Tasks {
				taskIDs[i] = t.ID
			}
			sb.WriteString(strings.Join(taskIDs, ", "))
			sb.WriteString("\n")
		}
		sb.WriteString("\n")
	}

	return sb.String()
}

// refactorFixSystemPrompt is used when refactors fail validation
const refactorFixSystemPrompt = `You are a YAML Fix Claude - an AI assistant that helps fix YAML validation errors in refactor files.

## Your Task
Fix the YAML validation errors in the refactor files. The refactors are located in: %s/refactors/

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
