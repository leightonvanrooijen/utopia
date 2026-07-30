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
	// WorkItemNeedsHuman means every bounded escalation path available to the item
	// is exhausted, so no further attempt can change the outcome. It is a distinct
	// terminal state rather than a synonym for failed because the operator action
	// differs: a failed item failed verification and can be retried as-is, while a
	// needs_human item is waiting for a person to re-scope or rewrite its change
	// request. Retrying it unchanged would exhaust the same caps again.
	WorkItemNeedsHuman WorkItemStatus = "needs_human"
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

	// FailureConclusions accumulates what each failed validation attempt
	// concluded: the failing validator's diagnosis and, on a comprehension
	// failure, the corrected intent. It is the one thing that survives a failed
	// attempt into an escalated one, because an escalated attempt is built a fresh
	// context from the evidence rather than handed the transcript that produced it
	// - replaying that transcript would anchor the escalated model to the mental
	// model that just failed. It is cleared when the change request is rewritten,
	// since the diagnoses were about a text that no longer exists.
	FailureConclusions []FailureConclusion `yaml:"failure_conclusions,omitempty"`

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

	// OpusExecutionAttempts tracks how many execution attempts have run on the
	// escalated executor. It is persisted so the cap holds across a resume, and it
	// is never reset - not by a scoping escalation either - because it bounds total
	// spend on the expensive path rather than a streak.
	OpusExecutionAttempts int `yaml:"opus_execution_attempts,omitempty"`

	// ScopingEscalationCount tracks how many times this item has been routed to
	// scoping escalation. It is persisted so the cap holds across a resume: a
	// rewritten change request that fails the same way must not be rewritten
	// forever.
	ScopingEscalationCount int `yaml:"scoping_escalations,omitempty"`

	// ValidatorsRouted records whether the relevance router has already run for
	// this work item. Once true, SelectedValidators is authoritative even when
	// empty, so routing is not repeated on retries or after a resume.
	ValidatorsRouted bool `yaml:"validators_routed,omitempty"`
}

// FailureConclusion is one failing validator's conclusion about one attempt. It
// holds conclusions only - never the attempt's reasoning, its partial diffs or
// its tool calls - so carrying it into a later prompt cannot smuggle the failed
// attempt's transcript along with it.
type FailureConclusion struct {
	// Iteration is the attempt the conclusion was reached about.
	Iteration int `yaml:"iteration"`
	// ValidatorID names the validator that reached it.
	ValidatorID string `yaml:"validator_id"`
	// FailureClass is the class that validator reported, after normalization.
	FailureClass string `yaml:"failure_class,omitempty"`
	// Diagnosis is why the check failed.
	Diagnosis string `yaml:"diagnosis"`
	// CorrectedIntent is what the executor should have understood the work item to
	// mean, present on a comprehension failure and empty otherwise.
	CorrectedIntent string `yaml:"corrected_intent,omitempty"`
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
