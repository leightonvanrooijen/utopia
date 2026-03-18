package validators

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/leightonvanrooijen/utopia/internal"
)

// validatorCreatorSystemPrompt is the system prompt for the AI assistant
// that helps users create validators.
const validatorCreatorSystemPrompt = `You are an interactive assistant that helps users create validators for the Utopia system.

## What are Validators?

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

## Your Workflow

1. **Ask what to validate**: Start by asking what aspect of code the user wants to enforce
2. **Understand the context**: If they mention existing files (docs, code examples), read them to extract patterns
3. **Draft the validator**: Create a complete validator based on their requirements and any referenced files
4. **Explain your choices**: Walk through the prompt structure, run trigger choice, and tool selection
5. **Confirm before writing**: Show the complete validator and ask for confirmation
6. **Write the file**: Save to .utopia/validators/{id}.md
7. **Prompt for config**: Remind user to add the validator to .utopia/config.yaml

## Best Practices for Prompts

- Be explicit about what constitutes a pass vs fail
- Reference project-specific terminology from ingested docs
- Focus on one concern per validator (don't combine unrelated checks)
- Give clear, actionable feedback on failures
- Use examples when helpful

## Reading Reference Files

If the user mentions existing documentation or code examples:
- Read the files they reference
- Extract naming conventions, patterns, or rules
- Incorporate these patterns into the validator prompt
- Quote specific examples to make the validator concrete

## Run Trigger Selection

- **after-workitem**: Default. Runs after each work item passes tests. Good for most standards.
- **after-phase**: Runs once after all work items complete. Good for cross-cutting concerns.
- **on-demand**: Skipped during normal execution. Good for optional or expensive checks.

## Example Conversation Flow

1. User: "I want to validate that all API endpoints follow our naming convention"
2. You: "I can help with that! Do you have any documentation about your API naming conventions I should read? Or describe the rules you want to enforce."
3. User: "Yes, read docs/api-guidelines.md"
4. You: [Read the file, extract patterns, propose validator]
5. User: "Looks good, create it"
6. You: [Write the file, remind about config]

Start by asking the user what they want to validate.`

// Creator handles the interactive creation of validators.
type Creator struct {
	projectDir string
	cli        *internal.CLI
}

// NewCreator creates a new validator creator for the given project directory.
func NewCreator(projectDir string) *Creator {
	return &Creator{
		projectDir: projectDir,
		cli:        internal.NewCLI(),
	}
}

// Run starts an interactive session to create a new validator.
// Returns an error if the session fails.
func (c *Creator) Run(ctx context.Context) error {
	// Ensure the validators directory exists
	validatorsDir := filepath.Join(c.projectDir, ".utopia", "validators")
	if err := os.MkdirAll(validatorsDir, 0755); err != nil {
		return fmt.Errorf("failed to create validators directory: %w", err)
	}

	fmt.Println("Starting validator creation assistant...")
	fmt.Println("Press Ctrl+C to exit at any time.")
	fmt.Println()

	// Run the interactive session
	_, err := c.cli.SessionWithCapture(ctx, validatorCreatorSystemPrompt)
	if err != nil {
		// Check if it was a user cancellation
		if ctx.Err() == context.Canceled {
			fmt.Println("\nValidator creation cancelled.")
			return nil
		}
		return fmt.Errorf("validator creation session failed: %w", err)
	}

	// Check if a validator was created and remind about config
	files, _ := filepath.Glob(filepath.Join(validatorsDir, "*.md"))
	if len(files) > 0 {
		fmt.Println()
		fmt.Println("Don't forget to add your validator to .utopia/config.yaml:")
		fmt.Println()
		fmt.Println("  validators:")
		for _, f := range files {
			relPath := strings.TrimPrefix(f, filepath.Join(c.projectDir, ".utopia")+"/")
			fmt.Printf("    - %s\n", relPath)
		}
		fmt.Println()
	}

	return nil
}
