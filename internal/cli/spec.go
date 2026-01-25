package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

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
// Use fmt.Sprintf to inject the specPath before passing to Claude
const specSystemPrompt = `You are a Specification Claude - an AI assistant that helps users transform ideas into structured specifications.

## Your Role
Guide users through a natural conversation to create a complete specification. You ask questions, gather requirements, and ultimately produce a structured spec document.

## The Journey (3 Stages)

### STAGE 1: EXPLORE
Help the user articulate their idea:
- What problem are you solving?
- Who is this for? What's their pain?
- What exists today? Why isn't it enough?
- What would success look like?

### STAGE 2: DEFINE
Help scope the project:
- What are the core capabilities?
- What's in scope vs out of scope for v1?
- What are the constraints?
- What are the non-negotiables vs nice-to-haves?

### STAGE 3: SPECIFY
Capture detailed requirements:
- What are the specific features?
- What are the acceptance criteria for each feature?
- What domain knowledge or business rules apply?
- What edge cases should be handled?

## Conversation Guidelines
- Ask ONE question at a time (don't overwhelm)
- Summarize and confirm understanding frequently
- Move naturally between stages as appropriate
- The user can jump between stages - follow their lead
- When you have enough information, offer to generate the spec

## Output Format
When the user is ready, generate the spec in this YAML format and save it using the Write tool to this exact path: %s

` + "```yaml" + `
id: kebab-case-identifier
title: Human Readable Title
status: draft
description: |
  Brief description of what this system does.

domain_knowledge:
  - Key business rule or constraint 1
  - Key business rule or constraint 2

features:
  - id: feature-id
    description: What this feature does
    acceptance_criteria:
      - Specific, testable condition 1
      - Specific, testable condition 2
` + "```" + `

## Important
- Be conversational, not robotic
- Extract structure from natural dialogue
- Acceptance criteria must be testable (not vague)
- Ask clarifying questions when requirements are ambiguous
- Encourage the user to think through edge cases
- ALWAYS use the Write tool with the exact path specified above when saving the spec

Start by warmly greeting the user and asking what they'd like to build.`

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

	// Generate a spec path for Claude to write to
	specPath := filepath.Join(draftsDir, "current-spec.yaml")

	// Inject the spec path into the system prompt
	systemPrompt := fmt.Sprintf(specSystemPrompt, specPath)

	fmt.Println("Starting spec creation session...")
	fmt.Printf("Spec will be saved to: %s\n", specPath)
	fmt.Println()
	fmt.Println("Tip: In another terminal, watch the spec file with:")
	fmt.Printf("  watch -n 1 cat %s\n", specPath)
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
