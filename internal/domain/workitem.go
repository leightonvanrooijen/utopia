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

	// LastValidatorFeedback stores feedback from validators that failed in the previous iteration.
	// Kept separate from LastFailureOutput to allow distinct prompt sections.
	LastValidatorFeedback string `yaml:"last_validator_feedback,omitempty"`

	// SelectedValidators holds the validator IDs the relevance router chose for
	// this work item. It is populated once, when the item first reaches the
	// validation gate, and reused across retries and resumes so the routing model
	// call happens once per work item rather than every iteration. See
	// ValidatorsRouted to distinguish "not yet routed" from "routed, selected none".
	SelectedValidators []string `yaml:"selected_validators,omitempty"`

	// MechanicalRetryCount tracks consecutive mechanical validation failures on
	// the default executor. It is persisted alongside IterationCount so the
	// mechanical retry cap holds across a resume, and it resets on any
	// comprehension failure because it counts slips in a row.
	MechanicalRetryCount int `yaml:"mechanical_retry_count,omitempty"`

	// ComprehensionCount tracks comprehension validation failures - the ones the
	// same executor cannot fix by trying harder. It is the escalation state: a
	// non-zero count means execution runs on the escalated executor, so persisting
	// it is what stops a resumed work item from resetting to the default executor.
	ComprehensionCount int `yaml:"comprehension_count,omitempty"`

	// ValidatorsRouted records whether the relevance router has already run for
	// this work item. Once true, SelectedValidators is authoritative even when
	// empty, so routing is not repeated on retries or after a resume.
	ValidatorsRouted bool `yaml:"validators_routed,omitempty"`
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
