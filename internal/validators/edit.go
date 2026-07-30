package validators

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/leightonvanrooijen/utopia/internal"
	"github.com/leightonvanrooijen/utopia/internal/domain"
	"github.com/leightonvanrooijen/utopia/internal/ui"
)

// buildValidatorEditorSystemPrompt builds the system prompt for the AI assistant
// that helps users edit existing validators.
func buildValidatorEditorSystemPrompt(validator *domain.Validator, filePath string) string {
	return fmt.Sprintf(`%s

---

## Your Role: Validator Editing Assistant

You are helping a user edit an EXISTING validator.

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
3. **Ask clarifying questions**: Understand the intent behind the change
4. **Read context if needed**: If they mention existing files (docs, code examples), read them
5. **Draft the changes**: Show the proposed changes clearly, highlighting what's different
6. **Explain your changes**: Walk through what you're changing and why. Discuss trade-offs.
7. **Confirm before writing**: Show the complete updated validator and ask for confirmation
8. **Write the file**: Save to the same location: %s

## Types of Changes

Common edit requests:
- Adjust what the validator checks for
- Change the run trigger (after-workitem, after-phase, on-demand)
- Add or remove allowed tools
- Refine the pass/fail criteria to be more unambiguous
- Make feedback messages more verbose and actionable
- Add examples or clarify rules
- Incorporate patterns from reference files
- Narrow or broaden the scope of validation

## Questions to Ask Before Making Changes

- What specific behavior do you want to change?
- Should this affect what passes, what fails, or how feedback is given?
- Are there edge cases we should handle differently?
- Should the error messages be more or less detailed?

Start by asking the user what changes they want to make to this validator.`,
		validatorAssistantPrompt,
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
	model      string
	// out receives the session's prompts and the load warnings ListValidators
	// raises. Nil means the process's own streams.
	out *ui.Printer
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

// WithPrinter routes the editor's output through the caller's printer, so what an
// edit session prints is capturable rather than written straight to the process
// streams.
func (e *Editor) WithPrinter(p *ui.Printer) *Editor {
	e.out = p
	e.cli = e.cli.WithPrinter(p)
	return e
}

// WithModel sets the Claude model to use for this editor.
func (e *Editor) WithModel(model string) *Editor {
	e.model = model
	e.cli = e.cli.WithModel(model)
	return e
}

// WithEffort sets the reasoning effort the editing session runs at, resolved by
// the caller the same way the model is. The empty string leaves the claude CLI on
// its own default.
func (e *Editor) WithEffort(effort string) *Editor {
	e.cli = e.cli.WithEffort(effort)
	return e
}

// WithAuth selects the credential the editing session authenticates with.
// The empty mode inherits the ambient environment, so a caller that never
// resolved a mode keeps the pre-auth behaviour.
func (e *Editor) WithAuth(mode domain.AuthMode) *Editor {
	e.cli = e.cli.WithAuth(mode, filepath.Join(e.projectDir, ".utopia"))
	return e
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
	for _, vc := range config.Validators {
		validator, err := e.store.LoadValidator(vc.GetPath())
		if err != nil {
			// Include the error but continue listing others
			ui.OrDefault(e.out).Progressf("Warning: failed to load validator %s: %v\n", vc.GetPath(), err)
			continue
		}
		validators = append(validators, ValidatorInfo{
			Path:      vc.GetPath(),
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

	out := ui.OrDefault(e.out)
	out.Printf("Editing validator: %s\n", validator.ID)
	out.Printf("File: %s\n", fullPath)
	out.Println()
	out.Println("Starting validator edit assistant...")
	out.Println("Press Ctrl+C to exit at any time.")
	out.Println()

	// Build the system prompt with the current validator state
	systemPrompt := buildValidatorEditorSystemPrompt(validator, fullPath)

	// Run the interactive session
	_, err = e.cli.SessionWithCapture(ctx, systemPrompt)
	if err != nil {
		// Check if it was a user cancellation
		if ctx.Err() == context.Canceled {
			out.Println("\nValidator editing cancelled.")
			return nil
		}
		return fmt.Errorf("validator edit session failed: %w", err)
	}

	return nil
}
