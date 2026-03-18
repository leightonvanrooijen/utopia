package validators

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/leightonvanrooijen/utopia/internal"
	"github.com/leightonvanrooijen/utopia/internal/domain"
)

// validatorKnowledgeBase contains the shared knowledge about validators
// used by both the create and edit assistants.
const validatorKnowledgeBase = `## What are Validators?

Validators are automated checks that run during change request execution to enforce code quality and project standards. They are markdown files with YAML frontmatter stored in .utopia/validators/.

## Validator File Format

Validators have this structure:

` + "```" + `yaml
---
id: my-validator-id
run: after-workitem
allowed_tools: [Read, Glob, Grep]
---
Your validation prompt here.

Review the following changes:

{{changed_files}}

Check for [specific things to check].

If all checks pass, output: <PASSED>
` + "```" + `

### Frontmatter Fields

- **id** (required): Unique identifier using kebab-case (e.g., "api-standards", "component-structure")
- **run** (optional): When to run - "after-workitem" (default), "after-phase", or "on-demand"
- **allowed_tools** (optional): Tools the validator can use. Defaults to ["Read", "Glob", "Grep"]. Add "WebFetch" for web lookups.

### Prompt Requirements

1. Always include {{changed_files}} placeholder - it gets replaced with the git diff
2. Be specific about what to check and why
3. Must output <PASSED> token when validation passes
4. Provide actionable feedback when validation fails

## Best Practices for Prompts

- Be explicit about what constitutes a pass vs fail
- Reference project-specific terminology from ingested docs
- Focus on one concern per validator (don't combine unrelated checks)
- Give clear, actionable feedback on failures
- Use examples when helpful

## Run Trigger Selection

- **after-workitem**: Default. Runs after each work item passes tests. Good for most standards.
- **after-phase**: Runs once after all work items complete. Good for cross-cutting concerns.
- **on-demand**: Skipped during normal execution. Good for optional or expensive checks.`

// validatorEditorSystemPrompt is the system prompt for the AI assistant
// that helps users edit existing validators.
func buildValidatorEditorSystemPrompt(validator *domain.Validator, filePath string) string {
	return fmt.Sprintf(`You are an interactive assistant that helps users edit validators for the Utopia system.

%s

## Current Validator

You are editing the validator at: %s

### Current Frontmatter
- **id**: %s
- **run**: %s
- **allowed_tools**: %s

### Current Prompt
%s
---

## Your Workflow

1. **Understand the current state**: You already have the validator loaded above
2. **Ask what to change**: Start by asking what changes the user wants to make
3. **Read context if needed**: If they mention existing files (docs, code examples), read them
4. **Draft the changes**: Show the proposed changes clearly
5. **Explain your changes**: Walk through what you're changing and why
6. **Confirm before writing**: Show the complete updated validator and ask for confirmation
7. **Write the file**: Save to the same location: %s

## Types of Changes

Common edit requests:
- Adjust what the validator checks for
- Change the run trigger (after-workitem, after-phase, on-demand)
- Add or remove allowed tools
- Refine the pass/fail criteria
- Add examples or clarify rules
- Incorporate patterns from reference files

## Reading Reference Files

If the user mentions existing documentation or code examples:
- Read the files they reference
- Extract naming conventions, patterns, or rules
- Incorporate these patterns into the validator prompt
- Quote specific examples to make the validator concrete

Start by asking the user what changes they want to make to this validator.`,
		validatorKnowledgeBase,
		filePath,
		validator.ID,
		string(validator.GetRun()),
		formatToolList(validator.GetAllowedTools()),
		formatPromptForDisplay(validator.Prompt),
		filePath,
	)
}

// formatToolList formats a list of tools for display
func formatToolList(tools []string) string {
	if len(tools) == 0 {
		return "[Read, Glob, Grep] (default)"
	}
	return "[" + strings.Join(tools, ", ") + "]"
}

// formatPromptForDisplay formats the validator prompt for display in the system prompt
func formatPromptForDisplay(prompt string) string {
	if prompt == "" {
		return "(empty)"
	}
	// Add indentation for readability
	lines := strings.Split(prompt, "\n")
	for i, line := range lines {
		if line != "" {
			lines[i] = "  " + line
		}
	}
	return "\n" + strings.Join(lines, "\n")
}

// Editor handles the interactive editing of validators.
type Editor struct {
	projectDir string
	cli        *internal.CLI
	store      *internal.YAMLStore
}

// NewEditor creates a new validator editor for the given project directory.
func NewEditor(projectDir string) *Editor {
	utopiaDir := filepath.Join(projectDir, ".utopia")
	return &Editor{
		projectDir: projectDir,
		cli:        internal.NewCLI(),
		store:      internal.NewYAMLStore(utopiaDir),
	}
}

// ValidatorInfo contains information about an available validator for selection.
type ValidatorInfo struct {
	Path      string            // Path relative to .utopia/
	Validator *domain.Validator // Loaded validator
}

// ListValidators returns all configured validators from .utopia/config.yaml.
// Returns an error if no validators are configured.
func (e *Editor) ListValidators() ([]ValidatorInfo, error) {
	config, err := e.store.LoadConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}

	if len(config.Validators) == 0 {
		return nil, fmt.Errorf("no validators configured in .utopia/config.yaml")
	}

	var validators []ValidatorInfo
	for _, path := range config.Validators {
		validator, err := e.store.LoadValidator(path)
		if err != nil {
			// Include the error but continue listing others
			fmt.Fprintf(os.Stderr, "Warning: failed to load validator %s: %v\n", path, err)
			continue
		}
		validators = append(validators, ValidatorInfo{
			Path:      path,
			Validator: validator,
		})
	}

	if len(validators) == 0 {
		return nil, fmt.Errorf("no valid validators found (check warnings above)")
	}

	return validators, nil
}

// Run starts an interactive session to edit a validator.
// The validatorPath should be relative to .utopia/ (e.g., "validators/my-validator.md").
func (e *Editor) Run(ctx context.Context, validatorPath string) error {
	// Load the validator
	validator, err := e.store.LoadValidator(validatorPath)
	if err != nil {
		return fmt.Errorf("failed to load validator: %w", err)
	}

	fullPath := filepath.Join(e.projectDir, ".utopia", validatorPath)

	fmt.Printf("Editing validator: %s\n", validator.ID)
	fmt.Printf("File: %s\n", fullPath)
	fmt.Println()
	fmt.Println("Starting validator edit assistant...")
	fmt.Println("Press Ctrl+C to exit at any time.")
	fmt.Println()

	// Build the system prompt with the current validator state
	systemPrompt := buildValidatorEditorSystemPrompt(validator, fullPath)

	// Run the interactive session
	_, err = e.cli.SessionWithCapture(ctx, systemPrompt)
	if err != nil {
		// Check if it was a user cancellation
		if ctx.Err() == context.Canceled {
			fmt.Println("\nValidator editing cancelled.")
			return nil
		}
		return fmt.Errorf("validator edit session failed: %w", err)
	}

	return nil
}
