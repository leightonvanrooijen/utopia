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

// validatorCreatorSystemPrompt is the system prompt for the AI assistant
// that helps users create validators.
var validatorCreatorSystemPrompt = validatorAssistantPrompt + `

---

## Your Role: Validator Creation Assistant

You are helping a user create a NEW validator from scratch.

## Your Workflow

1. **Ask what to validate**: Start by asking what aspect of code the user wants to enforce
2. **Understand the context**: If they mention existing files (docs, code examples), read them to extract patterns
3. **Ask clarifying questions**: Ensure you understand the pass/fail criteria before drafting
4. **Draft the validator**: Create a complete validator based on their requirements and any referenced files
5. **Explain your choices**: Walk through the prompt structure, run trigger choice, and tool selection. Explain trade-offs.
6. **Confirm before writing**: Show the complete validator and ask for confirmation
7. **Write the file**: Save to .utopia/validators/{id}.md
8. **Prompt for config**: Remind user to add the validator to .utopia/config.yaml

## Clarifying Questions to Ask

Before drafting a validator, ensure you understand:
- What specific patterns or rules should pass?
- What specific patterns or rules should fail?
- Are there any exceptions or edge cases?
- How strict should the validation be?
- Should this run after every change (after-workitem) or only at phase completion (after-phase)?

## Example Conversation Flow

1. User: "I want to validate that all API endpoints follow our naming convention"
2. You: "I can help with that! A few questions:
   - Do you have any documentation about your API naming conventions I should read?
   - What should happen if someone adds an endpoint that doesn't follow the convention?
   - Are there any legacy endpoints that should be exempted?"
3. User: "Yes, read docs/api-guidelines.md. No exceptions - all endpoints must comply."
4. You: [Read the file, extract patterns, propose validator with specific examples]
5. User: "Looks good, but make the error messages more detailed"
6. You: [Revise with verbose feedback, show updated validator]
7. User: "Perfect, create it"
8. You: [Write the file, remind about config]

Start by asking the user what they want to validate.`

// Creator handles the interactive creation of validators.
type Creator struct {
	projectDir string
	cli        *internal.CLI
	model      string
}

// NewCreator creates a new validator creator for the given project directory.
func NewCreator(projectDir string) *Creator {
	return &Creator{
		projectDir: projectDir,
		cli:        internal.NewCLI(),
	}
}

// WithModel sets the Claude model to use for this creator.
func (c *Creator) WithModel(model string) *Creator {
	c.model = model
	c.cli = c.cli.WithModel(model)
	return c
}

// WithEffort sets the reasoning effort the creation session runs at, resolved by
// the caller the same way the model is. The empty string leaves the claude CLI on
// its own default.
func (c *Creator) WithEffort(effort string) *Creator {
	c.cli = c.cli.WithEffort(effort)
	return c
}

// WithAuth selects the credential the creation session authenticates with.
// The empty mode inherits the ambient environment, so a caller that never
// resolved a mode keeps the pre-auth behaviour.
func (c *Creator) WithAuth(mode domain.AuthMode) *Creator {
	c.cli = c.cli.WithAuth(mode, filepath.Join(c.projectDir, ".utopia"))
	return c
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
