package domain

import "strings"

// RunTrigger specifies when a validator should execute
type RunTrigger string

const (
	// RunAfterWorkitem runs after each work item passes verification (default)
	RunAfterWorkitem RunTrigger = "after-workitem"
	// RunAfterPhase runs only after initiative phase completes
	RunAfterPhase RunTrigger = "after-phase"
	// RunOnDemand validators are skipped during normal execution
	RunOnDemand RunTrigger = "on-demand"
)

// DefaultAllowedTools returns the default read-only tools for validators
func DefaultAllowedTools() []string {
	return []string{"Read", "Glob", "Grep"}
}

// Validator represents a project standards validator loaded from a .md file.
// Validators use YAML frontmatter for configuration and markdown body for the prompt.
type Validator struct {
	// ID is the unique identifier for this validator (required)
	ID string `yaml:"id"`

	// Run specifies when the validator should execute (optional, defaults to "after-workitem")
	Run RunTrigger `yaml:"run,omitempty"`

	// AllowedTools specifies which tools the validator can use (optional, defaults to ["Read", "Glob", "Grep"])
	AllowedTools []string `yaml:"allowed_tools,omitempty"`

	// Prompt is the markdown body sent to Claude (not stored in frontmatter)
	Prompt string `yaml:"-"`

	// ModelOverride is set from config.yaml to override the models.validators default.
	// This is not stored in the validator file's frontmatter.
	ModelOverride string `yaml:"-"`

	// RunOverride is set from config.yaml to override the run trigger from frontmatter.
	// This is not stored in the validator file's frontmatter.
	RunOverride RunTrigger `yaml:"-"`
}

// GetRun returns the run trigger, checking override first, then frontmatter, then default.
// Priority: RunOverride (from config) > Run (from frontmatter) > "after-workitem" (default)
func (v *Validator) GetRun() RunTrigger {
	if v.RunOverride != "" {
		return v.RunOverride
	}
	if v.Run == "" {
		return RunAfterWorkitem
	}
	return v.Run
}

// GetModel returns the model override if set, otherwise empty string.
// Callers should fall back to models.validators or the global default if empty.
func (v *Validator) GetModel() string {
	return v.ModelOverride
}

// GetAllowedTools returns the allowed tools, defaulting to read-only tools if not specified
func (v *Validator) GetAllowedTools() []string {
	if len(v.AllowedTools) == 0 {
		return DefaultAllowedTools()
	}
	return v.AllowedTools
}

// ExpandPrompt returns the prompt with placeholders replaced.
// Currently supports:
//   - {{changed_files}} - replaced with the provided changedFiles content (typically a git diff)
func (v *Validator) ExpandPrompt(changedFiles string) string {
	return strings.ReplaceAll(v.Prompt, "{{changed_files}}", changedFiles)
}
