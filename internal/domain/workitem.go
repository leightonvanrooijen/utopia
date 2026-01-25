package domain

// Complexity indicates estimated effort for a work item
type Complexity string

const (
	ComplexityLow    Complexity = "low"
	ComplexityMedium Complexity = "medium"
	ComplexityHigh   Complexity = "high"
)

// WorkItemStatus tracks execution state
type WorkItemStatus string

const (
	WorkItemPending    WorkItemStatus = "pending"
	WorkItemInProgress WorkItemStatus = "in_progress"
	WorkItemCompleted  WorkItemStatus = "completed"
	WorkItemFailed     WorkItemStatus = "failed"
)

// WorkItem represents a discrete unit of work for Ralph execution.
// Acceptance criteria are baked into the Prompt field, not stored separately.
// The completion token <COMPLETE> is part of the prompt template.
type WorkItem struct {
	ID      string         `yaml:"id"`
	SpecRef string         `yaml:"spec_ref"` // e.g., "user-authentication.signup"
	Title   string         `yaml:"title"`
	Status  WorkItemStatus `yaml:"status"`

	// The prompt that will be fed to Claude/Ralph (includes acceptance criteria)
	Prompt string `yaml:"prompt"`

	// Constraints that bound the implementation (e.g., "no new abstractions")
	Constraints []string `yaml:"constraints,omitempty"`

	// Sequential execution order (0-indexed position from spec)
	Order int `yaml:"order"`

	// Work items that must complete before this one
	Dependencies []string `yaml:"dependencies,omitempty"`

	// Estimated effort
	Complexity Complexity `yaml:"estimated_complexity"`

	// IterationCount tracks how many Claude invocations have been attempted.
	// Set when status is in_progress or completed.
	IterationCount int `yaml:"iteration_count,omitempty"`

	// LastFailureOutput stores the verification failure from the previous iteration.
	// Only the most recent failure is kept (not accumulated).
	LastFailureOutput string `yaml:"last_failure_output,omitempty"`
}

// NewWorkItem creates a work item from a spec feature.
// The prompt and constraints are set separately by the chunking strategy.
func NewWorkItem(id string, specID string, featureID string, feature Feature, order int) *WorkItem {
	return &WorkItem{
		ID:         id,
		SpecRef:    specID + "." + featureID,
		Title:      feature.Description,
		Status:     WorkItemPending,
		Order:      order,
		Complexity: ComplexityMedium,
	}
}
