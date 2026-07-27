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
//
// Valid frontmatter fields: id, description, allowed_tools
// The "run" field should be configured in config.yaml, not in the validator file.
type Validator struct {
	// ID is the unique identifier for this validator (required)
	ID string `yaml:"id"`

	// Description is a short "what this checks and when it applies" line read by
	// the relevance router to decide whether this validator is worth running for a
	// given change - exactly as Claude skills route on their description, without
	// loading the (expensive) Prompt body. Optional: an empty Description means the
	// router has no signal to route on and must treat the validator as always
	// applicable, never silently skipping it.
	Description string `yaml:"description,omitempty"`

	// AllowedTools specifies which tools the validator can use (optional, defaults to ["Read", "Glob", "Grep"])
	AllowedTools []string `yaml:"allowed_tools,omitempty"`

	// Prompt is the markdown body sent to Claude (not stored in frontmatter)
	Prompt string `yaml:"-"`

	// ModelOverride is set from config.yaml to override the models.validators default.
	// This is not stored in the validator file's frontmatter.
	ModelOverride string `yaml:"-"`

	// Run is set from config.yaml to specify when the validator should execute.
	// Defaults to "after-workitem" if not configured.
	// This is not stored in the validator file's frontmatter (deprecated there).
	Run RunTrigger `yaml:"-"`
}

// GetRun returns the run trigger from config, defaulting to "after-workitem".
func (v *Validator) GetRun() RunTrigger {
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
